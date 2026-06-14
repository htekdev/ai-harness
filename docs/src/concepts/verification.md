# Verification

> Verification is the primitive that asks one question after every
> delegation: **did the work actually happen?** When the answer is "no,"
> the harness re-prompts the same delegate with the failure reason and
> tries again — a deterministic Ralph loop bolted onto the
> [delegation](./delegation.md) lifecycle.

If [tools](./tools.md) are *what an agent can do* and [delegation](./delegation.md)
is *how an agent recruits help*, verification is *how the harness refuses
to take the agent's word for it*. A delegate can claim it created the
file, opened the PR, or fixed the test. Verification proves it.

## The hallucinated-success problem

Sub-agents fail in a specific, expensive way: they finish their turn loop
and confidently report success when none of the side effects actually
happened. The file does not exist. The repo does not resolve. The commit
is not on the branch. The model said "Done." and meant it.

Every layer above the delegation now believes a lie. Hooks downstream of
`delegation.post` operate on fabricated context. The parent agent
composes follow-up work on a foundation that isn't there. By the time a
human notices, three turns of token spend later, the fix is no longer
"retry the delegate" — it is "unwind the conversation."

The deterministic answer is to assert the side effect against ground
truth before the parent ever sees the result. That assertion is what
verification is.

## What verification is

Verification is a check that runs **between the delegate's response and
`delegation.post`**. It has access to ground truth — the filesystem, the
network, the harness's own built-ins — and it returns a single structured
verdict:

```go
type VerifyOutcome struct {
    Verified bool   `json:"verified"`
    Reason   string `json:"reason,omitempty"`
}
```

`Verified: true` lets the result through. `Verified: false` triggers the
Ralph loop: the *same* delegate is re-invoked with the verifier's
`Reason` injected into the prompt, so the model sees the truth and can
correct course. The loop is bounded by `MaxVerifyRetries` (default
`2`, configurable per request).

Compile and runtime errors in the verifier itself are **hard failures**,
not "verified: false." Operators should see broken verifiers as broken
verifiers — not as silent acceptance.

## Two surfaces, one contract

Verification is exposed two ways. Both produce a `VerifyOutcome` and
both feed the same Ralph loop.

### Surface 1: inline `Verify` script on the request

Set `Verify` on a `delegation.Request` to declare a one-shot verifier
inline. The script is Starlark, with the same built-ins as a tool:

```starlark
def run(result):
    # `result` is a dict shaped like the JSON encoding of
    # delegation.Result: {response, tool_calls, tool_results}.
    resp = http.get("https://api.github.com/repos/htekdev/ai-harness")
    return json.encode({
        "verified": resp["status"] == 200,
        "reason": "" if resp["status"] == 200 else "repo not found",
    })
```

The script must define `run(result)` and return a JSON-encoded object
with at least `verified` (bool); `reason` is optional but strongly
recommended on failures because the string is what the delegate sees on
retry.

A bare `True` or `False` return is tolerated (treated as `verified: true`
or `verified: false` with a generic reason). Anything else is a hard
error.

### Surface 2: `delegation.post_verify` hook event

For policy-as-code verification — checks every delegation should run
regardless of who issued it — register a hook on the
`delegation.post_verify` event. The event fires *before*
`delegation.post`, so verifiers run before redaction or summarization
hooks have a chance to launder a fabricated success.

```yaml
---
event: delegation.post_verify
priority: 10
script: |
  def handle(event, payload):
      # payload is the same dict the inline verifier sees
      claims = payload.get("response", "")
      if "I created" in claims:
          # cheap heuristic: claim implies a file should exist
          return {"action": "block", "reason": "claim made but no file path provided"}
      return {"action": "allow"}
---
```

Hook verifiers use the standard `allow / block / modify` ternary.
`ActionBlock` is verification failure with the hook's reason.
`ActionModify` rewrites the result *in place* before the next verifier
sees it — useful for canonicalizing claims into a structured shape that
later verifiers can check against.

When both surfaces are present, **both must pass**. Inline `verify:`
runs first, hook verifiers run second, and the failure reasons are
joined into a single string for the retry prompt.

## The Ralph loop

The retry mechanic gives verification its name in the codebase. Each
attempt looks like this:

```
attempt 0:
  prompt = original task
  delegate runs → result
  verify(result) → {verified: false, reason: "file does not exist"}

attempt 1:
  prompt = "VERIFICATION FAILED on the previous attempt: file does not
            exist\n\nThe task is NOT complete. Re-examine the actual
            state of the world and finish the work. Do not just claim
            success — actually verify the side effects exist before
            responding."
  delegate runs → result
  verify(result) → {verified: true}

→ result returned to parent
```

Three properties of this loop matter:

1. **Same delegate, not a fresh one.** The conversation context, the
   tool history, and the partial reasoning are preserved across
   attempts. The delegate sees what it claimed and why the harness
   rejected it.
