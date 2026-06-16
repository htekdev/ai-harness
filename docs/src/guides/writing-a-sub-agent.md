# Writing a Sub-Agent

> A hands-on tutorial. By the end of this guide you'll have written a
> named **researcher** sub-agent profile, called it from a parent via the
> built-in `delegate` tool, gated the call with a `delegation.pre` hook,
> and audited the result with `delegation.post`. Every example runs
> against the same harness binary you used in the
> [Quickstart](../getting-started.md).

This guide assumes you finished the [Quickstart](../getting-started.md),
the [Writing a Tool](./writing-a-tool.md) tutorial, and the
[Writing a Hook](./writing-a-hook.md) tutorial. We'll reuse the
`tool.pre` / `tool.post` ternary you already know — `allow / block /
modify` — and apply it one level up, to whole sub-agent calls.

If you haven't read the [Delegation concept page](../concepts/delegation.md),
skim it first. This guide assumes you understand that a delegate is a
**runtime primitive**, not a separate process — same hook dispatcher,
same sandbox, same audit trail, one level deeper.

## What a sub-agent actually is

A sub-agent is a typed artifact stored at `.harness/agents/<name>.md`.
It declares everything the parent needs to spawn a focused child:

```
.harness/
└── agents/
    └── researcher.md     ← a sub-agent profile
```

The frontmatter is the contract:

| Field         | Purpose                                                          |
|---------------|------------------------------------------------------------------|
| `model`       | Override the parent model for this child (optional)              |
| `description` | Short summary the parent's planner sees in the tool catalog      |
| `tools`       | Inline tools, or names of tools defined elsewhere in the harness |
| `hooks`       | Inline hooks, or names of hooks already on disk                  |

The Markdown body is the system prompt the child runs under. That's
the whole surface. No registration step, no separate runtime config.
Drop the file in `.harness/agents/`, and the parent can call it.

## 1. Set up

If you don't already have a workspace:

```bash
mkdir -p my-agent && cd my-agent
harness init .
```

`harness init` scaffolds `.harness/harness.md`, the four starter tools,
and a `tools/` and `hooks/` directory. Add an `agents/` directory:

```bash
mkdir -p .harness/agents
```

That's the only structural change required to start delegating.

## 2. Write your first sub-agent profile

Create `.harness/agents/researcher.md`:

```markdown
---
model: gpt-4o-mini
description: Researches topics via HTTP and summarizes findings concisely

tools:
  - name: fetch_url
    parameters:
      url: { type: string, required: true }
    script: |
      def run(args):
          return http.get(args["url"], {}, 30)
  - name: search_text
    parameters:
      text:    { type: string, required: true }
      pattern: { type: string, required: true }
    script: |
      def run(args):
          matches = re.find_all(args["pattern"], args["text"])
          return json.encode(matches)

hooks: []
---

# Researcher

You are a research agent. Gather information from URLs, extract
relevant data, and summarize findings clearly and concisely.

## Guidelines

- Always cite your sources (include URLs)
- Summarize findings in structured format
- If a URL fails, try alternative sources
- Be thorough but concise
```

A few things to notice:

1. **Tools are inline.** They use the exact same artifact schema as
   `tools/word_count.md` from the [Writing a Tool](./writing-a-tool.md)
   guide — `name`, `parameters`, `script`. There is no "agent DSL."
2. **Hooks is empty.** This delegate inherits the parent's hook chain.
   Every `tool.pre` / `tool.post` policy you've already written runs
   inside this child too — without you touching it.
3. **The body is the system prompt.** It is plain Markdown. The harness
   passes it verbatim as the child's system message.

Validate the artifact:

```bash
harness validate
```

You should see `researcher` listed under **agents** alongside any
tools and hooks you already have.

## 3. Call the sub-agent from the parent

Delegation is exposed to the model as a single built-in tool named
`delegate`. The parent calls it like any other tool:

