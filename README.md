# AI Harness

A minimal, extensible AI agent harness in Go that makes building **self-extending, governed agents** trivial. Define tools, hooks, and even entire sub-agents inline in YAML using embedded Starlark scripts — no external plugins, no subprocess protocols, no framework lock-in.

## Why this project exists

Most agent frameworks force a choice: either you get a rigid plugin system that's hard to customize, or you get raw LLM access with no guardrails. `ai-harness` takes a different approach:

**Everything is defined in YAML. Everything is governed by hooks. Agents can extend themselves at runtime.**

- **Self-contained**: Tools, hooks, and governance rules are all defined inline in `harness.yaml` using Starlark scripts — no external files, no build steps
- **Self-extending**: The built-in `delegate` meta-tool lets agents spin up sub-agents with custom tools on the fly when they lack a capability
- **Governed by default**: Built-in retry guards, path traversal protection, secret detection, and hook-based lifecycle control prevent runaway behavior without relying on prompt engineering
- **Minimal**: Plain Go interfaces — `tools.Handler` is just `func(ctx, args) (string, error)`
- **Portable**: Works with GitHub Copilot, OpenAI, or any compatible API

### Core philosophy

> Make the right thing to do the easy thing to do.

The harness enforces safety through architecture, not instructions:
- `fs.replace` **fails** if the match isn't unique (forces surgical edits)
- Delegates are capped at 5 tool iterations (fail fast, don't loop)
- Retry guards auto-block tools after 2 consecutive errors
- Path operations are jailed to the working directory at the Go level
- Hooks run at every lifecycle point — blocking is a first-class action

## Architecture

```
┌───────────────────────────────────────────────────────────────┐
│                        AI Harness                              │
├───────────────────────────────────────────────────────────────┤
│                                                               │
│  ┌──────────┐    ┌──────────┐    ┌──────────────┐           │
│  │  Config  │───▶│  Agent   │───▶│  Completion  │           │
│  │  (YAML)  │    │  Loop    │    │  Client      │           │
│  └──────────┘    └────┬─────┘    └──────────────┘           │
│                       │                                       │
│       ┌───────────────┼───────────────┐                      │
│       ▼               ▼               ▼                      │
│  ┌──────────┐   ┌──────────┐   ┌──────────────┐            │
│  │  Hook    │   │  Tool    │   │   Context    │            │
│  │  System  │   │ Registry │   │   Manager    │            │
│  └──────────┘   └────┬─────┘   └──────────────┘            │
│                       │                                       │
│       ┌───────────────┼───────────────┐                      │
│       ▼               ▼               ▼                      │
│  ┌──────────┐   ┌──────────┐   ┌──────────────┐            │
│  │ Starlark │   │ Delegate │   │  fs/edit     │            │
│  │ Engine   │   │  System  │   │  Built-ins   │            │
│  └──────────┘   └──────────┘   └──────────────┘            │
│                                                               │
└───────────────────────────────────────────────────────────────┘
```

## Installation

```bash
go get github.com/htekdev/ai-harness
go install github.com/htekdev/ai-harness/cmd/example@latest
```

## Quick start

### 1. Define everything in YAML

```yaml
# harness.yaml
model:
  provider: copilot
  name: gpt-4o
  max_tokens: 4096
  temperature: 0.7

context:
  system_prompt: "You are a helpful AI assistant."

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

  - name: read_file
    description: Read a file
    parameters:
      path:
        type: string
        required: true
    script: |
      def run(args):
          return fs.read(args["path"])

  - name: edit_file
    description: Find and replace in a file
    parameters:
      path:
        type: string
        required: true
      old_str:
        type: string
        required: true
      new_str:
        type: string
        required: true
    script: |
      def run(args):
          return fs.replace(args["path"], args["old_str"], args["new_str"])

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
```

### 2. Run it

```go
package main

import "github.com/htekdev/ai-harness/harness"

func main() {
    h, err := harness.New("harness.yaml")
    if err != nil {
        panic(err)
    }
    h.Interactive() // starts the CLI loop
}
```

```bash
export GH_TOKEN=$(gh auth token)
go run ./cmd/example/
```

That's it. Tools execute Starlark inline. Hooks govern every lifecycle event. No Go code required for tool logic.

### Starlark scripting engine

All tools and hooks can be implemented entirely in Starlark (a Python-like language) embedded in the YAML. No Go code needed for tool logic.

**Available built-ins:**

| Category | Functions |
|----------|-----------|
| **Time** | `time.now()` |
| **JSON** | `json.encode(val)`, `json.decode(s)` |
| **Math** | `math.abs`, `math.min`, `math.max`, `math.floor`, `math.ceil` |
| **Runtime** | `os.cwd()`, `os.hostname()`, `os.platform()`, `os.args()` |
| **URL / IDs** | `url.parse(s)`, `url.encode(params)`, `uuid.v4()` |
| **Flow control** | `random(min, max)`, `sleep(ms)` |
| **Network** | `http.get(url, headers?, timeout_seconds?)`, `http.post(url, body?, headers?, timeout_seconds?)` |
| **Regex** | `re.match(pattern, text)`, `re.find_all(pattern, text)`, `re.replace(pattern, repl, text)` |
| **Hashing** | `hash.sha256(text)`, `hash.md5(text)` |
| **State** | `cache.set/get/has/delete/clear` |
| **I/O** | `env(key)`, `log(msg)` |
| **File read** | `fs.read(path)`, `fs.exists(path)`, `fs.list(path)`, `fs.stat(path)`, `fs.line_count(path)`, `fs.find(path, pattern)`, `fs.read_lines(path, start, end)` |
| **File write** | `fs.write(path, content)`, `fs.append(path, content)`, `fs.mkdir(path)`, `fs.remove(path)` |
| **File edit** | `fs.replace(path, old, new)`, `fs.replace_all(path, old, new)`, `fs.insert_at(path, line, content)`, `fs.replace_lines(path, start, end, content)`, `fs.delete_lines(path, start, end)` |
| **Hooks** | `allow()`, `block(reason)`, `modify(payload)` |

### Self-extending delegation

The `delegate` meta-tool lets the agent create sub-agents with custom tools at runtime:

```yaml
# The agent calls delegate automatically when it lacks a capability:
# delegate({
#   "task": "Count words in each .go file",
#   "tools": [{
#     "name": "count_words",
#     "description": "Count words in a file",
#     "script": "def run(args):\n    content = fs.read(args['path'])\n    return str(len(content.split()))"
#   }]
# })
```

**Delegation guardrails (harness-level, not prompting):**
- Max 5 tool iterations per delegate (fail fast)
- Built-in retry guard blocks tools after 2 consecutive errors
- Delegates never get the parent's `delegate` tool (no recursion)
- Task context auto-injected when tools have no declared parameters
- `delegate.pre` / `delegate.post` hooks can block or rewrite delegation requests and results
- Hooks can declare `when:` expressions to fire only for matching payloads (for example, only on specific tools)

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
The high-level entry point. It loads config, creates the completion client, context manager, hook system, tool registry, and agent.

```go
h, err := harness.New("harness.yaml")
result, err := h.Run(ctx, "Summarize this file")
```

Key methods:

- `New(configPath string) (*Harness, error)`
- `NewFromConfig(cfg *config.Config) (*Harness, error)`
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
YAML/JSON config loader and validator.

```go
cfg, err := config.Load("harness.yaml")
if err != nil {
  panic(err)
}

apiKey := cfg.ResolveAPIKey()
baseURL := cfg.BaseURL()
```

Capabilities:

- parse YAML and JSON
- apply defaults
- validate model, tool, and hook settings
- resolve API keys from environment variables

## Configuration reference

Example `harness.yaml`:

```yaml
model:
  provider: copilot         # openai | copilot | custom string
  name: gpt-4o              # required, non-empty
  max_tokens: 4096          # must be > 0
  temperature: 0.7          # must be between 0 and 2
  base_url: ""             # optional override; provider default used when empty
  api_key_env: GITHUB_TOKEN # env var to read API key from

context:
  max_history: 50           # max non-system messages retained
  max_tokens: 128000        # approximate context budget
  system_prompt: |
    You are a helpful AI assistant.

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
    handler: audit_log      # symbolic hook name resolved by your app
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
├── agent/          # Agent loop orchestration
├── cmd/example/    # Example CLI (just calls harness.New + Interactive)
├── completion/     # OpenAI-compatible client, including streaming
├── config/         # YAML/JSON config and validation
├── context/        # Conversation history manager
├── delegation/     # Delegate meta-tool (sub-agent creation)
├── harness/        # High-level API (wires everything together)
├── hooks/          # Lifecycle hook system
├── scripting/      # Starlark engine + fs/edit built-ins
├── tools/          # Tool registry and execution
├── harness.yaml    # Example configuration with inline tools
└── go.mod
```

## Contributing

Contributions are welcome. Keep changes small, add tests with code changes, and run:

```bash
go build ./...
go test ./... -cover
```

## License

MIT
