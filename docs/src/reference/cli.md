# CLI Reference

The `harness` binary is the single entry point for AI Harness. This page is the
exhaustive reference for every subcommand and every flag.

> **Versioning note.** Flag names, exit codes, and subcommand names listed here
> are part of the **stable CLI surface** under the project's SemVer policy
> (see `docs/src/project/`). Output formatting and INFO-level log fields are
> best-effort and may evolve between minor versions.

## Synopsis

```text
harness <command> [flags]
```

If invoked with no command, `harness` prints usage and exits with code `1`.

## Golden path

These are the commands you will use in roughly the order you reach for them:

| Command     | Purpose                                                         |
|-------------|-----------------------------------------------------------------|
| `scaffold`  | Create a new harness project in a new directory                 |
| `init`      | Initialize a harness in the current directory                   |
| `validate`  | Validate harness configuration without contacting the model     |
| `run`       | Start an interactive harness session (stdin REPL)               |
| `serve`     | Multi-source session: stdin + telegram + meshwire (long-lived)  |
| `deploy`    | Run the harness non-interactively (CI/CD, single prompt in/out) |
| `inspect`   | Snapshot of runtime state: tools, hooks, agents, artifacts      |

## Develop commands

| Command     | Purpose                                              |
|-------------|------------------------------------------------------|
| `tools`     | List registered tools                                |
| `hooks`     | List registered hooks                                |
| `agents`    | List configured agents                               |
| `artifacts` | List typed artifacts in the registry                 |
| `context`   | Show context window observability snapshot          |

## Other

| Command     | Purpose                                       |
|-------------|-----------------------------------------------|
| `version`   | Print version, commit hash, and build date    |
| `help`      | Print top-level usage (also `--help` / `-h`)  |

## Global flags

These flags are recognized **before** the subcommand dispatch and apply to
every command that loads the runtime (effectively everything except `version`
and `help`). They can also be set via environment variables.

| Flag                  | Env var                      | Default       | Description                                               |
|-----------------------|------------------------------|---------------|-----------------------------------------------------------|
| `--log-level <lvl>`   | `HARNESS_LOG_LEVEL`          | `info`        | One of `debug`, `info`, `warn`, `error`.                  |
| `--log-format <fmt>`  | `HARNESS_LOG_FORMAT`         | `text`        | One of `text` or `json`.                                  |
| `--otel-endpoint <u>` | `HARNESS_OTEL_ENDPOINT`      | _(unset)_     | OTLP/HTTP traces endpoint, e.g. `http://localhost:4318`. Unset = tracing disabled. |
| `--otel-sample <r>`   | `HARNESS_OTEL_SAMPLE_RATIO`  | `1.0`         | Trace sample ratio in `[0,1]`.                            |
| `--otel-service <n>`  | `HARNESS_OTEL_SERVICE_NAME`  | `ai-harness`  | `service.name` resource attribute for OTel.               |

Flag values take precedence over environment variables. Explicit `--otel-endpoint=""`
disables tracing even if the env var is set.

See the [Observability guide](../guides/observability.md) for the full OTel
attribute reference and a recipe for a local collector.

## Common flags

Several subcommands share these flags:

| Flag                   | Default | Description                                                                 |
|------------------------|---------|-----------------------------------------------------------------------------|
| `-c, --config <path>`  | _(auto)_| Path to harness config. Defaults to `harness.md` or `harness.yaml` in cwd.  |
| `-v, --verbose`        | `false` | Include per-component detail in human-readable output.                      |
| `--dir <path>`         | `.`     | Project directory to scan (artifacts/context/inspect).                      |
| `--json`               | `false` | Emit JSON instead of human-readable output (where supported).               |

---

## `scaffold`

```text
harness scaffold <name>
```

Create a new harness project in a **new directory**. Refuses to overwrite an
existing path.

**Arguments**

- `name` — project name **and** directory to create (required).

**Creates**

```text
<name>/harness.md                        # main harness configuration
<name>/.harness/tools/read_file.md       # starter tool: read_file
<name>/.harness/tools/write_file.md      # starter tool: write_file
<name>/.harness/hooks/safety.md          # starter safety hook
<name>/.harness/agents/                  # empty
```

