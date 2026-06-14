# Delegation

> Delegation is the primitive that lets an agent spawn a focused **sub-agent**
> with a scoped task, a custom tool/hook bundle, a depth-limited budget, and
> the *same* governance pipeline as its parent.

If [tools](./tools.md) are *what an agent can do* and [hooks](./hooks.md) are
*what the harness rules on*, delegation is *how an agent recruits help* —
without escaping the harness. A delegate inherits the lifecycle, the
sandbox, and the audit trail. It is not a fork, not a thread, not a separate
process talking to a separate runtime. It is the same harness, one level
deeper.

## What delegation is

A delegation is a single call against the runtime that:

1. **Resolves a target.** Either a named agent profile from
   `.harness/agents/<name>/` or an inline `tools` + `hooks` bundle supplied
   by the parent at call time.
2. **Allocates a child runtime.** Same model interface, same Starlark
   sandbox, same hook dispatcher — at `depth = parent.depth + 1`.
3. **Runs a bounded turn loop.** Capped by both a global recursion depth
   and a per-depth iteration budget.
4. **Returns a structured result.** Final response, tool calls, tool
   results, and span attributes — to the parent's hook chain.

Concretely, the delegation request is a typed Go struct (`delegation.Request`)
with a small, reviewable surface:

```go
type Request struct {
    Task         string     // what the delegate should accomplish
    Agent        string     // optional: named agent profile to load
    Model        string     // optional: model override
    Tools        []ToolSpec // inline tools the delegate can call
    Hooks        []HookSpec // inline hooks the delegate runs under
    SystemPrompt string     // optional override
}
```

Tools and hooks declared inline use the *same* artifact schema as files on
disk — name, description, parameters, Starlark `script`. There is no
"delegation DSL." A delegate's tools are tools; its hooks are hooks. Files
or inline, the contract is the same.

## The built-in `delegate` tool

Delegation is exposed to the model as a single named tool (`delegate`) plus
an async sibling (`delegate_async`). Both are first-class members of the
tool catalog — they are filtered by `tool.pre`, audited by `tool.post`, and
deny-listable by any policy hook in the stack. There is no privileged path.

The model's eye-view of a delegation looks identical to any other tool
call:

```json
{
  "tool": "delegate",
  "args": {
    "task": "Summarize the three highest-priority CVEs in the last release notes.",
    "agent": "researcher",
    "tools": [
      { "name": "fetch_cve", "description": "...", "parameters": {...},
        "script": "def run(args): ..." }
    ],
    "hooks": [
      { "event": "tool.post", "priority": 10,
        "script": "def handle(event, payload): ..." }
    ]
  }
}
```

The harness then takes over.

## The two delegation events

Delegation participates in the same hook lifecycle as every other operation.
Two events bracket the call:

| Event              | When it fires                                         | Typical use                                                   |
|--------------------|-------------------------------------------------------|---------------------------------------------------------------|
| `delegation.pre`   | After argument validation, before the child runs      | Deny dangerous agents, scrub secrets from the task, cap depth |
| `delegation.post`  | After the child returns, before parent sees the result | Redact, summarise, attach metrics, gate on the result         |

A `delegation.pre` hook can `block(reason)` the call entirely, `modify` the
request (rewrite the task, swap the agent, drop a tool from the bundle), or
`allow()` it to proceed. The same `allow / block / modify` ternary you
learned in [hooks](./hooks.md) — the contract does not change just because
the operation is "spawn a whole new agent."

> **Coming primitive:** [`delegation.post_verify`][i103] adds a third event
> that fires *between* the child's response and `delegation.post`. It runs
> verification hooks declared on the delegate's artifacts, returns
> `errs.KindVerificationFailed` on a `block`, and re-prompts up to a
> configured retry budget. The page you are reading now describes the
> contract that primitive will plug into.