2. **Failure reason is mechanical.** The retry prompt is a fixed
   template with the verifier's `reason` interpolated in. There is no
   model-of-the-day generating the correction text.
3. **Bounded.** `MaxVerifyRetries + 1` total attempts. If the loop
   exhausts without verification passing, the delegation returns
   `errs.KindVerificationFailed` and the parent's `tool.post` chain
   sees a structured error — not a fabricated success.

## Verification telemetry

Every verified delegation records four attributes on its
`delegation.execute` OTel span:

| Attribute                      | Meaning                                           |
|--------------------------------|---------------------------------------------------|
| `delegation.verify_attempts`   | How many times the verifier ran                   |
| `delegation.verify_passed`     | `true` if the final attempt was accepted          |
| `delegation.verify_outcome`    | `passed` / `failed` / `skipped`                   |
| `delegation.kind` (existing)   | Lets you slice verify metrics by delegate profile |

These are the raw inputs for the most useful operational dashboard a
governed agent has: **failure-mode distribution by delegate**. A
delegate that needs three retries on average to verify is telling you
something about the prompt, the tool surface, or the model — and you
have the data to fix it without re-deriving it from logs.

Pair the span attributes with the existing `tool.pre` / `tool.post`
audit hooks and a verification failure looks like a single connected
trace: the original tool calls inside the delegate, the verifier's
verdict, the retry prompt, the corrective tool calls, and the final
acceptance.

## Why verification is at the boundary, not per-tool

A common alternative design is to attach a verifier to *every tool*.
That has two problems:

1. **Tools don't know what success looks like.** A `write_file` tool
   knows whether the syscall returned, not whether the file's contents
   match the *intent* of the original task. Intent lives at the
   delegation boundary, where the task string is.
2. **Per-tool verification multiplies cost.** Every tool call pays
   verifier latency. At the boundary, you pay it once per delegation
   regardless of how many tools the delegate used.

Verification at the delegation boundary keeps both costs aligned with
the unit of work that has a *claim* attached: the sub-agent's final
response. The delegate can call `write_file` ten times during its turn
loop; verification only asks "is the world the way you said it would
be?" once, after the loop is over.

> A future surface — per-tool `verify:` blocks on tool artifacts — will
> let operators add cheap inline assertions inside the delegate's loop
> for a different purpose: catching a single bad tool call early so the
> delegate can correct course *without* burning a delegation retry. It
> is complementary to boundary verification, not a replacement. Tracked
> in [issue #103][i103].

[i103]: https://github.com/htekdev/ai-harness/issues/103

## Patterns

Three shapes recur in real verification scripts.

**Existence check.** The most common verifier — *did the artifact you
claimed to create actually appear?*

```starlark
def run(result):
    info = fs.stat(args["expected_path"])
    return json.encode({
        "verified": info != None,
        "reason": "" if info else "expected file does not exist",
    })
```

**Reachability check.** *Does the URL/repo/endpoint the delegate
referenced actually resolve?*

```starlark
def run(result):
    resp = http.get(args["url"])
    ok = resp["status"] in [200, 204]
    return json.encode({
        "verified": ok,
        "reason": "" if ok else "endpoint returned %d" % resp["status"],
    })
```

**Shape check.** *Is the structured output the delegate produced
parseable and well-formed?*

```starlark
def run(result):
    out = result.get("response", "")
    parsed = json.decode(out, default=None)
    if parsed == None:
        return json.encode({"verified": False, "reason": "response is not valid JSON"})
    if "id" not in parsed:
        return json.encode({"verified": False, "reason": "response missing required 'id' field"})
    return json.encode({"verified": True})
```

The pattern across all three: the verifier reads ground truth, not the
delegate's claim *about* ground truth. That is the whole point.

## Verification versus testing versus monitoring

Three nearby ideas, each useful in a different place:

- **Tests** assert that *code* is correct, run in CI, and block merges.
- **Monitoring** asserts that *production* is healthy, runs continuously,
  and pages humans.
- **Verification** asserts that *this delegation just told the truth*,
  runs once per delegation, and feeds back into the same delegate's next
  attempt.

Verification is not a substitute for either of the others. It is what
sits between them — the runtime check that turns "the model claimed it
worked" into "the model verifiably did the work" before any downstream
hook, parent agent, or user sees the result.

## What to read next

- **[Delegation](./delegation.md)** — for the lifecycle that
  verification slots into between child response and `delegation.post`.
- **[Hooks](./hooks.md)** — for the `allow / block / modify` contract
  that `delegation.post_verify` uses.
- **[Governance & Policy](./governance.md)** — for how verification
  composes with the broader four-layer governance stack.
- **Reference:** the [Hook Artifact Schema](../reference/hook-artifact.md)
  documents the `delegation.post_verify` payload shape and event
  ordering relative to `delegation.post`.