**Exit codes**

- `0` on success
- `1` if the target directory already exists, or any filesystem write fails

**Example**

```bash
harness scaffold my-agent
cd my-agent
harness validate
harness run
```

---

## `init`

```text
harness init [name]
```

Initialize a harness **in the current directory** by copying core defaults.

**Arguments**

- `name` — project name to embed in the generated `harness.md` (default: the
  current directory name).

**Behavior**

- Refuses to overwrite an existing `harness.md` or `harness.yaml`.
- Copies the runtime's core tools and hooks into `.harness/`.

**Exit codes**

- `0` on success
- `1` if `harness.md`/`harness.yaml` exists, or any filesystem write fails

---

## `validate`

```text
harness validate [-c <path>] [-v]
```

Parse and validate the harness configuration without contacting the model.
Useful as a CI gate and as a fast feedback loop while iterating on
artifacts.

**Flags**

- `-c, --config <path>` — config path override.
- `-v, --verbose` — print every registered tool, hook, artifact, and agent.

**Exit codes**

- `0` if the configuration parses and resolves
- `1` on validation failure (missing keys, schema violations, duplicate names,
  unknown artifact kinds, etc.)

**Example**

```bash
harness validate -v
# tools: 21 registered
# hooks: 5 registered
# artifacts: 7 (kind=harness:1, plugin:2, builtin:4)
```

---

## `run`

```text
harness run [-c <path>] [--stream]
```

Start an **interactive** harness session backed by stdin. Each line you type
becomes a user message; the model's reply streams back to the terminal.

**Flags**

- `-c, --config <path>` — config path override.
- `--stream` — stream model tokens to the terminal as they arrive (Phase 5.4).

**Exit codes**

- `0` on clean EOF (Ctrl-D / Ctrl-Z)
- `1` on unrecoverable runtime error (model auth failure, config error, etc.)

---

## `serve`

```text
harness serve [-c <path>] --source <name> [--source <name> ...] [source-flags]
```

Long-lived multi-source session. Unlike `run`, `serve` is designed to keep
running and to accept input from multiple sources concurrently.

**Source flags**

| Flag                              | Description                                                      |
|-----------------------------------|------------------------------------------------------------------|
| `--source <name>`                 | Input source to enable. Repeatable. One of `stdin`, `telegram`, `meshwire`. |
| `--telegram-chat <id>`            | Allowlisted Telegram chat ID. Repeatable. Required when `--source telegram`. |
| `--telegram-poll <seconds>`       | Telegram long-poll timeout, max `50` (default `25`).             |
| `--meshwire-mesh <id>`            | MeshWire mesh ID. Required when `--source meshwire`.             |
| `--meshwire-agent <id>`           | This harness's `agent_id` within the mesh. Required when `--source meshwire`. |
| `--meshwire-sender <id>`          | Allowlisted peer `agent_id`. Repeatable. Required when `--source meshwire`. |
| `--meshwire-poll <seconds>`       | MeshWire long-poll timeout, max `60` (default `30`).             |
| `--meshwire-base <url>`           | Override MeshWire API base URL (default `https://meshwire.io`).  |

**Required environment variables**

- `--source telegram` → `TELEGRAM_BOT_TOKEN`
- `--source meshwire` → `MESHWIRE_API_KEY`

**Exit codes**

- `0` on clean shutdown (SIGINT / SIGTERM)
- `1` on unrecoverable runtime error
- `2` on configuration error (missing required source flag, unknown source, etc.)

**Example**

```bash
export TELEGRAM_BOT_TOKEN=...
harness serve \
  --source stdin \
  --source telegram --telegram-chat 7729308746
```

---

## `deploy`

```text
harness deploy [-c <path>] [--input <prompt>] [--dry-run]
```

Run the harness **non-interactively**: one input in, one final answer out. The
intended target is CI/CD and scripted automation.

**Flags**

- `-c, --config <path>` — config path override.
- `--input <prompt>` — input prompt. If omitted, reads the entire prompt from stdin.
- `--dry-run` — validate the config and print the execution plan **without**
  calling the model.

