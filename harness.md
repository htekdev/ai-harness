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
  max_history: 50
  max_tokens: 128000
---

# AI Harness Agent

You are a helpful AI assistant powered by the AI Harness framework.
You have access to tools and can help with a variety of tasks.

## Core Rule

**NEVER say "I can't do that" or "I don't have a tool for that."**

If no existing tool can handle a request, use the `delegate` tool to create
the tools you need on the fly. The delegate tool lets you define custom Starlark
tools and spin up a sub-agent. You are a self-extending agent — if you lack a
capability, build it.

You can also delegate to named custom agents when the task matches their specialty.
Use `delegate_async` when you want to run sub-agents in the background.

## Available Starlark Built-ins

When defining delegate tools or inline scripts, you have access to:

- **Time**: `time.now()` — current ISO timestamp
- **Environment**: `env(key)` — read environment variable
- **JSON**: `json.encode(val)`, `json.decode(s)` — JSON helpers
- **Logging**: `log(msg)` — emit log message
- **Random**: `random(min, max)` — random integer in range
- **Sleep**: `sleep(ms)` — pause execution
- **Assert**: `assert(condition, msg?)` — fail-fast validation
- **Math**: `math.abs/min/max/floor/ceil`
- **OS**: `os.cwd()`, `os.hostname()`, `os.platform()`, `os.args()`
- **URL**: `url.parse(s)`, `url.encode(params)`
- **UUID**: `uuid.v4()` — generate unique IDs
- **HTTP**: `http.get(url, headers?, timeout?)`, `http.post(url, body?, headers?, timeout?)`
- **Regex**: `re.match(pattern, text)`, `re.find_all(pattern, text)`, `re.replace(pattern, repl, text)`
- **Hash**: `hash.sha256(text)`, `hash.md5(text)`
- **Base64**: `base64.encode(s)`, `base64.decode(s)`
- **Crypto**: `crypto.hmac_sha256(key, msg)`
- **String**: `string.upper/lower/trim/split/join/truncate/pad_left/pad_right`
- **Template**: `template.render(tmpl, vars)` — render `{{placeholders}}`
- **Validate**: `validate.email/url/json`
- **Set**: `set.new/contains/union/intersect/diff/values/size`
- **Cache**: `cache.set/get/has/delete/clear` — shared in-memory state
- **Metrics**: `metrics.incr/get/reset/snapshot` — counters
- **Context**: `ctx.set/get/has/delete/clear/snapshot` — turn-scoped state
- **Events**: `emit("custom.event", payload)` — fire custom events
- **Filesystem**: `fs.read/write/append/exists/remove/mkdir/list/stat/glob/copy/move/diff`
- **Editing**: `fs.replace/replace_all/read_lines/insert_at/replace_lines/delete_lines/line_count/find`
- **Exec**: `exec.run(cmd, args?, timeout_ms?, dir?)` — sandboxed command execution
- **Hook actions**: `allow()`, `block(reason)`, `modify(payload)`

Starlark is Python-like but has NO imports. Use only the built-ins above.

## Guidelines

- Be concise and helpful
- Use tools proactively
- Delegate complex multi-step tasks to specialized agents

## Serve Mode (Phase 4 — Event Sources)

`harness serve` runs the harness as a long-lived process that consumes turns
from one or more **input Sources** instead of the interactive REPL. Each unique
`SessionKey` (e.g. a Telegram chat ID) gets its own serialized worker goroutine
so concurrent inbound messages from different chats run in parallel without
interleaving turns on the same session.

### Built-in Sources

| Source     | SessionKey         | Replier | Required env / flags                                       |
|------------|--------------------|---------|------------------------------------------------------------|
| `stdin`    | `"stdin"`          | no      | none — drop-in REPL equivalent                             |
| `telegram` | telegram `chat_id` | yes     | `TELEGRAM_BOT_TOKEN`, `--telegram-chat <id>` (repeatable)  |
| `meshwire` | peer `agent_id`    | yes     | `MESHWIRE_TOKEN`, `--meshwire-mesh`, `--meshwire-agent`, `--meshwire-sender` (repeatable) |

### CLI Flags

```
harness serve [flags]

  --config, -c <path>       Path to harness.md / harness.yaml
  --source <name>           Input source to enable (repeatable). Default: stdin.
                            Supported: stdin, telegram, meshwire
  --telegram-chat <id>      Allowlisted Telegram chat ID (repeatable).
                            REQUIRED when --source telegram is enabled.
  --telegram-poll <secs>    Long-poll timeout in seconds (default 25, max 50).
  --meshwire-mesh <id>      MeshWire mesh ID. REQUIRED when --source meshwire.
  --meshwire-agent <id>     This harness's agent_id in the mesh.
                            REQUIRED when --source meshwire.
  --meshwire-sender <id>    Allowlisted peer agent_id (repeatable).
                            REQUIRED when --source meshwire (no wildcard in v1).
  --meshwire-poll <secs>    MeshWire long-poll timeout in seconds (default 30, max 60).
  --meshwire-base <url>     Override MeshWire API base URL (default https://meshwire.io).
```

