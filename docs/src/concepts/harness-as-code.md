# Harness as Code

> The harness — the layer between your model and your tools — is software.
> It deserves the same engineering rigor you give the rest of your codebase.

This page explains the core thesis behind AI Harness. Read it once, and the
rest of the docs (tools, hooks, delegation, governance) will line up around
the same axis.

## The problem: invisible harnesses

Every AI agent runs inside a *harness* — the layer that decides:

- which **system prompt** the model sees,
- which **tools** are available and how their results come back,
- which **policies** apply (allowlists, deny rules, depth limits),
- which **observability hooks** fire around each call,
- and which **state** survives across turns.

In most agent frameworks, that layer is hidden inside SDK internals, embedded
in editor plugins, or scattered across YAML, environment variables, and
hard-coded defaults. You can use the agent, but you can't review it. You
can't diff it. You can't promote a behavior change from staging to production
the way you would a normal code change.

That is the problem AI Harness exists to solve.

## The thesis

> **The harness should be code.** Declarative, versioned, composable, and
> reviewable — exactly like the infrastructure that runs it.

We call this **Harness as Code**. It is a deliberate echo of *Infrastructure
as Code*: the discipline that took ops out of ticket queues and into Git.
The same shift is overdue for agent runtimes.

A harness-as-code system has four properties:

1. **Declarative.** You describe what the agent *is* — its prompt, its tools,
   its hooks, its policies — not the imperative glue that wires them up.
2. **Composable.** Behavior is built from small, single-purpose artifacts
   that can be added, removed, or overridden without touching the core.
3. **Versioned.** Every artifact lives in your repo, on a branch, behind a
   PR. No hidden config screens. No "drift" between environments.
4. **Reviewable.** A teammate can read one file and understand exactly what
   it changes. Tools, hooks, and policies are diffable Markdown.

## Every harness is biased

It is tempting to claim a harness is "neutral." None of them are. Every
harness is biased toward *something* — and that bias shapes which agents are
easy to build inside it and which fight the runtime at every step.

| Harness          | Optimized for                                       |
|------------------|------------------------------------------------------|
| GitHub Copilot   | The GitHub ecosystem, VS Code, and Actions          |
| Claude Code      | Anthropic models and Anthropic's API surface        |
| Codex CLI        | OpenAI frontier models and OpenAI tool-call shapes  |
| Pi               | Minimal terminal coding, TypeScript extensibility   |
| **AI Harness**   | **Extensibility — Harness as Code**                 |

AI Harness's bias is explicit: we optimize for your ability to **define,
compose, and evolve harness behavior as code** — across providers, across
environments, across teams.

That is the trade we are willing to make. We will not be the most opinionated
chat experience. We will be the harness that survives a code review, a model
swap, and a security audit.

## What that looks like in practice

A working AI Harness project is a directory:

```
your-repo/
├── harness.md                       # system prompt + frontmatter
└── .harness/
    ├── tools/                       # one file per tool
    │   ├── web_fetch.md
    │   └── run_command.md
    └── hooks/                       # one file per cross-cutting policy
        ├── audit_tool_pre.md
        ├── command_guard.md
        └── path_guard.md
```

Every file is a typed artifact:

- **`harness.md`** is the root artifact. Its frontmatter declares the model,
  retry policy, tool policy, delegation depth, and which built-ins are
  enabled. Its body is the system prompt.
- **Tool artifacts** declare a single tool — its name, schema, sandbox, and
  Starlark implementation — in one file.
- **Hook artifacts** declare a single policy or observability concern — what
  event it listens to, what priority it runs at, and what it does — in one
  file.

This is the entire mental model. There is no separate "framework config,"
no global registry, no plugin manifest to keep in sync. **One file = one
capability bundle.**

## The primitives

AI Harness elevates four things to first-class primitives in the core
runtime — not as plugins, not as add-ons, but as part of the artifact model.

### 1. Tools

Tools are not "functions you happen to register." They are versioned,
sandboxed artifacts with declared schemas, declared side effects, and a
clear execution boundary. See [Tools](./tools.md).

### 2. Hooks

Hooks are how you express **policy**, **audit**, and **shape** without
forking the core. They run at well-known points in the execution graph
(`tool.pre`, `tool.post`, `completion.pre`, `delegate.pre`, …), they have
priorities, and they can soft-block or hard-block calls. See [Hooks](./hooks.md).

### 3. Delegation

Sub-agents are a primitive, not a pattern you reinvent. The core enforces
delegation depth, propagates governance, and surfaces the delegation tree as
an inspectable structure. See [Delegation](./delegation.md).

### 4. Governance

Policies — allowlists, deny rules, network sandboxes, command guards, meta
tool guards — compose at the artifact layer and are evaluated **per turn**,
not just at startup. Behavior changes don't require restarts; they require
edits. See [Governance & Policy](./governance.md).

## Per-turn evaluation

A subtle but important property: AI Harness re-evaluates active artifacts on
every turn. That means:

- Hooks added mid-session take effect immediately.
- A policy change in an artifact applies to the *next* tool call, not the
  next process restart.
- Conditional artifacts (e.g., "this hook only fires when `env == prod`")
  resolve dynamically against the current run context.

This is what makes a *small* core viable: composition does the heavy lifting,
not configuration flags inside the runtime.

## Context observability

Most harnesses treat the context window as an opaque blob — a thing the
model sees, that you mostly don't. AI Harness treats it as a product
surface:

- Every artifact's contribution to the prompt is attributable.
- Tool results, hook outputs, and delegated sub-agent transcripts are
  inspectable.
- OpenTelemetry spans wrap each phase of the turn so you can see *why* the
  context looks the way it does.

If you have ever debugged an agent by `printf`-ing the entire prompt, you
already know why this matters.

## What Harness as Code is *not*

To keep the term sharp:

- **Not "another YAML format."** Artifacts are typed, versioned Markdown.
  Frontmatter carries structured config; the body carries the prompt or
  the Starlark implementation.
- **Not "a plugin marketplace."** The bias is toward composition in *your*
  repo, not toward a third-party ecosystem of opaque packages.
- **Not "the biggest framework."** The core stays small on purpose. The
  power lives at the edges, in artifacts you and your team write.
- **Not "lock-in to one model."** Provider and model selection are
  artifacts too. Swapping `gpt-4o` for `claude-3.5-sonnet` is a frontmatter
  change, not a refactor.

## Why this matters now

Three things have shifted:

1. **Models have stopped being the bottleneck.** The bottleneck is now the
   *systems we wrap around models* — and those systems are wildly under-engineered.
2. **Agents are entering production.** "It worked in the demo" is no longer
   acceptable. Auditability, reproducibility, and governance are table
   stakes.
3. **Harnesses outlive models.** The model you ship with today will be
   replaced inside a year. The harness you build around it should not be.

Harness as Code is the discipline that makes those three things tractable.

## Where to go next

- **See it in one screen** → [Quickstart](../getting-started.md)
- **Read the flagship reference** → [Governed Agent example](../examples/governed-agent.md)
- **Go deeper on the primitives** → [Tools](./tools.md), [Hooks](./hooks.md),
  [Delegation](./delegation.md), [Governance & Policy](./governance.md)