**Exit codes**

- `0` on a successful single-shot run
- `1` on runtime error (model failure, hook block, tool error, etc.)
- `2` on configuration error

**Example**

```bash
echo "summarize this PR" | harness deploy
harness deploy --input "say hello"
harness deploy --dry-run
```

---

## `inspect`

```text
harness inspect [-c <path>] [--dir <path>] [-v] [--events] [--failures]
```

One-shot snapshot of runtime state. Useful for sanity-checking a freshly
loaded configuration before you commit a change.

**Flags**

- `-c, --config <path>` — config path override.
- `--dir <path>` — project directory to inspect (default `.`).
- `-v, --verbose` — include parameters, hook scopes, agent details.
- `--events` — show recent events _(placeholder — requires runtime)_.
- `--failures` — show recent failures _(placeholder — requires runtime)_.

---

## `tools`, `hooks`, `agents`

```text
harness tools     [-c <path>] [-v]
harness hooks     [-c <path>] [-v]
harness agents    [-c <path>] [-v]
```

Narrower variants of `inspect` that print only one component class.

- `-c, --config <path>` — config path override.
- `-v, --verbose` — include parameters / hook details / agent details.

---

## `artifacts`

```text
harness artifacts [--dir <path>] [--type <kind>] [-v]
```

List typed artifacts in the registry (`harness_artifact/v1alpha1` files in
`.harness/{builtins,plugins,overrides}/`).

**Flags**

- `--dir <path>` — project directory to scan (default `.`).
- `--type <kind>` — filter by artifact kind: `override`, `harness`, `builtin`,
  `plugin`, or `model`.
- `-v, --verbose` — include artifact metadata (path, capabilities, priority).

---

## `context`

```text
harness context [--dir <path>] [--agent <name>] [--budget <tokens>] [--json] [-v]
```

Print the **context window observability** snapshot: which sections are
active, what their sources are, and how many tokens each contributes against
the budget. This is the externalization of the "what does the model actually
see?" question.

**Flags**

- `--dir <path>` — project directory to scan (default `.`).
- `--agent <name>` — resolve context for a specific named agent.
- `--budget <tokens>` — token budget for the context window (default `128000`).
- `--json` — emit machine-readable JSON.
- `-v, --verbose` — include provenance for every section.

See the [Observability guide](../guides/observability.md) for the broader
telemetry story.

---

## `version`

```text
harness version
```

Prints `harness <version> (commit: <sha>, built: <date>)`. Values are
injected at build time via `-ldflags`.

---

## Exit codes

| Code | Meaning                                                            |
|------|--------------------------------------------------------------------|
| `0`  | Success                                                            |
| `1`  | Runtime error (config error, model failure, hook block, etc.)      |
| `2`  | Flag-parsing or global-flag validation error (e.g. bad OTel value) |

Long-lived `serve` and `run` sessions translate `SIGINT`/`SIGTERM` to a clean
exit with code `0`.

---

## Environment variable summary

| Variable                       | Used by                  | Default       |
|--------------------------------|--------------------------|---------------|
| `HARNESS_LOG_LEVEL`            | global                   | `info`        |
| `HARNESS_LOG_FORMAT`           | global                   | `text`        |
| `HARNESS_OTEL_ENDPOINT`        | global                   | _(unset)_     |
| `HARNESS_OTEL_SAMPLE_RATIO`    | global                   | `1.0`         |
| `HARNESS_OTEL_SERVICE_NAME`    | global                   | `ai-harness`  |
| `TELEGRAM_BOT_TOKEN`           | `serve --source telegram`| _(required)_  |
| `MESHWIRE_API_KEY`             | `serve --source meshwire`| _(required)_  |
| `GH_TOKEN`                     | scaffolded default model | _(required)_  |

Model-provider credentials are configured per-harness in `harness.md` under
the `model.api_key_env` key, not via CLI flags.

---

## See also

- [Quickstart (5 minutes)](../getting-started.md)
- [`harness.md` Frontmatter reference](./harness-md.md)
- [Production Deployment guide](../guides/deployment.md)
- [Observability with OpenTelemetry](../guides/observability.md)
