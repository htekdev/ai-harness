# Your First Governed Agent (End-to-End)

> **One sitting. Five files. A real agent you'd trust in production.**
>
> The [Quickstart](../getting-started.md) gets you a running harness in five
> minutes. The "Writing a…" guides each go deep on one primitive. **This guide
> stitches them together** — by the end you will have built a single agent
> from scratch that uses a custom tool, a custom hook, a tool policy, and a
> sub-agent, all expressed as code in one repo.
>
> **Time budget:** ~20 minutes if you already have `GH_TOKEN` or
> `OPENAI_API_KEY`.

---

## What you'll build

A research assistant called **`reporter`** that:

1. Uses a custom `web_fetch` **tool** to pull HTTP content.
2. Has a custom `path_guard` **hook** that blocks any tool from reaching
   outside the working directory.
3. Enforces a **tool policy** in `harness.md` — allowlist mode, no `fs.remove`,
   no raw `exec`.
4. Delegates summarization work to a `summarizer` **sub-agent** with its own
   stricter budget.

Every primitive is a separate file, every file is a diff, every diff is a PR.
That's *Harness as Code*.

---

## Layout

```text
my-reporter/
├── harness.md
└── .harness/
    ├── tools/
    │   └── web_fetch.md
    ├── hooks/
    │   └── path_guard.md
    └── agents/
        └── summarizer.md
```

Five files. Nothing more. Create the directories now:

```bash
mkdir -p my-reporter/.harness/{tools,hooks,agents}
cd my-reporter
```

---

## Step 1 — `harness.md` (the spine)

`harness.md` is the only file the harness *requires*. Everything else is
loaded by convention from `.harness/`. Start with the system prompt, the
model, and a deliberately permissive policy that we will tighten later:

```markdown
---
model:
  provider: copilot
  name: gpt-4o
  api_key_env: GH_TOKEN
  retry:
    max_retries: 3
    initial_backoff_ms: 250
    max_backoff_ms: 8000

context:
  max_history: 30
  max_tokens: 32000

# Step 3 will tighten this. For now: allow everything except destructive ops.
tools_policy:
  mode: allowlist
  allow:
    - "fs.read"
    - "fs.list"
    - "web_fetch"
    - "delegate*"
  deny:
    - "fs.remove"
    - "fs.move"
    - "exec"

delegation:
  max_depth: 1
  max_concurrent: 2
  iterations_per_depth: [8]
---

# Reporter

You are **reporter**, a careful research assistant.

When asked a research question:

1. Use `web_fetch` to pull primary sources (one URL per call).
2. Use `fs.read` only inside the working directory.
3. Delegate long summarization work to the `summarizer` sub-agent.
4. Always cite the URLs you fetched in your final answer.

You will never call `fs.remove`, `fs.move`, or raw `exec`. The policy will
refuse those calls before they reach the model anyway — but you should not
even propose them.
```

Test that it boots:

```bash
harness run --config harness.md "Say hello and list your capabilities."
```

You should see a single completion. No tools have been registered yet, so
the agent will just describe what it *would* do.

---

## Step 2 — Add the `web_fetch` tool

Tools are Starlark scripts wrapped in a frontmatter envelope. See
[Writing a Tool](./writing-a-tool.md) for the full schema.

Create `.harness/tools/web_fetch.md`:

```markdown
---
parameters:
  url: { type: string, required: true }
  timeout_ms: { type: number, required: false }
script: |
  def run(args):
      url = args.get("url", "")
      if not url:
          return {"error": "url is required"}
      timeout = args.get("timeout_ms", 10000)
      resp = http.get(url, {}, timeout)
      return {
          "status": resp.get("status", 0),
          "url": url,
          "body": string.truncate(resp.get("body", ""), 4000),
      }
---

# web_fetch

Fetch an HTTP(S) URL through the harness network sandbox. Only hosts the
engine was started with via `--allowed-domain` will succeed; everything else
is blocked before the request leaves the process.

Use this instead of asking the user to paste content.
```

Run it with the sandbox engaged:

```bash
harness run --config harness.md \
  --allowed-domain "raw.githubusercontent.com" \
  "Fetch https://raw.githubusercontent.com/htekdev/ai-harness/main/README.md and summarize the first paragraph."
```

The agent should call `web_fetch` exactly once. Try it again with a domain
**not** on the allowlist (`example.com`) — the call surfaces a `SandboxError`
and the agent recovers without crashing. That's [Network
Sandboxing](./network-sandbox.md) doing its job.

---

## Step 3 — Add a `path_guard` hook

Hooks are Starlark predicates that run on lifecycle events
(`tool.pre`, `tool.post`, `completion.pre`, `delegation.pre`, …). See
[Writing a Hook](./writing-a-hook.md).

Create `.harness/hooks/path_guard.md`:

```markdown
---
event: tool.pre
priority: 10
script: |
  def run(ctx):
      args = ctx.tool.args or {}
      # Inspect every string-valued arg that smells like a path.
      for key in ("path", "file", "dir", "target"):
          val = args.get(key, "")
          if not val:
              continue
          if val.startswith("/") or val.startswith("\\") or ".." in val:
              return {
                  "decision": "block",
                  "reason": "path_guard: absolute or escaping path '%s' rejected" % val,
              }
      return {"decision": "allow"}
---

# path_guard

Refuses any tool call whose path-like argument is absolute (`/etc/passwd`,
`C:\Windows`) or contains `..` (escapes the working directory). Runs at
`priority: 10` so it gates *before* the policy/audit hooks at priority 1.
```

Verify it fires:

