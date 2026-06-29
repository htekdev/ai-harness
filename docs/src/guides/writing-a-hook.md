# Writing a Hook

> A hands-on tutorial. By the end of this guide you'll have written,
> validated, and shipped four hooks that exercise every part of the
> hook contract: a `tool.pre` guard that blocks, a `tool.pre` mutator
> that rewrites arguments, a `tool.post` auditor that logs, and a
> `when:`-gated hook that fires selectively.

This guide assumes you finished the [Quickstart](../getting-started.md)
and the [Writing a Tool](./writing-a-tool.md) tutorial. We'll reuse the
`word_count` tool from that guide as the target of our hooks.

If you skipped writing a tool, run `harness init` in an empty directory
to get the four scaffolded tools (`read_file`, `write_file`,
`list_files`, `get_current_folder`) — every example here works against
them too.

## What a hook actually is

A hook is a typed artifact that subscribes to a **lifecycle event** and
returns one of four verdicts:

| Verdict   | Effect                                                         |
|-----------|----------------------------------------------------------------|
| `allow`   | Continue. Other hooks on this event still run.                 |
| `block`   | Stop the operation. Subsequent hooks do **not** run.           |
| `delegate` | Redirect control flow into a new delegation request. Supported on `agent.stop` and `delegation.post`. |
| `modify`  | Replace the payload. Following hooks see the new payload.      |

That control surface is the whole control plane. Everything from
secret-scanning to per-tool retries to claims verification is built
from `allow / block / delegate / modify`.

The events you can subscribe to are fixed:

```
session.start    session.end
turn.start       turn.end
agent.stop
tool.pre         tool.post
completion.pre   completion.post
delegation.pre   delegation.post   delegation.post_verify
error
```

You can also emit and listen for `custom.*` and `meta.*` events for
your own workflows.

## 1. Set up

If you don't already have a workspace:

```bash
mkdir -p my-agent && cd my-agent
harness init .
```

This guide also assumes you have the `word_count` tool from the
[Writing a Tool](./writing-a-tool.md) guide saved as
`.harness/tools/word_count.md`. If you skipped it, copy that file in
now — every snippet below targets it by name.

## 2. The hook artifact format

Every hook is a markdown file with YAML frontmatter:

```markdown
---
event: tool.pre        # required — which lifecycle event to subscribe to
priority: 10           # optional — lower numbers run first (default 100)
when: "<expr>"         # optional — Starlark expression; hook skipped if false
script: |              # required — YAML literal block of Starlark
  def handle(event, payload):
      # ...
      return {"action": "allow"}
---

# Human-readable name

Body markdown is **model-visible context** — it's composed into the
system prompt the same way a tool's body is. Use it to explain *why*
this hook exists, not what it blocks.
```

Three things worth burning in:

- **The entry point is `handle(event, payload)`.** Always. Not `run`,
  not `main`, not `on_event`. The runtime calls `globals["handle"]`
  by name.
- **`script:` is a YAML literal block, not a fenced code block.** The
  `|` and the indentation are what make it Starlark. Fenced
  ` ```starlark ` blocks in the body are documentation, not code.
- **Returns are dicts (or helper builtins).** The canonical shape is
  `{"action": "block", "reason": "..."}`. There are also `allow()`,
  `block(reason=...)`, and `modify(payload=...)` builtins for brevity.

## 3. Your first hook — a `tool.pre` path guard

Create `.harness/hooks/word_count_path_guard.md`:

```markdown
---
event: tool.pre
priority: 10
when: 'payload["name"] == "word_count"'
script: |
  def handle(event, payload):
      args = payload.get("arguments", {})
      path = args.get("path", "")
      forbidden = ["/etc/", "/root/", "/var/lib/"]
      for prefix in forbidden:
          if path.startswith(prefix):
              return {
                  "action": "block",
                  "reason": "path " + path + " is in a protected directory",
              }
      return {"action": "allow"}
---

# word_count path guard

Prevents `word_count` from reading files under system-sensitive
directories. Blocks at the `tool.pre` boundary so the call never
reaches the Starlark sandbox.
```

A few things to notice:

- **`when:` is a fast filter.** It runs *before* `handle` is even
  invoked. Use it to scope a hook to a single tool, model, or
  condition without paying the `handle` overhead on every other call.
- **The payload for `tool.pre` is flat.** Top-level keys are
  `id`, `name`, and `arguments`. There is no `payload["tool"]` wrapper.
- **`arguments` is a dict.** The tool's typed parameters are
  already JSON-decoded by the time your hook sees them.

Validate it compiles:

```bash
harness validate
```

Expected:

```
✅ harness.md valid
   5 tools, 3 hooks, 0 agents (3 ms)
```

Trigger it:

```bash
harness run "Count the words in /etc/passwd."
```

The model will receive a structured error (`blocked by hook
"word_count_path_guard": path /etc/passwd is in a protected
directory`) and explain to the user that the path is protected. The
Starlark `run` function in `word_count.md` never executed.

## 4. A `tool.pre` hook that **modifies** instead of blocking

`block` is the heavy hammer. Often you want to *fix* the call instead
— normalize a path, fill in a default, strip dangerous characters.

Create `.harness/hooks/word_count_default_ignore_blanks.md`:

```markdown
---
event: tool.pre
priority: 20
when: 'payload["name"] == "word_count"'
script: |
  def handle(event, payload):
      args = payload.get("arguments", {})
      if "ignore_blank_lines" not in args:
          args["ignore_blank_lines"] = True
          payload["arguments"] = args
          return {"action": "modify", "payload": payload}
      return {"action": "allow"}
---

# word_count default: ignore blank lines

