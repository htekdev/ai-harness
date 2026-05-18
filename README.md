# AI Harness

A minimal, extensible AI agent harness in Go. Built for composability, governance, and runtime flexibility.

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

## Core Components

### Agent Loop (`agent/`)
The heart of the harness. Manages the turn-based conversation:
1. Accept user input
2. Build context (system prompt + history + tool definitions)
3. Call completion API
4. If tool calls → execute tools → loop back to step 3
5. Return final assistant response

### Tool Registry (`tools/`)
Register, discover, and invoke tools at runtime:
- Static tool registration via config
- Dynamic tool creation (architected for runtime tool generation)
- JSON Schema-based parameter validation
- Tool execution with timeout and error handling

### Completion Client (`completion/`)
OpenAI-compatible completion client:
- Supports GitHub Copilot API endpoint
- Supports any OpenAI-compatible API
- Streaming and non-streaming modes
- Automatic retry with backoff

### Hook System (`hooks/`)
Governance injection points throughout the agent lifecycle:
- `session.start` / `session.end`
- `turn.start` / `turn.end`
- `tool.pre` / `tool.post`
- `completion.pre` / `completion.post`
- Hooks can modify, block, or audit any operation
- Architected for per-prompt governance (HookFlow pattern)

### Context Manager (`context/`)
Manages conversation history and context window:
- Token-aware context truncation
- System prompt management
- Tool result injection
- Context caching support

### Configuration (`config/`)
YAML-based harness configuration:
- Model selection and parameters
- Tool definitions and permissions
- Hook registration
- Context limits

## Vision (Roadmap)

This harness is the foundation for:

1. **HookFlow Integration** — Dynamic governance rules that can be defined per-prompt, enabling fine-grained control over what tools are available, what actions are allowed, and how the agent behaves for each specific request.

2. **Dynamic Tool Creation** — Tools that can be created, modified, and composed at runtime. Not just static MCP servers, but tools that emerge from context and need.

3. **Per-Prompt Configuration** — Each prompt can carry its own harness configuration, defining governance rules, available tools, and model parameters. The harness reshapes itself for every turn.

## Quick Start

```bash
# Build
go build ./...

# Run the example
export GITHUB_TOKEN=your_copilot_token
go run ./cmd/example/

# Run tests
go test ./...
```

## Configuration

```yaml
# harness.yaml
model:
  provider: "copilot"
  name: "gpt-4o"
  max_tokens: 4096
  temperature: 0.7

context:
  max_history: 50
  max_tokens: 128000
  system_prompt: "You are a helpful assistant."

tools:
  - name: "read_file"
    description: "Read a file from disk"
    parameters:
      path:
        type: string
        description: "The file path to read"
        required: true

hooks:
  - event: "tool.pre"
    handler: "audit_log"
  - event: "session.start"
    handler: "load_governance"
```

## Project Structure

```
ai-harness/
├── agent/          # Agent loop orchestration
├── completion/     # LLM completion client
├── config/         # Configuration loading
├── context/        # Context/history management
├── hooks/          # Hook system for governance
├── tools/          # Tool registry and execution
├── cmd/
│   └── example/    # Working demo
├── harness.yaml    # Example config
└── go.mod
```

## License

MIT