```bash
harness run --config harness.md \
  --allowed-domain "raw.githubusercontent.com" \
  "Use fs.read to read /etc/passwd."
```

You should see the call rejected with `path_guard: absolute or escaping path
'/etc/passwd' rejected`. The agent will explain it cannot do that and
continue.

---

## Step 4 — Tighten the tool policy

Policy is the *registry-level* gate — it runs before the model even sees
that a tool exists. See [Writing a Policy](./writing-a-policy.md).

Edit `harness.md` and replace the `tools_policy` block:

```yaml
tools_policy:
  mode: allowlist
  allow:
    - "fs.read"
    - "fs.list"
    - "web_fetch"
    - "delegate"
    - "delegate_async"
  deny:
    - "fs.*"          # deny beats allow — wildcards too
    - "exec"
    - "meta.*"
```

Two important rules:

- **Deny beats allow.** `fs.read` and `fs.list` are explicitly listed in
  `allow`, so they survive the `fs.*` deny. Anything *else* under `fs.*`
  (notably `fs.remove`, `fs.move`, `fs.write`) is refused at registration
  time.
- **`meta.*` is denied.** That kills the self-augment surface entirely. If
  you want to opt into [self-augmenting agents](../concepts/governance.md),
  you allow `meta.register_tool` *and* add a `meta_tool_guard` hook.

Confirm by listing the active registry:

```bash
harness run --config harness.md --list-tools
```

You should see only `fs.read`, `fs.list`, `web_fetch`, `delegate`,
`delegate_async`. No `fs.remove`, no `exec`, no `meta.*`.

---

## Step 5 — Delegate to a `summarizer` sub-agent

Sub-agents are profiles in `.harness/agents/`. They get their own model,
their own context budget, and their own (usually stricter) policy. See
[Writing a Sub-Agent](./writing-a-sub-agent.md).

Create `.harness/agents/summarizer.md`:

```markdown
---
model:
  provider: copilot
  name: gpt-4o-mini
  api_key_env: GH_TOKEN

context:
  max_history: 10
  max_tokens: 8000

# Summarizer is a leaf agent — no tools, no further delegation.
tools_policy:
  mode: allowlist
  allow: []
  deny:
    - "*"
---

# summarizer

You are **summarizer**, a leaf sub-agent. You receive a block of text and
return a 3-bullet summary plus a one-line takeaway. You have no tools and
cannot delegate further. Be terse.
```

Now ask the parent agent to use it:

```bash
harness run --config harness.md \
  --allowed-domain "raw.githubusercontent.com" \
  "Fetch https://raw.githubusercontent.com/htekdev/ai-harness/main/README.md and delegate to summarizer for a 3-bullet summary."
```

Watch the trace — you'll see a `delegation.execute` span (cheap `gpt-4o-mini`)
nested under the parent (`gpt-4o`). The summarizer cannot fetch anything,
cannot read files, cannot delegate further; it just summarizes the prompt
it was handed. Budgets compose: `delegation.iterations_per_depth: [8]`
caps the parent's loop, and the leaf has its own `max_history: 10`.

---

## Step 6 — Inspect what just happened

Two surfaces give you the truth:

**Audit log.** Add the standard pre/post audit pair from [Writing a
Policy](./writing-a-policy.md) (or copy
[`examples/governed-agent/.harness/hooks/audit_tool_pre.md`](https://github.com/htekdev/ai-harness/tree/main/examples/governed-agent/.harness/hooks)).
Every `tool.pre` and `tool.post` becomes a structured log line you can grep:

```bash
harness run --config harness.md ... 2> audit.jsonl
jq 'select(.event=="tool.pre") | {tool: .tool, decision: .decision}' audit.jsonl
```

`audit.tool.pre - audit.tool.post` is your refusal rate.

**OpenTelemetry.** Run the agent against a local Tempo/Jaeger:

```bash
export HARNESS_OTEL_ENDPOINT=http://localhost:4318
harness run --config harness.md "..."
```

You'll see one span per tool call, one per hook decision, one per
delegation. See [Observability](./observability.md) for the full attribute
schema.

---

## What you've built

| Primitive   | File                                | Concept                                                |
| ----------- | ----------------------------------- | ------------------------------------------------------ |
| System      | `harness.md`                        | [Harness as Code](../concepts/harness-as-code.md)      |
| Tool        | `.harness/tools/web_fetch.md`       | [Tools](../concepts/tools.md)                          |
| Hook        | `.harness/hooks/path_guard.md`      | [Hooks](../concepts/hooks.md)                          |
| Tool policy | `harness.md → tools_policy`         | [Governance & Policy](../concepts/governance.md)       |
| Sub-agent   | `.harness/agents/summarizer.md`     | [Delegation](../concepts/delegation.md)                |

Five files. Every governance primitive expressed as code. Every change is a
PR. Every PR is a diff a human can review.

---

## Where to go next

- **Harden it.** Add the rest of the [Writing a Policy](./writing-a-policy.md)
  hook stack: `command_guard`, `meta_tool_guard`, `completion_window_guard`,
  the `audit_tool_pre`/`audit_tool_post` pair.
- **Test it.** Wrap the agent in [evals](./testing.md) so each PR runs the
  full governance suite in CI.
- **Ship it.** Move to a [Production Deployment](./deployment.md) recipe
  with systemd, OTel, and rate limits.
- **Read the flagship.** The [Governed Agent
  example](../examples/governed-agent.md) is the same idea taken to its
  logical conclusion: every Phase 5 primitive, all wired together, copy-paste
  runnable.

If you completed this guide, you have the full mental model for building
*any* governed harness. The rest is just adding more files.
