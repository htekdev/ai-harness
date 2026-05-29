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

## What Makes It Different

- **Markdown-first** — `harness.md` (YAML frontmatter + body as system prompt) defines your agent declaratively. `.harness/` directory adds tools, hooks, and sub-agents as individual `.md` files.
- **Governed by default** — Hooks enforce safety through architecture, not instructions. You don't make agents trustworthy by writing better prompts — you make them trustworthy by engineering harnesses where wrong behavior is architecturally impossible.
- **Self-extending** — The `delegate` meta-tool lets agents create tools and spawn sub-agents recursively at runtime.
- **Minimal** — Single Go binary, ~5 dependencies, compiles in seconds. `tools.Handler` is just `func(ctx, args) (string, error)`.
- **Portable** — Works with GitHub Copilot, OpenAI, or any compatible chat completions API.
- **Testable** — Built-in eval framework validates agent behavior against real models in CI.

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

### Async delegation

```json
{
  "task": "Research the latest Go release notes",
  "agent": "researcher",
  "async": true
}
```

Returns a task handle immediately. Query status with `delegate_status`, get results with `delegate_result`, or block with `delegate_await`.

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
```

### Full schema

#### `model`

| Field | Type | Required | Notes |
| --- | --- | --- | --- |
| `provider` | string | no | Defaults to `openai` |
| `name` | string | yes | Model name; validated as non-empty |
| `max_tokens` | int | yes | Must be greater than 0 |
| `temperature` | float | yes | Must be between 0 and 2 |
| `base_url` | string | no | Overrides provider-based default |
| `api_key_env` | string | no | Defaults to `GITHUB_TOKEN` |

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
| `event` | string | yes | Must match a defined lifecycle event |
| `handler` | string | yes | Symbolic handler name |
| `when` | string | no | Optional Starlark expression; hook runs only when it evaluates truthy |
| `priority` | int | no | Lower numbers execute first (default: 100) |
| `script` | string | no | Starlark script implementing `def handle(event, payload): ...` |

## Hook system

### Events

Valid events:

Hooks may also include a `when:` expression that can inspect `event`, `payload`, and the standard Starlark built-ins before the main `handle(event, payload)` function runs.


- `session.start`
- `session.end`
- `turn.start`
- `turn.end`
- `tool.pre`
- `tool.post`
- `completion.pre`
- `completion.post`

### Actions

A hook handler returns `hooks.Result` with one of these actions:

- `ActionContinue`: continue normally
- `ActionBlock`: stop execution and return an error
- `ActionModify`: replace the payload passed to subsequent handlers

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
| Hook system (8 lifecycle events) | ✅ Stable |
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
