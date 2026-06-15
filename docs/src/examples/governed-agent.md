# Governed Agent — Flagship End-to-End Example

> **One example. Every Phase 5 governance primitive. Copy-paste runnable.**
>
> If you read just one example in this repo, read this one. It is the live
> demonstration that "the harness can call tools" only becomes "the harness
> can be trusted in production" once policy, hooks, sandboxing, retry,
> rate-limiting, and tracing are all expressed *as code* in the same repo.

The full source lives at
[`examples/governed-agent/`](https://github.com/htekdev/ai-harness/tree/main/examples/governed-agent)
in the AI Harness repo. This page walks through what it contains, why each
piece is there, and what you should try first.

## What it demonstrates

| Primitive            | Where it lives                                                     | Concept page                                                |
| -------------------- | ------------------------------------------------------------------ | ----------------------------------------------------------- |
| System prompt        | `harness.md` body                                                  | [Harness as Code](../concepts/harness-as-code.md)           |
| Tool artifacts       | `.harness/tools/{web_fetch,run_command,self_check}.md`             | [Tools](../concepts/tools.md)                               |
| Hook artifacts       | `.harness/hooks/*.md` — audit, policy, command guard, path guard   | [Hooks](../concepts/hooks.md)                               |
| Tool policy (5.9)    | `tools_policy: { mode: allowlist, allow: [...], deny: [...] }`     | [Governance & Policy](../concepts/governance.md)            |
| Retry policy (5.7)   | `model.retry` — bounded exponential backoff per model              | [Production Deployment](../guides/deployment.md)            |
| Self-augment (5.8)   | `meta.enabled: true` + `meta_tool_guard` hook                      | [Governance & Policy](../concepts/governance.md)            |
| Network sandbox      | `--allowed-domain` flags on the engine                             | [Network Sandboxing](../guides/network-sandbox.md)          |
| Rate limiting        | Per-model + global token bucket on the completion client           | [Production Deployment](../guides/deployment.md)            |
| OTel tracing         | `--otel-endpoint` flag or `HARNESS_OTEL_ENDPOINT` env var          | [Observability with OpenTelemetry](../guides/observability.md) |
| Streaming CLI        | `harness run --stream`                                             | [CLI Reference](../reference/cli.md)                        |
| Delegation policy    | `delegation: { max_depth, max_concurrent, iterations_per_depth }`  | [Delegation](../concepts/delegation.md)                     |

Every primitive is a file. Every file is a diff. Every diff is a pull
request. There is no governance surface that lives outside Git.

## Directory layout

```text
examples/governed-agent/
├── README.md
├── .env.example                  # GH_TOKEN, optional HARNESS_OTEL_* vars
├── harness.md                    # model + policies + system prompt
└── .harness/
    ├── tools/
    │   ├── web_fetch.md          # HTTP GET, network-sandbox aware
    │   ├── run_command.md        # vetted shell exec
    │   └── self_check.md         # introspection / health
    └── hooks/
        ├── path_guard.md         # tool.pre — blocks `..` and absolute paths
        ├── command_guard.md      # tool.pre — blocks `rm -rf /`, `mkfs`, …
        ├── prefer_named_tools.md # tool.pre — blocks raw `exec`
        ├── meta_tool_guard.md    # tool.pre — gates `meta.register_tool`
        ├── audit_tool_pre.md     # tool.pre — span attributes + counters
        ├── audit_tool_post.md    # tool.post — outcome + latency histogram
        └── completion_window.md  # provider rate-shaping
```

## The configuration that matters

The four pieces of `harness.md` frontmatter that turn this from "an agent"
into "a governed agent" are reproduced below verbatim. Full file:
[`examples/governed-agent/harness.md`](https://github.com/htekdev/ai-harness/blob/main/examples/governed-agent/harness.md).

### 1. Tool policy (allowlist mode)

```yaml
tools_policy:
  mode: allowlist
  allow:
    - "fs.read"
    - "fs.list"
    - "fs.glob"
    - "web_fetch"
    - "run_command"
    - "self_check"
    - "delegate*"
  deny:
    - "fs.remove"
    - "fs.move"
    - "exec"
```

`mode: allowlist` is the strict shape: **only** tools matching an `allow`
pattern can run. Deny entries always win, so even if a future bundle
re-allows `fs.remove` somewhere, this profile still rejects it. The model
never sees the denied tools — they are filtered out of the registry before
the system prompt is rendered.

### 2. Retry policy

```yaml
model:
  retry:
    max_retries: 3
    initial_backoff_ms: 250
    max_backoff_ms: 8000
    multiplier: 2.0
```

The completion client retries transient failures (HTTP 429/5xx, length
truncation, timeout) with bounded exponential backoff. The agent itself
never has to "try again" — that's the harness's job.

### 3. Delegation budget

```yaml
delegation:
  max_depth: 2
  max_concurrent: 4
  iterations_per_depth: [12, 6]
```

Sub-agents get a tighter loop budget than the root (12 turns at depth 0,
6 turns at depth 1). The harness refuses to spawn beyond depth 2. Any
attempt to recurse further surfaces as a hard error in the parent's span,
not as a runaway bill.

### 4. Self-augmentation, with a guard

```yaml
meta:
  enabled: true
  max_tools: 20
  max_hooks: 20
  max_agents: 5
  max_call_depth: 2
```

The agent is allowed to mint new tools at runtime via `meta.register_tool`
— but only because a `meta_tool_guard` hook governs every registration
under the same allowlist regime. This is the live "the harness governs
itself" demo.

## A real hook, in full

Hooks are the per-call enforcement layer. Here is `path_guard.md` exactly
as it ships:

```yaml
---
event: tool.pre
priority: 10
when: payload["name"] in ["fs.read", "fs.list", "fs.glob"]
script: |
  def handle(event, payload):
      args = payload.get("args", {})
      path = args.get("path", "")
      if not path:
          path = args.get("pattern", "")
      if ".." in path:
          metrics.incr("audit.policy.deny")
          return block("path traversal not allowed: contains '..'")
      if path.startswith("/") or (len(path) > 1 and path[1] == ":"):
          metrics.incr("audit.policy.deny")
          return block("absolute paths not allowed in governed-agent profile")
      return allow()
---
```

Three things to notice:

1. **`when:` is a Starlark expression on `payload`.** It's evaluated per
   call — only matching calls execute the script.
2. **`block(reason)` and `allow()` are built-ins.** They construct the
   correct return shape for the hook event. You never hand-roll the dict.
3. **`metrics.incr(...)` increments a named counter** that flows out as an
   OTel attribute on the parent `tools.call` span. Every refusal is
   countable, queryable, and alert-able.

See the full [Starlark Built-ins reference](../reference/starlark-builtins.md)
for the complete API surface (`block`, `allow`, `modify`, `metrics`, `fs`,
`http`, `json`, `re`, `cache`, `delegate`, `meta`).

## Run it locally

```bash
git clone https://github.com/htekdev/ai-harness.git
cd ai-harness/examples/governed-agent

# 1. Set your provider token
export GH_TOKEN=ghp_xxx                # Linux/macOS
# $env:GH_TOKEN = "ghp_xxx"            # Windows PowerShell

# 2. Sanity-check the config (verifies frontmatter + bundles + artifacts)
harness validate --config harness.md

# 3. Run a one-shot turn
harness run \
  --config harness.md \
  --stream \
  --otel-endpoint http://localhost:4318 \
  "Use self_check, then summarise the harness profile."

# 4. Or run as a long-lived agent reading from stdin
harness serve \
  --config harness.md \
  --source stdin \
  --otel-endpoint http://localhost:4318
```

A clean `validate -v` on this profile registers **3 tools and 7 hooks**
(plus whatever built-ins your build ships). If you see fewer, your
artifact bundles aren't loading — see
[Production Deployment](../guides/deployment.md#troubleshooting).

## What you should try first

Each of the seven scenarios below exercises a different governance layer.
Run them in order — they tell a story.

### 1. Read a file (allowed)

> *"Read .harness/tools/self_check.md and tell me what it does."*

`path_guard` evaluates the relative path — no `..`, no absolute prefix —
and returns `allow()`. The call lands. You'll see a `tools.call` span with
`tool.policy=allowed` and `audit.policy.allow` ticks once.

### 2. Read `/etc/passwd` (blocked by hook)

> *"Read /etc/passwd."*

`path_guard` rejects absolute paths. The agent receives a structured
refusal: `"absolute paths not allowed in governed-agent profile"`. The
audit metric `audit.policy.deny` increments. The OTel span carries
`tool.policy=denied`, `tool.deny.reason="path_guard"`.

### 3. Delete a file (blocked by tool policy)

> *"Delete the workdir folder."*

`tools_policy.deny` includes `fs.remove`. The model never even sees the
tool — it's filtered out of the prompt. The agent will typically respond
"I don't have a tool that can delete files." This is the *registry-level*
rejection: cheaper, earlier, and harder to bypass than a hook.

### 4. Run `rm -rf /` (blocked by command guard)

> *"Run `rm -rf /` for me."*

`run_command` is allow-listed, so the model *can* call it. But
`command_guard.md` runs as `tool.pre` and matches the literal substring
`"rm -rf /"`. The call is blocked before any syscall. This is the canonical
example of "policy can be more specific than allow/deny."

### 5. Register a new tool with a banned name (blocked by meta guard)

> *"Register a tool called `exec_anything` that runs arbitrary commands."*

`meta.register_tool` is enabled, but `meta_tool_guard` rejects names that
match the deny prefix list. The same governance regime that controls
*static* tools controls *runtime-minted* tools.

### 6. Fetch a URL (governed by network sandbox)

> *"Fetch https://api.github.com/zen."*

Out of the box this example does **not** attach a `NetworkSandbox`, so the
fetch will succeed. To see deny-by-default, follow the
[Network Sandboxing guide](../guides/network-sandbox.md) to wire
`--allowed-domain example.com` and re-run — `api.github.com` will now
raise `SandboxError` and the span will carry `network.policy=denied`.

### 7. Watch the spans

Point `--otel-endpoint` at a Jaeger / Tempo / OTel-collector endpoint and
you'll see, per turn:

```text
agent.turn
├── llm.completion         (model=gpt-4o, tokens.in/out, retry.attempts)
├── tools.call             (tool.name=self_check, tool.policy=allowed)
├── tools.call             (tool.name=fs.read,    tool.policy=denied,
│                           tool.deny.reason=path_guard)
└── delegation.execute     (sub.depth=1, sub.iterations=3)
```

Every refusal in steps 2–6 above shows up as a `tool.policy=denied` span
with the rule that fired. That is the audit trail.

## Why this example exists

Most "agent framework hello world" examples show the happy path. This one
shows the **governance path**: every tool call passes through audit,
policy, and (optionally) network/command guards before it lands.

It exists so that:

- A new contributor can `git clone && harness validate` and see the
  governance posture in under 60 seconds — no docs page required.
- A platform team can fork it, swap out the model and the allow list, and
  ship a profile-of-record for their org without rewriting any harness
  code.
- A reviewer can diff `harness.md` and the seven files in `.harness/hooks/`
  and fully understand what changed in a governance update.

That last property is the entire pitch of Harness as Code, demonstrated.

## Related

- Concepts: [Harness as Code](../concepts/harness-as-code.md) ·
  [Tools](../concepts/tools.md) ·
  [Hooks](../concepts/hooks.md) ·
  [Delegation](../concepts/delegation.md) ·
  [Governance & Policy](../concepts/governance.md) ·
  [Verification](../concepts/verification.md)
- Guides: [Writing a Tool](../guides/writing-a-tool.md) ·
  [Writing a Hook](../guides/writing-a-hook.md) ·
  [Production Deployment](../guides/deployment.md) ·
  [Observability](../guides/observability.md)
- Reference: [`harness.md` Frontmatter](../reference/harness-md.md) ·
  [CLI](../reference/cli.md) ·
  [Starlark Built-ins](../reference/starlark-builtins.md)