[i103]: https://github.com/htekdev/ai-harness/issues/103
[i104]: https://github.com/htekdev/ai-harness/issues/104

## Depth, iterations, and budgets

Recursion is allowed. Unbounded recursion is not. Two limits work together:

```go
const MaxDelegationDepth        = 3   // levels of nesting
const MaxDelegateToolIterations = 5   // tool-call loops per delegate
const MaxToolRetries            = 2   // per-tool retry budget
```

These are defaults. A harness can override them in `harness.md` or in a
`DelegatorConfig`:

```yaml
delegation:
  max_depth: 3
  max_concurrent: 5
  iterations_per_depth: [20, 10, 5, 3]
  timeout_ms: 300000
  allow_recursive: true
```

The shape that matters is **`iterations_per_depth`**. Budgets *decrease*
with depth:

```
depth 0 — root agent           (20 tool iterations)
  └─ depth 1 — sub-agent       (10 iterations)
       └─ depth 2 — sub-sub    ( 5 iterations)
            └─ depth 3 — leaf  ( 3 iterations)
```

Decreasing budgets do three things at once: prevent infinite trees, force
sub-agents to stay focused, and cap the worst-case token blast radius of
any single root turn. When `currentDepth >= maxDepth`, the runtime returns
`errs.KindDelegation` with a structured *"delegation depth limit reached"*
message — the parent's `tool.post` hooks see it like any other error and
can decide how to react.

## Composition patterns

The same primitive composes into three recognisable shapes.

**Sequential (chain).** Each delegate completes before the next begins.

```
researcher → writer → reviewer
```

Use when stages have *different* skills and the output of one is the input
of the next.

**Parallel (fan-out).** `delegate_async` spawns multiple delegates that run
concurrently; the parent collects results.

```
parent
 ├─ scout-A (parallel)
 ├─ scout-B (parallel)
 └─ scout-C (parallel)
```

Use when the work is independent and latency matters more than determinism.

**Recursive (tree).** A decomposer splits a problem and delegates each
sub-problem; sub-agents may decompose further, up to `max_depth`.

```
decomposer
 ├─ subtask-1
 │   ├─ subtask-1.1
 │   └─ subtask-1.2
 └─ subtask-2
```

Use when problem shape is unknown ahead of time and depth is the natural
control surface.

In all three, the *governance* path is identical: every tool call, in
every delegate, at every depth, traverses the same hook chain.

## Delegation observability

Delegation is an OTel-instrumented operation. Every call emits a
`delegation.execute` span with these attributes:

| Attribute                | Meaning                                              |
|--------------------------|------------------------------------------------------|
| `delegation.agent`       | Named agent (or empty for inline)                    |
| `delegation.depth`       | Parent depth at entry                                |
| `delegation.model`       | Model the child is running on                        |
| `delegation.task_len`    | Length of the task string                            |
| `delegation.tools_count` | How many tools the delegate received                 |
| `delegation.tool_calls`  | How many calls the delegate actually made (on success) |

Pair that with the existing `tool.pre` / `tool.post` audit hooks — which
fire inside the delegate the same way they fire inside the parent — and
you get a full traceable record of every decision in the tree, indexable
in Jaeger or any OTel collector.

> Run `docker compose -f data/examples/otel-jaeger-compose.yml up` against
> the [governed-agent](../examples/governed-agent.md) example and you can
> watch a recursive delegation tree render live as a flame graph.

## Why delegation is a primitive, not a tool you bring

Many agent frameworks treat sub-agents as something the *application*
implements: spin up another runtime, marshal a prompt, parse a response.
That works until you ask three questions:

1. **What policy applies inside the sub-agent?** If it is a separate
   process, your hook stack does not run there. The deny-list you carefully
   reviewed in `.harness/hooks/` is silently bypassed.
2. **What budget does the sub-agent share?** If iteration counts and depth
   live in the application, every team writes their own broken version of
   them.