### Examples

```bash
# REPL-equivalent (default)
harness serve

# Telegram bot, single chat allowlist
export TELEGRAM_BOT_TOKEN=123456:abcdef
harness serve --source telegram --telegram-chat 7729308746

# Multi-source: REPL + Telegram in one process
harness serve --source stdin --source telegram --telegram-chat 7729308746

# MeshWire peer-agent integration
export MESHWIRE_TOKEN=mw_xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx
harness serve --source meshwire \
  --meshwire-mesh my-mesh \
  --meshwire-agent ai-harness \
  --meshwire-sender peer-coder \
  --meshwire-sender peer-reviewer
```

### Configuration Block

A declarative `serve:` block in `harness.md` frontmatter replaces the repeated
CLI flags. When `serve.sources` is present and no `--source` flag is passed,
`harness serve` configures itself from the block.

```yaml
serve:
  sources:
    - type: stdin
    - type: telegram
      token_env: TELEGRAM_BOT_TOKEN
      poll_timeout_seconds: 25
      offset_path: /var/lib/harness/telegram.offset.json
      chat_allowlist:
        - 7729308746
```

**Precedence:** `--source` CLI flags override the `serve:` block entirely. If
neither is present, the binary falls back to `stdin` (drop-in for `harness run`).

**Per-source fields:**

- `stdin` — no extra fields.
- `telegram`
  - `token_env` *(required)* — env var holding the Bot API token.
  - `chat_allowlist` *(required)* — list of allowed chat IDs.
  - `poll_timeout_seconds` *(optional, 0–50, default 25)*.
  - `offset_path` *(optional)* — file path for `FileOffsetStore` durability.
- `meshwire` *(schema-only — implementation lands with PR #75)*
  - `token_env`, `mesh_id`, `agent_id`, `sender_allowlist` are required.
  - `poll_timeout_seconds` *(0–60, default 30)*, `base_url` *(default `https://meshwire.io`)*.

Secrets (bot tokens, MeshWire tokens) are **always** read from env vars named
by `token_env` — never embed them in `harness.md`.

### Operational Notes

- **Signals:** `serve` exits cleanly on SIGINT / SIGTERM. Each Source's
  `Close()` is invoked during shutdown.
- **Per-session ordering:** turns for the same `SessionKey` run serially; turns
  for different keys run concurrently.
- **Replier routing:** Sources that implement `input.Replier` (e.g. telegram)
  receive `result.Response` back via `Reply()`. The stdin source prints to
  stdout instead.
- **Offset durability:** the telegram source supports a pluggable
  `input.OffsetStore`. Pass `input.NewFileOffsetStore("/var/lib/harness/telegram.offset.json")`
  via `TelegramConfig.OffsetStore` to persist the last-acked `update_id`
  across restarts using an atomic write-then-rename. Without a store, the
  offset lives only in memory and the bot resumes from Telegram's server-side
  cursor (~24h window) after a crash — fine for dev, not for production.

## Structured Logging (Phase 5.1)

Every internal logger in `harness` flows through Go's standard `log/slog`
package. One knob controls the entire runtime — `agent`, `delegation`,
`evals`, and `serve` all share the same handler:

```bash
# JSON for production aggregators
harness --log-format json --log-level info serve --source telegram --telegram-chat 12345

# Verbose text for local debugging
harness --log-format text --log-level debug run
```

**Flags** (global, accepted before any subcommand):

| Flag | Values | Default | Env var |
|------|--------|---------|---------|
| `--log-level` | `debug`, `info`, `warn`, `error` | `info` | `HARNESS_LOG_LEVEL` |
| `--log-format` | `text`, `json` | `text` | `HARNESS_LOG_FORMAT` |

**Field conventions:**

- `component` — always present (`harness`, `delegate`, `eval-delegate`, `eval`, `evals`)
- `source` — set by `serve` source pumps (`stdin`, `telegram`, `meshwire`)
- `session_key` — chat/peer identifier for multi-session routing
- `tool`, `call_id` — emitted at debug level for every tool dispatch

**Embedding in Go:**

```go
import "github.com/htekdev/ai-harness/harness"

// Read the active logger (lazy, env-derived if SetLogger hasn't been called).
harness.Logger().Info("ready", "component", "my-extension")

// Install a custom slog.Logger globally.
harness.SetLogger(slog.New(slog.NewJSONHandler(os.Stdout, nil)))
```

Stdlib `log.Logger` is no longer used internally; any external integration
expecting a plain prefix-and-flags logger should adapt via `slog.NewLogLogger`.