If the model forgets to pass `ignore_blank_lines`, default it to
`True`. Keeps counts consistent across calls and stops the model from
relying on undocumented defaults.
```

Key rules of `modify`:

- **Return the full payload back**, not just the field you changed.
  The runtime replaces the whole payload with what you returned.
- **Subsequent hooks see the modified version.** If two hooks both
  modify the same field, the higher-priority (lower number) one wins
  the first pass and the next one sees the result.
- **Modification is silent to the model** — the call is reported as a
  normal tool invocation with the rewritten arguments. That's the
  whole point: governance without surprising the agent.

## 5. A `tool.post` audit hook

Even allowed calls should leave a trail. Add
`.harness/hooks/word_count_audit.md`:

```markdown
---
event: tool.post
priority: 50
when: 'payload["name"] == "word_count"'
script: |
  def handle(event, payload):
      log("word_count.audit name=" + payload.get("name", "") +
          " is_error=" + str(payload.get("is_error", False)) +
          " bytes=" + str(len(payload.get("result", ""))))
      return {"action": "allow"}
---

# word_count audit log

Emits a single audit line after every `word_count` call, including
failed ones. Pair with the OpenTelemetry exporter to ship audit
events to Jaeger or any OTel collector.
```

Two things worth knowing about `tool.post`:

- **The payload is the `tools.Result`**, not the original call. Keys
  are `call_id`, `name`, `content`, `is_error`, and `result` (an alias
  for `content` added for script ergonomics).
- **`result`/`content` is a string**, not a parsed object. The tool's
  Starlark `run(args)` returned a dict; the runtime stringified it
  before reaching your hook. If you need structured access, decode
  with `json.decode(payload["result"])`.

Run a counted call:

```bash
harness run --stream "How many words in CHANGELOG.md?"
```

You should see a `[script] word_count.audit name=word_count
is_error=False bytes=…` line in stderr. The hook fired *after* the
tool returned, saw the actual result, and emitted a structured event
without changing the tool or its return value.

## 6. Composing hooks with `priority`

Both `word_count_path_guard` (priority 10) and
`word_count_default_ignore_blanks` (priority 20) listen on `tool.pre`.
The path guard runs first. If it blocks, the modifier never runs —
which is exactly what you want for security hooks.

The ordering contract:

1. Hooks on the same event are sorted by `priority` ascending.
2. Lower priority numbers run first.
3. A `block` short-circuits everything after it.
4. A `modify` is visible to every later hook on the same event.

Conventional bands:

| Priority   | Purpose                                          |
|------------|--------------------------------------------------|
| 1–49       | Security / governance — runs first, can veto.    |
| 50–99      | Application logic — defaults, normalization.     |
| 100+       | Observability — audit, metrics, tracing.         |

The scaffolded `block_dangerous_commands.md` ships with priority 100
intentionally — it's a coarse safety net, not the primary gate.

## 7. Using helper builtins for brevity

For the very common cases, you can skip the dict literal:

```python
def handle(event, payload):
    if payload.get("name") != "word_count":
        return allow()
    if payload.get("arguments", {}).get("path", "").startswith("/etc/"):
        return block(reason="protected path")
    return allow()
```

The `allow()`, `block(reason=...)`, and `modify(payload=...)` builtins
produce the same `hooks.Result` as the equivalent dict, with no
chance of misspelling the `"action"` key.

## 8. Custom events and meta hooks

Hooks aren't limited to lifecycle events. You can emit your own:

```python
# inside any tool or hook
emit("custom.user_signed_up", {"user_id": uid})
```

And subscribe with `event: custom.user_signed_up` in a hook
frontmatter. This is how you build app-level pipelines (post-purchase
audits, on-deploy smoke checks, anything you'd otherwise write as a
message bus) without leaving the harness.

The `meta.*` event family is reserved for the harness's own
introspection events (model swap, sub-agent spawn, context truncation).
Same `handle(event, payload)` signature.

## 9. Iterate

A hook is done when:

1. **Validate passes.** No frontmatter typos, no Starlark compile errors.
2. **The `when:` clause narrows correctly** so the hook is cheap for
   unrelated calls.
3. **The verdict is one of `allow / block / modify`.** No `return None`,
   no raising exceptions — the runtime treats both as "continue with
   no change" and the silent fallthrough will haunt you later.
4. **The body explains *why* the hook exists.** That markdown is part
   of the system prompt the model sees. Treat it as documentation
   the agent itself reads.
5. **Priority sits in the right band** for its role.

## What you've learned

You've built every layer of the hook surface:

- **The artifact format** — `event:`, `priority:`, optional `when:`,
  `script:` YAML literal block.
- **The handler contract** — `handle(event, payload)` returning
  `allow / block / modify`.
- **`tool.pre` guards and modifiers** — gating inputs and rewriting
  arguments before the sandbox runs.
- **`tool.post` auditors** — observing results without changing them.
- **Composition rules** — `priority` ordering, `when:` filtering,
  short-circuit semantics.

That's the full hook authoring surface. Every more advanced hook —
delegation verification, completion rewriting, custom event pipelines
— is the same five pieces.

## What to read next

- **[Writing a Tool](./writing-a-tool.md)** — the symmetric tutorial
  for tools.
- **[Hooks (concept)](../concepts/hooks.md)** — the design rationale
  for the event catalog and dispatch model.
- **[Verification](../concepts/verification.md)** — how
  `delegation.post_verify` hooks implement a Ralph loop around
  sub-agent results.
- **[Governance & Policy](../concepts/governance.md)** — how hooks
  compose with the other three governance layers.
- **Reference:** the [Hook Artifact Schema](../reference/hook-artifact.md)
  documents every supported frontmatter field and event payload shape.