3. **What does the audit trail look like?** If the sub-agent is its own
   binary, your `turn.end` traces stop at the parent.

AI Harness answers all three by making delegation a **runtime primitive**:

- The same hook dispatcher runs in parent and child.
- Depth, iteration, and retry limits are enforced by the runtime, not the
  caller.
- The OTel span hierarchy crosses the parent/child boundary natively.

The cost of this discipline is a small one: a delegate cannot do anything
the harness has not been told to allow. That is the point.

## Inheritance and isolation

A delegate is a *child*, not a *clone*. The runtime makes deliberate
choices about what crosses the boundary:

| Surface                  | Inherited? | Notes                                                             |
|--------------------------|------------|-------------------------------------------------------------------|
| Hook stack               | ✅         | Parent hooks run on child's `tool.pre` / `tool.post` / `turn.*`   |
| Tool catalog             | ❌ (opt-in) | Child gets only the tools the request specifies                   |
| Filesystem sandbox       | ✅         | Same `path_guard` / `command_guard` posture as parent             |
| Network allowlist        | ✅         | Inherited from harness config                                     |
| Memory / context         | ❌         | Child gets the task string and system prompt; nothing else        |
| Metrics namespace        | ✅         | `metrics.incr` aggregates across the whole tree                   |
| Cache                    | ✅         | Per-run KV cache is shared parent ↔ child                         |

The default of "child gets *only* the tools the parent passes in" is what
makes `delegate` safe to put in front of a model. A misbehaving delegate
cannot reach for tools its parent never named.

## Delegation versus agents-as-tools versus orchestration

Three nearby ideas, often conflated:

- **Agents-as-tools** wraps another agent behind a single tool call with no
  recursion, no shared budget, and no shared hooks. Useful, but flat.
- **External orchestration** (Temporal, Airflow, a workflow engine) runs
  agents as black-box steps in a DAG. The orchestrator owns control flow;
  the harness sees nothing.
- **Delegation** keeps control flow *inside the harness*. The model
  decides when to delegate, the harness decides whether and how, and the
  audit trail is one continuous trace.

The first two are valid; AI Harness can participate in either. Delegation
is what you reach for when the control flow itself is part of the agent's
job — when the *decomposition* is the work — and you want it governed.

## Delegation execution lifecycle

Every call follows this sequence:

1. **Validate.** Parse the `Request`; reject empties; resolve `Agent` to a
   named profile if one was given.
2. **Pre-check.** Compare `currentDepth` to `maxDepth`; short-circuit with
   `errs.KindDelegation` if exceeded.
3. **Pre-hooks.** Dispatch `delegation.pre` through the parent's hook
   chain. `block` short-circuits; `modify` rewrites the request.
4. **Compose child.** Build a child runtime with the resolved tools, the
   inherited hook stack, the bounded iteration budget, and the OTel span.
5. **Run.** Drive the child's turn loop up to its iteration cap.
6. **Post-hooks.** Dispatch `delegation.post` with the structured result;
   apply any redactions or rewrites.
7. **Return.** Hand the (possibly modified) `Result` back to the parent's
   tool dispatcher, which threads it into the parent's next `tool.post`.

Steps 3 and 6 are where governance lives. Steps 2 and 4 are where budgets
live. There is no step where the harness disengages.

## What to read next

- **[Governance & Policy](./governance.md)** — how delegation hooks compose
  with tool hooks into a single deployable policy posture.
- **[Hooks](./hooks.md)** — for the underlying `allow / block / modify`
  contract that every delegation event uses.
- **[Tools](./tools.md)** — for the artifact schema that inline `ToolSpec`
  shares with files on disk.
- **Reference:** the [Hook Artifact Schema](../reference/hook-artifact.md)
  documents `delegation.pre` and `delegation.post` payload shapes, and
  forward-references the upcoming [`delegation.post_verify`][i103] and
  [`agent.stop`][i104] events.
