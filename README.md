# AI Harness

A minimal, extensible AI agent harness in Go for building tool-using agents without committing to a large framework. It gives you a small set of composable packages for configuration, model I/O, tools, hooks, context management, and a high-level harness API.

## Why this project exists

Most agent frameworks are either too opinionated or too hard to govern. `ai-harness` aims for a smaller surface area:

- **Minimal**: plain Go structs and interfaces
- **Extensible**: register tools and hooks at runtime
- **Configurable**: YAML-driven setup via `harness.yaml`
- **Governable**: lifecycle hooks for audit, blocking, and modification
- **Portable**: works with GitHub Copilot and OpenAI-compatible APIs

## Architecture

```
┌─────────────────────────────────────────────────────┐
│                    AI Harness                         │
├─────────────────────────────────────────────────────┤
│                                                      │
│  ┌──────────┐    ┌──────────┐    ┌──────────────┐  │
│  │  Config  │───▶│  Agent   │───▶│  Completion  │  │
│  │  (YAML)  │    │  Loop    │    │  Client      │  │
│  └──────────┘    └────┬─────┘    └──────────────┘  │
│                       │                              │
│              ┌────────┼────────┐                    │
│              ▼        ▼        ▼                    │
│  ┌──────────────┐ ┌──────┐ ┌──────────┐           │
│  │    Hook      │ │ Tool │ │ Context  │           │
│  │   System     │ │ Reg  │ │ Manager  │           │
│  └──────────────┘ └──────┘ └──────────┘           │
│                                                      │
└─────────────────────────────────────────────────────┘
```

## Installation

```bash
go get github.com/htekdev/ai-harness
go install github.com/htekdev/ai-harness/cmd/example@latest
```

## Quick start

### High-level API (`harness` package)

```go
package main

import (
  "context"
  "encoding/json"
  "fmt"

  "github.com/htekdev/ai-harness/harness"
  "github.com/htekdev/ai-harness/hooks"
  "github.com/htekdev/ai-harness/tools"
)

func main() {
  h, err := harness.New("harness.yaml")
  if err != nil {
    panic(err)
  }

  err = h.RegisterTool(tools.Definition{
    Name:        "echo",
    Description: "Echo a message",
    Parameters: []tools.Parameter{
      {Name: "message", Type: tools.TypeString, Description: "Message to echo", Required: true},
    },
  }, func(ctx context.Context, args json.RawMessage) (string, error) {
    var payload struct {
      Message string `json:"message"`
    }
    if err := json.Unmarshal(args, &payload); err != nil {
      return "", err
    }
    return payload.Message, nil
  })
  if err != nil {
    panic(err)
  }

  h.RegisterHook(hooks.Registration{
    Name:  "audit_log",
    Event: hooks.EventToolPre,
    Handler: func(ctx context.Context, event hooks.Event, payload any) hooks.Result {
      return hooks.Result{Action: hooks.ActionContinue}
    },
  })

  if err := h.RunSession(context.Background()); err != nil {
    panic(err)
  }
  defer h.EndSession(context.Background())

  result, err := h.Run(context.Background(), "Say hello and use the echo tool.")
  if err != nil {
    panic(err)
  }

  fmt.Println(result.Response)
}
```

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
export GITHUB_TOKEN=your_token

# Windows PowerShell
$env:GITHUB_TOKEN = "your_token"

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

- Configured tools are pre-registered as definitions with an **unimplemented placeholder handler** until you supply a real handler.
- Configured hooks are pre-registered by **name and event** with a placeholder handler until you replace them with `RegisterHook`.

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
| `handler` | string | yes | Symbolic handler name to register later |

## Hook system

### Events

Valid events:

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
├── cmd/example/    # Example CLI
├── completion/     # OpenAI-compatible client, including streaming
├── config/         # YAML/JSON config and validation
├── context/        # Conversation history manager
├── harness/        # High-level API
├── hooks/          # Lifecycle hook system
├── tools/          # Tool registry and execution
├── harness.yaml    # Example configuration
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
