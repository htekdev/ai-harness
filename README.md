# Harness as Code

**Declarative AI agent governance in Go.** Define tools, hooks, delegation rules, and entire sub-agents in version-controlled Markdown files — reviewable in PRs, testable in CI, reproducible across environments.

> *Like Infrastructure as Code, but for AI agent behavior.*
> Every prompt ships with its governance. Every agent behavior is reproducible, reviewable, and testable.

[![CI](https://github.com/htekdev/ai-harness/actions/workflows/ci.yml/badge.svg)](https://github.com/htekdev/ai-harness/actions/workflows/ci.yml)
[![Go](https://img.shields.io/badge/Go-1.25+-00ADD8?logo=go&logoColor=white)](https://go.dev)
[![Release](https://img.shields.io/github/v/release/htekdev/ai-harness?include_prereleases)](https://github.com/htekdev/ai-harness/releases)
[![License](https://img.shields.io/badge/License-MIT-green.svg)](LICENSE)

---

## The Problem

Most agent frameworks force a choice:

| Approach | Tradeoff |
|----------|----------|
| Rigid plugin systems | Hard to customize, vendor lock-in |
| Raw LLM wrappers | No guardrails, no governance |
| Python mega-frameworks | 200+ deps, hidden state, slow iteration |

**Harness as Code** takes a different approach: your `harness.md` file IS the control plane for an AI agent — governance, tools, delegation limits, and system prompt in one reviewable artifact.

## Naming and terminology direction

To avoid blocking architecture work while product naming evolves:

- Keep **AI Harness / Harness as Code** as the umbrella product surface.
- Keep internal schema language descriptive and artifact-first: **artifacts**, **definitions**, **events**, **triggers**, **watchers**, **schedules**, **runtimes**.
- Do not position **extensions** as the primary abstraction in new copy.

### Alternatives to "extensions"

- capability artifacts (recommended default)
- definition bundles
- runtime modules
- plugin artifacts

### Current recommendation

Use **artifacts/definitions** as the core noun set now, and treat any `extension` wording as compatibility terminology rather than the long-term conceptual center.

## What Makes It Different

- **Markdown-first** — `harness.md` (YAML frontmatter + body as system prompt) defines your agent declaratively. `.harness/` directory adds tools, hooks, and sub-agents as individual `.md` files.
- **Governed by default** — Hooks enforce safety through architecture, not instructions. You don't make agents trustworthy by writing better prompts — you make them trustworthy by engineering harnesses where wrong behavior is architecturally impossible.
- **Self-extending** — The `delegate` meta-tool lets agents create tools and spawn sub-agents recursively at runtime.
- **Minimal** — Single Go binary, ~5 dependencies, compiles in seconds. `tools.Handler` is just `func(ctx, args) (string, error)`.
- **Portable** — Works with GitHub Copilot, OpenAI, or any compatible chat completions API.
- **Testable** — Built-in eval framework validates agent behavior against real models in CI.

### Differentiation from OpenHarness (Category-Level)

OpenHarness and AI Harness both operate in the **agent harness / agent infrastructure** category.

Where AI Harness is intentionally different:

- **Minimal core as a product constraint** — optimize for a small, inspectable runtime over broad platform surface area.
- **Composable artifacts as primary interface** — behavior is centered on versioned Markdown artifacts (`harness.md` + `.harness/**`), designed for PR review.
- **Governance in the execution path** — hooks, policies, approval gates, and delegation limits are first-class controls, not add-ons.

Language we avoid:

- “OpenHarness but in Go”
- “OpenHarness clone”
- “Drop-in replacement for OpenHarness”

Preferred framing:

- “A minimal, governance-first harness for coding agents”
- “Harness as code: composable artifacts + explicit operational controls”
- “Built for teams that want predictable, reviewable agent behavior”

Messaging framework (reuse for docs/launch copy):

- **One-liner:** “AI Harness is a minimal, governance-first runtime for coding agents, where behavior lives in composable, versioned artifacts.”
- **Three pillars:** Minimal core • Composable artifacts • Governance by default
- **30-second pitch:** “AI Harness helps teams move agent behavior into files they can review, validate, and govern. Instead of a large opaque framework, it provides a compact runtime with explicit lifecycle controls.”

### Core Philosophy

> Make the right thing to do the easy thing to do.

The harness enforces safety through architecture:
- `fs.replace` **fails** if the match isn't unique (forces surgical edits)
- Recursive delegation is depth-limited (configurable, hard cap at 5)
- Iterations decrease per depth level (20 → 10 → 5 → 3)
- Retry guards auto-block tools after 2 consecutive errors
- Path operations are jailed to the working directory at the Go level
- Hooks run at every lifecycle point — blocking is a first-class action

### The DevOps Parallel

| DevOps Gave Humans | Harness as Code Gives Agents |
|---|---|
| Infrastructure as Code | Agent governance as code |
| CI/CD pipelines | Agent loops with termination and retry |
| Deployment gates | Autonomy levels and approval gates |
| Git hooks | Pre-tool hookflows |
| RBAC / least privilege | Tool registry access control |
| Observability | Agent event streams and metrics |

## Harness Levels (Progressive Sophistication)

Harness levels are an adoption model for onboarding and positioning.  
They answer: *"How much governance depth is applied to this agent system right now?"*

- **Primary axis:** governance depth
- **Secondary effects:** capability breadth and team maturity
- **Goal:** let teams start simple, then add controls incrementally without re-platforming

| Level | Intent | Differentiator | Typical user stage | Shipped in repo today | Planned evolution |
|---|---|---|---|---|---|
| **L1 — Prompt + Basic Tools** | Get productive quickly with minimal setup | Single harness identity + direct tool use | Individual prototyping | `harness.md`, core tool handlers, `harness run`, `harness validate` | Better starter templates and guided bootstrap |
| **L2 — Structured Capabilities** | Decompose work into reusable artifacts | File-based `.harness/` tools/hooks/agents + composition | Team adoption | Directory loader, artifact composition, `harness inspect`, examples library | Richer capability packs and opinionated bundles |
| **L3 — Governed Autonomy** | Enforce safe default behavior as architecture | Lifecycle hooks, policy-style blocking, bounded delegation | Production hardening | Hook system (`*.pre`/`*.post`), command/path guard patterns, delegation depth + retry limits | More policy packs, stronger default guardrails, approval-gate presets |
| **L4 — Observable, Adaptive Operations** | Operate agent systems as a managed platform | Runtime signals drive governance and rollout decisions | Org-scale operations | Event streams/metrics + eval framework foundations | Maturity scoring, progressive rollout controls, roadmap-level operational dashboards |

This model is intentionally progressive: teams can adopt L1 immediately, harden with L2/L3, then scale with L4.

## Architecture

```
┌─────────────────────────────────────────────────────────────────┐
│                     HARNESS AS CODE                               │
├─────────────────────────────────────────────────────────────────┤
│                                                                   │
│  ┌──────────┐    ┌──────────┐    ┌──────────────┐               │
│  │  Config  │───▶│  Agent   │───▶│  Completion  │               │
│  │ (MD+Dir) │    │  Loop    │    │  Client      │               │
│  └──────────┘    └────┬─────┘    └──────────────┘               │
│                       │                                           │
│       ┌───────────────┼───────────────┐                          │
│       ▼               ▼               ▼                          │
│  ┌──────────┐   ┌──────────┐   ┌──────────────┐                │
│  │  Hook    │   │  Tool    │   │   Context    │                │
│  │  System  │   │ Registry │   │   Manager    │                │
│  └──────────┘   └────┬─────┘   └──────────────┘                │
│                       │                                           │
│       ┌───────────────┼───────────────┐                          │
│       ▼               ▼               ▼                          │
│  ┌──────────┐   ┌──────────┐   ┌──────────────┐                │
│  │ Starlark │   │ Delegate │   │  fs/edit     │                │
│  │ Engine   │   │  System  │   │  Built-ins   │                │
│  └──────────┘   └──────────┘   └──────────────┘                │
│                       │                                           │
│       ┌───────────────┼───────────────┐                          │
│       ▼               ▼               ▼                          │
│  ┌──────────┐   ┌──────────┐   ┌──────────────┐                │
│  │  Model   │   │  Agent   │   │   Task       │                │
│  │ Registry │   │ Registry │   │   Store      │                │
│  └──────────┘   └──────────┘   └──────────────┘                │
│                                                                   │
└─────────────────────────────────────────────────────────────────┘
```

## Installation

### CLI (recommended)

```bash
go install github.com/htekdev/ai-harness/cmd/harness@latest
```

### Pre-built binaries

Download from [Releases](https://github.com/htekdev/ai-harness/releases) — available for Linux, macOS, and Windows (amd64 + arm64).

```bash
# Example: Linux amd64
curl -Lo harness.tar.gz https://github.com/htekdev/ai-harness/releases/latest/download/harness_linux_amd64.tar.gz
tar xzf harness.tar.gz
sudo mv harness /usr/local/bin/
```

### As a library

```bash
go get github.com/htekdev/ai-harness
```

**Requirements:** Go 1.25+, an API key for any OpenAI-compatible endpoint (GitHub Copilot, OpenAI, etc.)

## Quick Start

### 1. Scaffold a new project

```bash
harness init my-agent
cd my-agent
```

This creates:
- `harness.md` — main configuration (YAML frontmatter + system prompt)
- `.harness/tools/` — tool definitions
- `.harness/hooks/` — hook definitions
- `.harness/agents/` — sub-agent definitions

### 2. Validate your harness

```bash
harness validate
# ✅ harness.md — valid (6 tools, 2 hooks, 0 agents) [3ms]
```

### 3. Run interactively

```bash
harness run
```

### 4. Inspect your configuration

```bash
harness tools          # List all registered tools
harness hooks -v       # List hooks with details
harness agents         # List configured sub-agents
harness artifacts      # List typed artifacts in the registry
harness artifacts -v   # Detailed view with tools, hooks, conditions
harness artifacts --type plugin  # Filter by artifact type
harness context        # Show context window composition (what the agent sees)
harness context -v     # Detailed provenance (which file, which artifact, why)
harness context --json # Machine-readable context snapshot
harness context --budget 64000  # Show token budget utilization
```

### 5. Define your harness in Markdown

```markdown
<!-- harness.md -->
---
model:
  provider: copilot
  name: gpt-4o
  max_tokens: 4096
  temperature: 0.7
  api_key_env: GH_TOKEN

models:
  - name: gpt-4o
    provider: copilot
    api_key_env: GH_TOKEN
  - name: gpt-4o-mini
    provider: copilot
    api_key_env: GH_TOKEN

delegation:
  max_depth: 3
  max_concurrent: 5
  iterations_per_depth: [20, 10, 5, 3]

context:
  system_prompt: ""  # body below is the system prompt

tools:
  - name: greet
    description: Greet someone by name
    parameters:
      name:
        type: string
        description: Name to greet
        required: true
    script: |
      def run(args):
          return "Hello, " + args["name"] + "!"

hooks:
  - event: tool.pre
    handler: secret_guard
    priority: 10
    script: |
      def handle(event, payload):
          encoded = json.encode(payload)
          if "password" in encoded or "secret" in encoded:
              return block("potential secret detected")
          return allow()
---

# AI Assistant

You are a helpful AI assistant powered by the AI Harness framework.

## Rules

- Use the delegate tool to spawn sub-agents when you need specialized capabilities
- Never say "I can't do that" — delegate to a specialist agent
- Be concise and helpful
```

### 2. Add file-based tools (optional)

```markdown
<!-- .harness/tools/read_file.md -->
---
parameters:
  path: { type: string, required: true }
script: |
  def run(args):
      return fs.read(args["path"])
---

# read_file

Read a file from the workspace and return its contents.
```

### 3. Add custom agents (optional)

```markdown
<!-- .harness/agents/code-writer.md -->
---
model: gpt-4o
description: Writes and tests Go code
tools:
  - read_file
  - write_file
  - name: run_tests
    parameters: {}
    script: |
      def run(args):
          return exec.run("go", ["test", "./..."])
hooks:
  - path_guard
---

# Code Writer

You are a senior Go developer. Write clean, idiomatic, well-tested code.
Always run tests after writing code.
```

### 4. Run it

```go
package main

import "github.com/htekdev/ai-harness/harness"

func main() {
    h, err := harness.New("harness.md")
    if err != nil {
        panic(err)
    }
    h.Interactive()
}
```

```bash
export GH_TOKEN=$(gh auth token)
go run ./cmd/example/
```

That's it. The harness auto-discovers `.harness/tools/`, `.harness/hooks/`, and `.harness/agents/` directories. Inline definitions and file-based definitions are additive — mix freely.

## Directory convention

```
project/
  harness.md                     # root harness (frontmatter + system prompt)
  .harness/
    agents/
      code-writer.md             # custom agent: "code-writer"
      researcher.md              # custom agent: "researcher"
    tools/
      read_file.md               # tool: "read_file"
      write_file.md              # tool: "write_file"
      edit_file.md               # tool: "edit_file"
    hooks/
      path_guard.md              # hook: "path_guard"
      command_guard.md           # hook: "command_guard"
```

**Loading rules:**
1. `harness.md` frontmatter is loaded first (inline tools/hooks registered)
2. `.harness/tools/*.md` are scanned and ADDED to the tool registry
3. `.harness/hooks/*.md` are scanned and ADDED to the hook system
4. `.harness/agents/*.md` are scanned and registered in the agent registry
5. On name collision, **file wins** (allows overriding inline defaults)
6. `.harness/` is optional — inline-only works perfectly

## Delegation system

Delegation is a first-class harness primitive. The runtime exposes both sync and async delegation as native tools:

- `delegate` (sync)
- `delegate_async` (spawn background task)
- `delegate_status` (poll lifecycle state)
- `delegate_result` (read terminal result/error)
- `delegate_await` (join one or many async tasks)

### Recursive delegation (agent trees)

Delegates can spawn their own delegates, creating trees of specialized workers:

```json
{
  "task": "Build and test a REST API",
  "agent": "code-writer",
  "model": "gpt-4o-mini"
}
```

**Guardrails (harness-level, not prompting):**
- Depth-limited: configurable max (hard cap at 5 regardless)
- Iterations decrease per depth: `[20, 10, 5, 3]` by default
- Retry guard blocks tools after 2 consecutive errors
- `delegate.pre` / `delegate.post` hooks can block or rewrite at any level

### Sync and async execution relationship

Sync and async delegation share the same sub-agent semantics and execution engine:

- Same request shape (`task` required, plus either `agent` or inline `tools`; optional `hooks`, `model`, `system_prompt`)
- Same guardrails (depth, iteration budgeting, retry guard, hooks)
- Same named-agent composition behavior (agent defaults resolved first, request-level overrides merged in)

The difference is runtime delivery:

- `delegate` blocks and returns final output inline
- `delegate_async` returns immediately with `task_id`; work continues in background
- `delegate_status` / `delegate_result` / `delegate_await` provide explicit result retrieval and synchronization

### Async delegation

```json
{
  "tool": "delegate_async",
  "task": "Research the latest Go release notes",
  "agent": "researcher"
}
```

Returns a task handle immediately. Query status with `delegate_status`, get results with `delegate_result`, or block with `delegate_await`.

### Orchestration and dependency rules

- No implicit dependencies are inferred between async tasks.
- Fan-out: launch independent work with multiple `delegate_async` calls.
- Fan-in: join dependency boundaries explicitly with `delegate_await` (or poll with `delegate_status`).
- Strict dependency chain: use sync `delegate`, or `delegate_async` + immediate `delegate_await`.
- Failed tasks remain isolated and report terminal `failed` status through `delegate_result`/`delegate_await`.

### Custom agents

Named agents in `.harness/agents/` bundle:
- **Model** — which model to use
- **System prompt** — the markdown body
- **Tools** — references to `.harness/tools/` or inline definitions
- **Hooks** — references to `.harness/hooks/` or inline definitions

Agent tools can be **string references** (loaded from `.harness/tools/`) or **inline objects**. Hooks work the same way. This makes tools and hooks **composable** — define once, reuse across agents.

### Parallel tool execution

All tool calls within a single model turn execute concurrently (goroutines + WaitGroup). Results are collected in order and added to context sequentially.

### Starlark scripting engine

All tools and hooks are implemented in Starlark (a Python-like language) embedded in the Markdown frontmatter. No Go code needed for tool logic.

**Available built-ins:**

| Category | Functions |
|----------|-----------|
| **Time** | `time.now()` |
| **JSON** | `json.encode(val)`, `json.decode(s)` |
| **Math** | `math.abs`, `math.min`, `math.max`, `math.floor`, `math.ceil` |
| **Runtime** | `os.cwd()`, `os.hostname()`, `os.platform()`, `os.args()` |
| **URL / IDs** | `url.parse(s)`, `url.encode(params)`, `uuid.v4()` |
| **Flow control** | `random(min, max)`, `sleep(ms)`, `assert(condition, msg?)` |
| **Network** | `http.get(url, headers?, timeout_seconds?)`, `http.post(url, body?, headers?, timeout_seconds?)` |
| **Regex** | `re.match(pattern, text)`, `re.find_all(pattern, text)`, `re.replace(pattern, repl, text)` |
| **Hashing** | `hash.sha256(text)`, `hash.md5(text)` |
| **Encoding / crypto** | `base64.encode(s)`, `base64.decode(s)`, `crypto.hmac_sha256(key, msg)` |
| **Strings / templating** | `string.upper/lower/trim/split/join/truncate/pad_left/pad_right`, `template.render(tmpl, vars)` |
| **Validation / sets** | `validate.email/url/json`, `set.new/contains/union/intersect/diff/values/size` |
| **State** | `cache.set/get/has/delete/clear`, `metrics.incr/get/reset/snapshot`, `ctx.set/get/has/delete/clear/snapshot` |
| **I/O** | `env(key)`, `log(msg)`, `emit("custom.event", payload)`, `exec.run(cmd, args?, timeout_ms?, dir?)` |
| **File read** | `fs.read(path)`, `fs.exists(path)`, `fs.list(path)`, `fs.stat(path)`, `fs.glob(pattern)`, `fs.line_count(path)`, `fs.find(path, pattern)`, `fs.read_lines(path, start, end)` |
| **File write** | `fs.write(path, content)`, `fs.append(path, content)`, `fs.mkdir(path)`, `fs.remove(path)`, `fs.copy(src, dst)`, `fs.move(src, dst)` |
| **File edit / preview** | `fs.replace(path, old, new)`, `fs.replace_all(path, old, new)`, `fs.insert_at(path, line, content)`, `fs.replace_lines(path, start, end, content)`, `fs.delete_lines(path, start, end)`, `fs.diff(old_content, new_content, old_name?, new_name?)` |
| **Hooks** | `allow()`, `block(reason)`, `modify(payload)` |

### Lower-level API

```go
client := completion.NewClient(completion.ClientConfig{
  BaseURL:    "https://api.githubcopilot.com",
  APIKey:     os.Getenv("GITHUB_TOKEN"),
  Model:      "gpt-4o",
  MaxRetries: 3,
})

registry := tools.NewRegistry()
system := hooks.NewSystem()
ctxMgr := contextpkg.NewManager(contextpkg.Config{
  SystemPrompt: "You are a helpful assistant.",
})

a := agent.New(agent.Options{
  Client:  client,
  Tools:   registry,
  Hooks:   system,
  Context: ctxMgr,
})
```

### CLI example

```bash
# Linux/macOS
export GH_TOKEN=$(gh auth token)

# Windows PowerShell
$env:GH_TOKEN = $(gh auth token)

go run ./cmd/example/
```

## API reference

### `harness`
The high-level entry point. It loads config, creates the completion client, context manager, hook system, tool registry, model registry, agent registry, and agent.

```go
h, err := harness.New("harness.md")
result, err := h.Run(ctx, "Summarize this file")
```

Key methods:

- `New(configPath string) (*Harness, error)` — auto-detects .md vs .yaml
- `NewFromConfig(cfg *config.Config, agents map[string]*config.AgentConfig) (*Harness, error)`
- `Run(ctx, input)`
- `RunSession(ctx)` / `EndSession(ctx)`
- `RegisterTool(def, handler)`
- `RegisterHook(reg)`
- `Agent()`

Notes:

- Tools with a `script` field are fully functional immediately — no Go handler needed.
- Tools without a `script` are registered with a placeholder handler until you supply one via `RegisterTool`.
- Hooks with a `script` field are fully functional immediately.
- The `delegate` meta-tool is auto-registered when using the harness package.

### `agent`
The core turn loop. It sends messages to the model, executes requested tools, appends tool results, and continues until the model returns a final response.

```go
result, err := a.Run(ctx, "What time is it?")
fmt.Println(result.Response)
fmt.Println(result.ToolCalls)
fmt.Println(result.ToolResults)
```

Behavior highlights:

- supports tool-call loops
- aggregates token usage across completion calls
- enforces `MaxToolIterations`
- emits lifecycle hooks around sessions, turns, tools, and completions

### `completion`
OpenAI-compatible chat client with retry support and both non-streaming and streaming modes.

```go
resp, err := client.Complete(ctx, completion.Request{
  Messages: []completion.Message{{Role: completion.RoleUser, Content: "Hello"}},
})
```

#### Streaming example

```go
stream, err := client.CompleteStream(ctx, completion.Request{
  Messages: []completion.Message{{Role: completion.RoleUser, Content: "Stream the answer"}},
})
if err != nil {
  panic(err)
}

for chunk := range stream {
  if chunk.Err != nil {
    panic(chunk.Err)
  }
  if chunk.Done {
    break
  }

  if chunk.Delta != "" {
    fmt.Print(chunk.Delta)
  }
  for _, tc := range chunk.ToolCallDeltas {
    fmt.Printf("\npartial tool call: %s %s", tc.Function.Name, tc.Function.Arguments)
  }
}
```

Streaming details:

- parses Server-Sent Events in OpenAI chat format (`data: {...}\n\n`)
- handles the `[DONE]` sentinel
- returns `StreamChunk` values with text deltas, tool call deltas, finish reason, done state, and stream errors

### `tools`
Runtime tool registry used by the agent.

```go
registry := tools.NewRegistry()
err := registry.Register(definition, handler)
result := registry.Execute(ctx, tools.Call{
  ID:        "call_1",
  Name:      "echo",
  Arguments: json.RawMessage(`{"message":"hello"}`),
})
```

Capabilities:

- register/unregister tools
- inspect definitions with `Get` and `List`
- execute handlers with JSON arguments
- convert registered tools to OpenAI tool schema with `ToOpenAIFormat()`

### `hooks`
Lifecycle hook system for governance and cross-cutting behavior.

```go
sys := hooks.NewSystem()
sys.Register(hooks.Registration{
  Name:     "block-dangerous-tool",
  Event:    hooks.EventToolPre,
  Priority: 10,
  Handler: func(ctx context.Context, event hooks.Event, payload any) hooks.Result {
    return hooks.Result{Action: hooks.ActionContinue}
  },
})
```

Capabilities:

- register ordered handlers by event
- inspect handlers by event
- block, modify, or continue execution

### `context`
Conversation history manager with a system prompt and basic token-aware truncation.

```go
ctxMgr := contextpkg.NewManager(contextpkg.Config{
  SystemPrompt: "You are concise.",
  MaxMessages:  50,
  MaxTokens:    128000,
})
ctxMgr.AddMessage(completion.Message{Role: completion.RoleUser, Content: "Hi"})
messages := ctxMgr.Messages()
```

Capabilities:

- maintain conversation history
- preserve system prompt outside history
- estimate tokens approximately
- truncate oldest messages when limits are exceeded
- fork contexts for branching workflows

### `config`
Markdown/YAML config loader, directory scanner, and validator.

```go
cfg, agents, err := config.LoadFull("harness.md")
if err != nil {
  panic(err)
}

apiKey := cfg.ResolveAPIKey()
baseURL := cfg.BaseURL()
```

Capabilities:

- parse Markdown (frontmatter + body) and legacy YAML/JSON
- auto-detect format by extension
- scan `.harness/tools/`, `.harness/hooks/`, `.harness/agents/` directories
- merge file-based definitions with inline (additive, files win on collision)
- apply defaults and validate

## Durable scheduling primitive (data + scanner)

AI Harness scheduling is defined as durable data and event contracts. The runtime does **not** require a built-in cron daemon.

### Schedule records (durable data)

Schedule definitions are stored as data with one of three `kind` values:

- `one_shot` — run once at an absolute timestamp
- `interval` — run every fixed duration from an anchor
- `cron` — run from a cron expression (with optional timezone)

Example shape:

```yaml
schedules:
  - id: nightly-evals
    kind: cron
    expr: "0 2 * * *"
    timezone: UTC
    payload:
      task: run_evals

  - id: backlog-sync
    kind: interval
    every: 15m
    anchor_at: "2026-01-01T00:00:00Z"
    payload:
      task: sync_backlog

  - id: one-time-release
    kind: one_shot
    at: "2026-06-01T17:00:00Z"
    payload:
      task: publish_release_notes
```

### Schedule lifecycle events

The primitive emits four explicit schedule events:

| Event | When emitted |
| --- | --- |
| `schedule.created` | A new schedule record is created |
| `schedule.updated` | Schedule definition or status changes |
| `schedule.deleted` | Schedule is removed or soft-deleted |
| `schedule.due` | A due occurrence is claimed by a scanner and emitted |

### Projection: computing `next_due_at`

A projection maintains derived timing fields from schedule data:

- `next_due_at` — next eligible due timestamp
- `last_due_at` — last emitted due timestamp (if any)
- `active` / `deleted_at` — eligibility flags

Projection rules:

1. `one_shot`: `next_due_at = at` until emitted, then `next_due_at = null`
2. `interval`: `next_due_at` advances by `every` from `anchor_at` (or previous due)
3. `cron`: `next_due_at` is the next cron match in `timezone`

### External due-event scanner contract

The scanner is an external daemon/sidecar that repeatedly:

1. Reads projection rows where `active = true` and `next_due_at <= now()`
2. Claims rows atomically (lease/lock) to avoid duplicate emitters
3. Emits `schedule.due` with `{schedule_id, due_at, payload, idempotency_key}`
4. Advances projection (`last_due_at`, recomputed `next_due_at`, lease release)

Required contract details:

- `idempotency_key` should be deterministic per occurrence (for example `schedule_id + due_at`)
- Emitters must tolerate retries and duplicate delivery attempts
- Multiple scanners may run concurrently if they honor the same claim/lease semantics

This keeps AI Harness core lightweight while providing a clear integration path for Kubernetes CronJobs, long-running workers, queue consumers, or any external scheduler process.

## Configuration reference

Example `harness.md` frontmatter:

```yaml
model:
  provider: copilot         # openai | copilot | custom string
  name: gpt-4o              # required, non-empty
  max_tokens: 4096          # must be > 0
  temperature: 0.7          # must be between 0 and 2
  base_url: ""             # optional override; provider default used when empty
  api_key_env: GH_TOKEN     # env var to read API key from

models:                      # named model registry for delegation
  - name: gpt-4o
    provider: copilot
    api_key_env: GH_TOKEN
  - name: gpt-4o-mini
    provider: copilot
    api_key_env: GH_TOKEN

delegation:
  max_depth: 3              # max recursive depth (hard cap: 5)
  max_concurrent: 5         # max async tasks running
  iterations_per_depth:     # tool iterations allowed per depth level
    - 20
    - 10
    - 5
    - 3

context:
  max_history: 50           # max non-system messages retained
  max_tokens: 128000        # approximate context budget
  system_prompt: ""         # overridden by markdown body if empty

tools:
  - name: echo
    description: Echo back a message
    parameters:
      message:
        type: string        # string | number | boolean | object | array
        description: Message to echo back
        required: true

hooks:
  - event: tool.pre         # see valid hook events below
    handler: audit_log      # symbolic hook name

triggers:
  - name: followup-on-due
    match:
      stream: events
      types: [schedule.due]
      when: 'payload.get("kind") == "followup"'
    actions:
      - type: wake_runtime
        runtime: followup-worker
      - type: emit
        event: followup.requested
```

### Full schema

#### `type: model` artifact

New providers and models should be onboarded as artifacts in `.harness/models/*.md`, not by editing core composition code.

```markdown
---
name: openai-models
type: model
version: "1.0.0"
description: OpenAI model catalog for this harness
models:
  - name: gpt-4o
    provider: openai
    api_key_env: OPENAI_API_KEY
    base_url: https://api.openai.com/v1
    temperature: 0.2
    max_tokens: 16384
    capabilities:
      streaming: true
      tool_calling: true
      vision: true
      json_mode: true
---

Optional markdown body describing when to use these models.
```

Model artifacts may carry standard artifact metadata (`name`, `version`, `description`, `tags`, `depends_on`, `condition`, `priority`) plus a `models:` list. They may include markdown body context, but they do **not** define tools or hooks.

#### `models[]`

| Field | Type | Required | Notes |
| --- | --- | --- | --- |
| `name` | string | yes | Model identifier; validated as non-empty |
| `provider` | string | yes | Provider slug such as `openai`, `copilot`, or `anthropic` |
| `api_key_env` | string | no | Environment variable containing credentials |
| `base_url` | string | no | Provider endpoint override |
| `temperature` | float | no | Declarative default sampling setting; must stay between 0 and 2 when used |
| `max_tokens` | int | yes | Declarative response/token limit; must be greater than 0 |
| `capabilities.streaming` | bool | no | Model supports streamed responses |
| `capabilities.tool_calling` | bool | no | Model can call harness tools |
| `capabilities.vision` | bool | no | Model accepts image input |
| `capabilities.json_mode` | bool | no | Model can be constrained to structured JSON output |

#### Model onboarding flow

1. Create a new artifact under `.harness/models/<provider-or-team>.md`.
2. Add one or more `models[]` entries describing provider wiring, defaults, limits, and capabilities.
3. Optionally gate the artifact with `condition:` or organize it with `tags:` / `depends_on:`.
4. Run `harness artifacts -v` to confirm the artifact is discovered and the expected models are registered.
5. No core rewrite should be necessary for ordinary model/provider additions; only add Go code when a provider needs entirely new transport/auth/runtime semantics.

#### Interaction with harness artifacts and overrides

- `model` artifacts load from `.harness/models/*.md` and compose at the lowest priority, contributing only `result.Models`.
- `harness` artifacts define the base identity/system prompt.
- `builtin` and `plugin` artifacts define tools and hooks.
- `override` artifacts can change context, tools, hooks, or activation conditions around model artifacts, but they do not carry `models:` themselves.
- Multiple active model artifacts compose by concatenating their `models[]`, so keep names distinct and use `condition:` for environment-specific catalogs.

#### `context`

| Field | Type | Required | Notes |
| --- | --- | --- | --- |
| `max_history` | int | no | Defaults to `50` |
| `max_tokens` | int | no | Defaults to `128000` |
| `system_prompt` | string | no | Prepended as a system message |

#### `tools[]`

| Field | Type | Required | Notes |
| --- | --- | --- | --- |
| `name` | string | yes | Must be unique and non-empty |
| `description` | string | no | Sent to the model |
| `parameters` | map | no | Parameter definitions keyed by name |
| `script` | string | no | Starlark script implementing `def run(args): ...` |

#### `tools[].parameters.*`

| Field | Type | Required | Notes |
| --- | --- | --- | --- |
| `type` | string | yes | JSON-schema-like primitive type |
| `description` | string | no | Parameter description |
| `required` | bool | no | Marks the field required |

#### `hooks[]`

| Field | Type | Required | Notes |
| --- | --- | --- | --- |
| `event` | string | yes | Must match a defined lifecycle event (see Hook system section) or use `custom.*` / `meta.*` |
| `handler` | string | yes | Symbolic handler name |
| `when` | string | no | Optional Starlark expression; hook runs only when it evaluates truthy |
| `priority` | int | no | Lower numbers execute first (default: 100) |
| `script` | string | no | Starlark script implementing `def handle(event, payload): ...` |

#### `triggers[]`

| Field | Type | Required | Notes |
| --- | --- | --- | --- |
| `name` | string | yes | Unique durable trigger rule name |
| `match` | object | yes | Event stream/type/predicate matcher |
| `actions` | list | yes | One or more primitive actions |
| `enabled` | bool | no | Defaults to `true` |

#### `triggers[].match`

| Field | Type | Required | Notes |
| --- | --- | --- | --- |
| `stream` | string | no | Durable event stream name; defaults to `events` if not specified |
| `types` | []string | yes | One or more event types that should fire the rule |
| `when` | string | no | Optional predicate over `event` and `payload`; trigger fires only when truthy |

#### `triggers[].actions[]`

| Field | Type | Required | Notes |
| --- | --- | --- | --- |
| `type` | string | yes | `emit` \| `invoke_adapter` \| `wake_runtime` \| `create_wakeup` |
| `event` | string | yes (`emit`) | Event type to emit |
| `adapter` | string | yes (`invoke_adapter`) | Adapter name to invoke |
| `runtime` | string | yes (`wake_runtime`, `create_wakeup`) | Runtime identifier or selector |
| `payload` | object | no | Input passed to the emitted event, adapter, or runtime |
| `at` | string | yes (`create_wakeup`) | Due time / schedule expression for the wake-up record |
| `dedupe_key` | string | no | Optional idempotency key for durable actions |

## Hook system

### Lifecycle (control-plane order)

Hooks are part of the control plane and run at explicit lifecycle boundaries in deterministic priority order.

| Phase | Event | Purpose |
| --- | --- | --- |
| Session | `session.start` / `session.end` | Session bootstrap and teardown policies |
| Turn | `turn.start` / `turn.end` | Input normalization and turn-level governance |
| Completion | `completion.pre` / `completion.post` | Gate and shape model request/response |
| Tool execution | `tool.pre` / `tool.post` | Enforce tool policy before/after execution |
| Delegation | `delegation.pre` / `delegation.post` | Gate recursive delegation and post-process results |
| Error | `error` | Observe/report runtime failures |

Extension namespaces:

- `custom.*` — user-defined domain events emitted via `emit("custom.event_name", payload)` from Starlark hook/tool scripts
- `meta.*` — runtime governance events emitted by `meta.register_tool`, `meta.register_hook`, `meta.register_agent`, and `meta.call_tool` (dynamic registration/invocation control points)

Hooks may also include a `when:` expression that can inspect `event`, `payload`, and standard Starlark built-ins before `handle(event, payload)` runs.

### Actions and capability boundaries

A hook handler returns `hooks.Result` with one of these actions:

- `ActionContinue`: continue normally
- `ActionBlock`: stop execution and return an error
- `ActionModify`: replace payload for subsequent handlers and downstream execution

Governance boundaries are explicit:

- registration validates event names (`session.*`, `turn.*`, `tool.*`, `completion.*`, `delegation.*`, `error`, `custom.*`, `meta.*`)
- lower priority numbers run first, giving deterministic policy ordering
- `ActionModify` is scoped to the dispatched payload seen by subsequent handlers and downstream execution
- `ActionBlock` is the only deny primitive and is surfaced with explicit reason text

### Monitoring and telemetry surfaces

Hooks pair with observability surfaces so policy and runtime behavior can be inspected and persisted:

- `harness hooks --verbose`: active handlers, events, and priorities (policy plane visibility)
- `harness context [-v|--json]`: composed context snapshot with provenance, token budget usage, inactive artifacts, and warnings
- Starlark runtime state: `metrics.incr/get/reset/snapshot`, `ctx.*`, and `cache.*` for in-turn counters and state inspection
- event stream surface: built-in lifecycle events, `custom.*` events (via `emit(...)`), and `meta.*` governance events; export is hook-driven (for example `log(...)`, `http.post(...)`, or `exec.run(...)` from hook scripts)

This aligns with event-driven persistence: treat hook dispatches and context snapshots as append-only runtime facts that can be exported to your observability stack for auditing, replay, and governance reporting.

### Priority

Lower priority numbers run first.

```go
sys.Register(hooks.Registration{
  Name:     "normalize-input",
  Event:    hooks.EventTurnStart,
  Priority: 1,
  Handler: func(ctx context.Context, event hooks.Event, payload any) hooks.Result {
    input := payload.(string)
    return hooks.Result{Action: hooks.ActionModify, Payload: strings.TrimSpace(input)}
  },
})
```

### Common hook examples

#### Block a tool call

```go
sys.Register(hooks.Registration{
  Name:  "block-delete",
  Event: hooks.EventToolPre,
  Handler: func(ctx context.Context, event hooks.Event, payload any) hooks.Result {
    call := payload.(*tools.Call)
    if call.Name == "delete_file" {
      return hooks.Result{Action: hooks.ActionBlock, Reason: "delete_file is disabled"}
    }
    return hooks.Result{Action: hooks.ActionContinue}
  },
})
```

#### Modify user input

```go
sys.Register(hooks.Registration{
  Name:  "rewrite-input",
  Event: hooks.EventTurnStart,
  Handler: func(ctx context.Context, event hooks.Event, payload any) hooks.Result {
    return hooks.Result{Action: hooks.ActionModify, Payload: "Answer in one sentence: " + payload.(string)}
  },
})
```

## Trigger system

Triggers are the durable, event-driven counterpart to hooks. A hook intercepts the current in-process operation; a trigger reacts to an already-recorded event and schedules or fans out follow-up work.

### Rule model

A trigger rule evaluates an event envelope with three matching inputs:

- `stream` — which durable event stream to watch
- `types` — one or more configured event types for the rule
- `when` — an optional predicate over the event payload and metadata

Conceptually, the engine processes committed event envelopes shaped like:

```json
{
  "id": "evt_123",
  "stream": "events",
  "type": "schedule.due",
  "payload": {"kind": "followup"},
  "meta": {
    "causation_id": "evt_122",
    "origin_event_id": "evt_122"
  }
}
```

If `match.stream`, `match.types`, and `match.when` all pass, the rule fires and executes its actions in order.

### Primitive actions

The action surface stays intentionally small:

- `emit` — append another event to a durable stream
- `invoke_adapter` — call an adapter that bridges to an external system
- `wake_runtime` — wake an existing runtime or start it if policy allows
- `create_wakeup` — write a durable wake-up record for later processing

More complex workflows should be expressed by composing these primitives instead of adding bespoke trigger actions.

### Loop protection

The system must prevent triggers from recursing indefinitely. The runtime should enforce all of the following:

1. **Per-event idempotency** — a rule executes at most once for a given `(trigger_name, event_id)`.
2. **Causation tracking** — emitted events and wake-up records carry `causation_id`, `origin_event_id`, and `trigger_name`.
3. **Depth limit** — every trigger-produced event increments `trigger_depth`; execution stops at a configured cap.
4. **Ancestry check** — if a rule name already appears in the current causation chain assembled from carried `trigger_name`, `causation_id`, and `origin_event_id` metadata, do not execute it again. For example, `followup-on-due` must not re-fire on an event already descended from `followup-on-due`.
5. **Wake-up dedupe** — `create_wakeup` must deduplicate on `(runtime, dedupe_key)` or an equivalent durable uniqueness key.

These rules allow safe fan-out while preventing self-triggering loops and duplicate wake-ups.

### Hooks vs triggers

| Use | Hooks | Triggers |
| --- | --- | --- |
| Timing | Inline, while the current agent/tool/completion lifecycle is executing | After an event is durably recorded |
| Primary job | Block, rewrite, or observe in-flight work | Start follow-up work or emit more events |
| Persistence | Ephemeral/in-process | Durable/event-driven |
| Best for | Governance, validation, prompt/tool rewrites | Long-running orchestration, adapters, scheduling, runtime wake-ups |

In short: use **hooks** to control what is happening *right now*; use **triggers** to react to what has *already happened*.

## Tool registration

A tool is made of two parts:

1. a **definition** the model sees
2. a **handler** that executes the tool

```go
def := tools.Definition{
  Name:        "calculate",
  Description: "Add two numbers",
  Parameters: []tools.Parameter{
    {Name: "a", Type: tools.TypeNumber, Description: "First number", Required: true},
    {Name: "b", Type: tools.TypeNumber, Description: "Second number", Required: true},
  },
}

handler := func(ctx context.Context, args json.RawMessage) (string, error) {
  var input struct {
    A float64 `json:"a"`
    B float64 `json:"b"`
  }
  if err := json.Unmarshal(args, &input); err != nil {
    return "", err
  }
  return fmt.Sprintf("%g", input.A+input.B), nil
}

registry.Register(def, handler)
```

Execution flow:

1. model requests a tool call
2. agent converts the request into `tools.Call`
3. registry invokes the handler
4. result is added back to conversation context as a `tool` message
5. model continues with the new information

## Testing

```bash
go test ./... -cover
```

Current package coverage is designed to stay high across the core libraries, including streaming, config validation, agent loop behavior, hooks, tools, context management, and the top-level harness package.

## Project structure

```
ai-harness/
├── .harness/           # File-based tools, hooks, agents (auto-discovered)
│   ├── agents/         # Custom named agents (.md files)
│   ├── hooks/          # Hook definitions (.md files)
│   └── tools/          # Tool definitions (.md files)
├── agent/              # Agent loop orchestration (parallel tool execution)
├── cmd/example/        # Example CLI (auto-detects harness.md vs harness.yaml)
├── completion/         # OpenAI-compatible client, including streaming
├── config/             # Markdown/YAML config, directory loader, validation
├── context/            # Conversation history manager
├── delegation/         # Recursive delegation, depth tracking, async task store
├── harness/            # High-level API (model registry, agent resolver, wiring)
├── hooks/              # Lifecycle hook system
├── scripting/          # Starlark engine + fs/edit built-ins
├── tools/              # Tool registry and execution
├── harness.md          # Root harness configuration
└── go.mod
```

## Typed Artifact System

Artifacts are the fundamental building blocks of a harness. Each artifact is a single Markdown file that bundles identity, tools, hooks, and models into one composable unit.

For the versioned base schema/design model (`harness_artifact`, `harness`, `builtin`, `plugin`, `override`), see [`artifact/SCHEMA.md`](artifact/SCHEMA.md).

### Artifact Types (priority order)

| Type | Priority | Purpose |
|------|----------|---------|
| `override` | 100 | Project-local overrides that supersede anything |
| `harness` | 80 | Root identity and policy (exactly one per project) |
| `builtin` | 60 | Core capabilities shipped with the runtime |
| `plugin` | 40 | User-authored or third-party capability bundles |
| `model` | 20 | Provider/model onboarding configurations |

### One file = one capability

```markdown
---
name: git-safety
type: plugin
version: 1.0.0
description: Prevent force-pushes and history rewrites
tags: [governance, git]
condition: '"*.git*" in ctx.get("active_files", [])'
tools:
  - name: git-status
    description: Show git status safely
    timeout_ms: 5000
hooks:
  - event: onPreToolUse
    handler: block_force_push
    script: |
      def handle(event, payload):
          if "force" in payload.get("args", ""):
              return deny("Force push blocked by governance")
          return allow()
---

Git safety context: this plugin ensures all git operations
go through the governed workflow. Force pushes are blocked
at the architectural level.
```

### Per-turn evaluation

Artifacts with `condition` expressions are evaluated **every turn** using Starlark:

```yaml
condition: 'ctx.get("turn", 0) > 3'          # Activate after turn 3
condition: 'ctx.get("mode") == "review"'      # Activate in review mode
condition: 'len(time.now()) > 0'              # Always active (time-based)
```

After `EvaluateConditions()` runs, each artifact's `Active` field reflects whether it should participate in composition. This is the key differentiator: **governance adapts per-turn, not just at startup**.

## Composition & Options Pattern

The `Composer` merges all active artifacts into a unified view using priority-based resolution.

### Deterministic composition and merge rules

Composition is deterministic and fully inspectable:

1. Artifacts are sorted by `EffectivePriority()` ascending (lower first).
2. If priorities tie, artifacts are ordered alphabetically by `name` (stable tie-break).
3. `ComposedResult.ActiveArtifacts` returns the exact ordered list that participated.

Field merge behavior is explicit:

- `Identity`: set from `harness` context.
- `ContextBlocks`: appends non-harness context blocks in composition order.
- `Tools`: deduplicated by tool name; later (higher-priority) artifacts override earlier definitions.
- `Hooks`: appended in composition order (no hidden dedup/override).
- `Models`: appended in composition order.

Conflict resolution:

- Duplicate artifact names are rejected at registration time.
- Only one `harness` artifact may be registered.
- Tool-name collisions are resolved by deterministic priority order (highest wins).

### Basic usage

```go
import "github.com/htekdev/ai-harness/artifact"

reg := artifact.NewRegistry()
// ... register artifacts ...

composer := artifact.NewComposer(reg)

// Default: compose only Active artifacts (respects EvaluateConditions)
result, err := composer.Compose(nil)

// With dynamic condition evaluation at compose time
result, err = composer.Compose(func(cond string) (bool, error) {
    return evaluateStarlark(cond)
})
```

### Functional options (ComposeWith)

For fine-grained control over composition:

```go
// Only active artifacts (default)
result, _ := composer.ComposeWith()

// Include inactive artifacts (debugging/observability)
result, _ := composer.ComposeWith(artifact.WithIncludeInactive())

// Filter by type
result, _ := composer.ComposeWith(artifact.WithTypeFilter(artifact.TypePlugin))

// Filter by tag
result, _ := composer.ComposeWith(artifact.WithTagFilter("governance"))

// Dynamic evaluation (overrides cached Active state)
result, _ := composer.ComposeWith(artifact.WithEvalFn(myEvalFn))

// Combine options
result, _ := composer.ComposeWith(
    artifact.WithTypeFilter(artifact.TypePlugin, artifact.TypeBuiltin),
    artifact.WithTagFilter("security"),
)
```

### Per-turn lifecycle

The full per-turn workflow:

```go
// 1. Set turn state
ctx := scripting.WithTurnState(context.Background())
scripting.SetTurnState(ctx, "turn", turnNumber)
scripting.SetTurnState(ctx, "mode", "coding")

// 2. Evaluate all artifact conditions against current state
composer.EvaluateConditions(ctx)

// 3. Compose only the artifacts that passed evaluation
result, err := composer.Compose(nil)
// result.Tools       — deduplicated, priority-ordered tools
// result.Hooks       — all hooks from active artifacts
// result.Identity    — merged system prompt from harness artifact
// result.Models      — model catalog from active model artifacts
// result.ContextBlocks — context from all active non-harness artifacts
```

## Event-driven persistence (artifact/runtime)

Harness persistence is modeled as an append-only event stream (no in-place mutation).

### Event model

Core event kinds:

- `artifact.upsert` — create/update an artifact projection
- `artifact.remove` — remove an artifact projection
- `runtime.turn.start` — mark turn lifecycle start
- `runtime.turn.end` — mark turn lifecycle end
- `runtime.hook.dispatch` — record hook dispatch activity
- `runtime.context.built` — record context observability results (token usage / build point)

Each event carries:

- `id` (stable unique identifier)
- `parent_id` (lineage pointer; enables branch/replay)
- `branch` (defaults to `main`)
- `timestamp`
- mutation payload (`artifact` or `runtime`)

### Rebuild / replay semantics

- State is derived by replaying events from root → branch head.
- Branch heads are independent pointers to the newest event in that branch.
- Branch replay walks `parent_id` links, so alternate histories can coexist for audit/testing.
- Current state is a projection: artifacts map + latest runtime summary.
- Full audit history is always available from the append-only log.

### Inspection and debugging

- The event log can be inspected directly (`Events()` in append order).
- Rebuild a branch projection with `Rebuild(branch)`.
- Replay is deterministic because events are immutable and ordered by lineage.
- `runtime.context.built` events connect persistence to `observe.Snapshot` metrics.
- `runtime.hook.dispatch` events connect persistence to hook lifecycle activity.

## Status

**v0.3.0** — Typed artifact system, context observability, per-turn evaluation engine.

| Component | Status |
|-----------|--------|
| Config (Markdown + YAML) | ✅ Stable |
| Agent loop (parallel tools) | ✅ Stable |
| Hook system (core lifecycle events + custom/meta namespaces) | ✅ Stable |
| Tool registry + Starlark engine | ✅ Stable |
| Delegation (sync + async) | ✅ Stable |
| Completion client (streaming) | ✅ Stable |
| Eval framework | ✅ Stable |
| `.harness/` directory convention | ✅ Stable |
| Typed artifact registry | ✅ Stable |
| Context observability | ✅ Stable |
| Per-turn evaluation engine | ✅ Stable |
| Composition options pattern | ✅ Stable |
| Event-driven persistence log | ✅ Stable |

## Contributing

Contributions are welcome. Keep changes small, add tests with code changes, and run:

```bash
go build ./...
go test ./... -cover
go vet ./...
```

## License

MIT