```json
{
  "tool": "delegate",
  "args": {
    "agent": "researcher",
    "task": "Summarize the three highest-priority CVEs in https://example.com/security/release-notes"
  }
}
```

You don't write that JSON by hand — the parent's planner does. To
exercise it interactively:

```bash
harness run "Use the researcher sub-agent to summarize the security
release notes at https://example.com/security/release-notes."
```

The runtime:

1. Resolves `researcher` from `.harness/agents/researcher.md`.
2. Spawns a child runtime at `depth = parent.depth + 1`.
3. Runs the child's turn loop, capped by the per-depth iteration
   budget (default `[20, 10, 5, 3]`).
4. Returns the child's final answer to the parent's `delegate`
   tool result.

The parent never sees the child's intermediate tool calls in its
own context window — only the final structured result. That is the
point: **a sub-agent is a context-isolation primitive**.

## 4. Add a `delegation.pre` guard

Every `delegate` call traverses the full hook chain. Two events
bracket the call: `delegation.pre` (after argument validation, before
the child runs) and `delegation.post` (after the child returns,
before the parent sees the result).

Write a guard at `.harness/hooks/researcher_guard.md` that blocks
research tasks that look suspicious:

```markdown
---
event: delegation.pre
priority: 10
when: payload.agent == "researcher"

script: |
  def handle(event, payload):
      task = payload.get("task", "")
      if "internal" in task.lower() or "confidential" in task.lower():
          return block("researcher cannot be asked about internal/confidential topics")
      return allow()
---
```

Three things this hook demonstrates:

- **Subscription is declarative.** `event: delegation.pre` is the
  whole subscription. You don't register the hook anywhere.
- **`when:` filters scope.** This hook only fires on `delegate`
  calls targeting the `researcher` agent. Calls to other agents
  skip it entirely.
- **The verdict is the same ternary.** `allow()`, `block(reason)`,
  and `modify(payload)` work here exactly as they do in
  `tool.pre`.

Re-run the agent with a task containing "confidential" and observe
the call get blocked before the child ever spawns.

## 5. Audit results with `delegation.post`

Add `.harness/hooks/researcher_audit.md`:

```markdown
---
event: delegation.post
priority: 50
when: payload.agent == "researcher"

script: |
  def handle(event, payload):
      result = payload.get("result", "")
      tool_calls = payload.get("tool_calls", 0)
      log.info("researcher delegation completed", {
          "tool_calls": tool_calls,
          "result_len": len(result),
      })
      return allow()
---
```

`delegation.post` runs *after* the child returns but *before* the
parent's `delegate` tool result is materialized. That gives you a
single place to:

- Redact secrets the child may have accidentally surfaced.
- Summarize a long result before it bloats the parent's context.
- Reject results that fail a quality bar (`block(...)` returns an
  error to the parent's `tool.post` chain).
- Emit metrics or audit log entries for compliance review.

## 6. Inline delegates (no profile required)

Sometimes a sub-agent is a one-shot — a focused, single-use bundle
the parent assembles at call time. The `delegate` tool accepts inline
`tools` and `hooks` directly:

```json
{
  "tool": "delegate",
  "args": {
    "task": "Extract all CVE IDs from this changelog and return them as JSON.",
    "tools": [
      { "name": "regex_extract",
        "parameters": { "text": { "type": "string", "required": true },
                        "pattern": { "type": "string", "required": true } },
        "script": "def run(args):\n    return json.encode(re.find_all(args['pattern'], args['text']))" }
    ],
    "hooks": []
  }
}
```

Inline delegates use the **same artifact schema** as files on disk.
They go through the same validator, the same hook chain, and the
same depth/iteration budgets. The only difference is they live for
the duration of the call.

When to prefer one over the other:

| Pattern              | Use when                                                              |
|----------------------|-----------------------------------------------------------------------|
| Named profile (file) | Reusable role across many calls; you want the prompt under review.    |
| Inline bundle (call) | One-shot decomposition; tools are derived from the task itself.       |

## 7. Composition patterns

The same primitive composes into three shapes you'll see repeatedly:

**Sequential (chain).** Each delegate finishes before the next begins.
Use when stages have *different* skills and the output of one is the
input of the next.

```
researcher → writer → reviewer
```

**Parallel (fan-out).** Use `delegate_async` to spawn multiple
delegates concurrently; the parent collects results. Use when work is
independent and latency matters more than determinism.

```
parent
 ├─ scout-A (parallel)
 ├─ scout-B (parallel)
 └─ scout-C (parallel)
```

**Recursive (tree).** A decomposer splits a problem and delegates each
sub-problem; sub-agents may decompose further, up to `max_depth`. Use
when problem shape is unknown ahead of time.

```
decomposer
 ├─ subtask-1
 │   ├─ subtask-1.1
 │   └─ subtask-1.2
 └─ subtask-2
```

In all three, every tool call inside every delegate at every depth
runs through the same hook chain. Governance does not weaken with
depth — only the iteration budget does.

## 8. Depth, iterations, and budgets

Recursion is allowed. Unbounded recursion is not. The runtime enforces
two limits by default:

```
MaxDelegationDepth        = 3   // levels of nesting
MaxDelegateToolIterations = 5   // tool-call loops per delegate
```

Override per-harness in `harness.md`:

```yaml
delegation:
  max_depth: 3
  max_concurrent: 5
  iterations_per_depth: [20, 10, 5, 3]
  timeout_ms: 300000
  allow_recursive: true
```

Iteration budgets *decrease* with depth. The shape forces sub-agents
to stay focused, prevents infinite trees, and caps the worst-case
token blast radius of any single root turn. When a delegate hits the
depth limit, the runtime returns `errs.KindDelegation` —
*"delegation depth limit reached"* — and the parent's `tool.post`
hooks decide how to react.

## 9. Observability

Every `delegate` call emits a `delegation.execute` OTel span with
attributes for agent name, depth, model, task length, tools count, and
the number of tool calls the child actually made. Pair it with the
`tool.pre` / `tool.post` spans that fire *inside* the delegate and
you get a full traceable record of every decision in the tree.

```bash
docker compose -f data/examples/otel-jaeger-compose.yml up
```

Run the [governed-agent](../examples/governed-agent.md) example
against this collector and you can watch a recursive delegation
tree render live as a flame graph.

## What to write next

Once you've shipped a researcher, the next sub-agents practically
write themselves. A few starter shapes worth keeping around:

- **`code-writer.md`** — inherits `read_file` / `write_file` /
  `edit_file` / `run_command` and a `path_guard` hook; system prompt
  enforces "build before declaring done."
- **`reviewer.md`** — read-only tool surface, `delegation.post`
  hook that re-prompts on low-confidence verdicts (see
  [Verification](../concepts/verification.md)).
- **`decomposer.md`** — single tool: `delegate`. The whole job is
  to fan work out into other sub-agents.

Each one is a single Markdown file. Each one runs under the same
governance pipeline as the parent. That is the shape of harness
engineering: **one capability bundle per file, composition by
reference, governance in the middle**.

## Recap

- A sub-agent is `.harness/agents/<name>.md` with frontmatter
  (`model`, `description`, `tools`, `hooks`) and a Markdown body.
- The parent calls it via the built-in `delegate` tool; arguments
  are typed, the result is structured.
- `delegation.pre` and `delegation.post` hooks bracket every call
  with the same `allow / block / modify` ternary you already know.
- Inline delegates use the same artifact schema for one-shot
  decomposition.
- Depth and iteration budgets are enforced by the runtime.
- Every call is OTel-instrumented; every nested tool call inherits
  the parent's hook chain.

Next: read the [Verification concept](../concepts/verification.md)
to learn how to gate delegate results on a third event —
`delegation.post_verify` — that re-prompts the child on a failed
verdict.
