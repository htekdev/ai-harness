# `harness.md` Frontmatter Reference

`harness.md` is the **root artifact** of every AI Harness project. It is a
Markdown file with a YAML frontmatter block: the frontmatter declares the
runtime configuration; the body becomes the system prompt.

This page is the exhaustive reference for every field the loader recognizes,
the type and default for each, and a worked example for the non-obvious
ones.

> **Versioning note.** Every field documented on this page is part of the
> stable harness configuration surface under SemVer. Fields marked
> *experimental* may change; new optional fields may be added in minor
> releases without breaking existing files.

## File shape

```markdown
---
# YAML frontmatter — runtime configuration
model:
  provider: copilot
  name: gpt-4o
context:
  max_history: 50
delegation:
  max_depth: 2
---

# Markdown body — becomes the system prompt

You are a careful assistant. ...
```

Rules enforced by the loader (`config.LoadMarkdown` in `config/markdown.go`):

1. The file **must** start with a `---` delimiter line.
2. The frontmatter **must** be closed by a second `---` on its own line.
3. The body after the closing delimiter is the system prompt. If empty, no
   system prompt is set from this file.
4. Frontmatter is parsed as YAML. Unknown top-level keys are **ignored**
   silently — typos in field names produce no error. Use `harness validate`
   to confirm the runtime sees what you expect.

`harness.md` may also be supplied as plain `harness.yaml` / `harness.yml`
for environments where Markdown is awkward; the schema is identical and
no system prompt is read from the file.

## Top-level fields

| Field          | Type                              | Default        | Required |
|----------------|-----------------------------------|----------------|----------|
| `model`        | [Model](#model)                   | see below      | no       |
| `models`       | [\[Model\]](#model)               | _empty_        | no       |
| `context`      | [Context](#context)               | see below      | no       |
| `tools`        | [\[Tool\]](#tools)                | _empty_        | no       |
| `tools_policy` | [ToolsPolicy](#tools_policy)      | _no policy_    | no       |
| `hooks`        | [\[Hook\]](#hooks)                | _empty_        | no       |
| `delegation`   | [Delegation](#delegation)         | see below      | no       |
| `meta`         | [Meta](#meta)                     | _disabled_     | no       |
| `serve`        | [Serve](#serve)                   | _none_         | no       |
| `network`      | [Network](#network)               | _unrestricted_ | no       |

The minimal valid frontmatter is an empty block — defaults will fill in a
working `gpt-4o` profile against the GitHub Copilot endpoint, provided
`GITHUB_TOKEN` is set in the environment.

---

## `model`

The primary completion model. Exactly one `model` block is active per turn;
if `models` is also set, it becomes a routing table (see [`models`](#models)).

```yaml
model:
  provider: copilot
  name: gpt-4o
  max_tokens: 4096
  temperature: 0.3
  base_url: https://api.githubcopilot.com
  api_key_env: GH_TOKEN
  retry:
    max_retries: 3
    initial_backoff_ms: 250
    max_backoff_ms: 8000
    multiplier: 2.0
```

| Field         | Type    | Default                          | Notes                                                                                    |
|---------------|---------|----------------------------------|------------------------------------------------------------------------------------------|
| `name`        | string  | `gpt-4o`                         | Provider-specific model identifier. Must be non-empty after defaults.                    |
| `provider`    | string  | `openai`                         | One of `openai`, `copilot`. Drives default `base_url` selection.                         |
| `max_tokens`  | int     | `4096`                           | Per-completion cap. Must be `> 0`.                                                       |
| `temperature` | float   | `0.7`                            | Must be in `[0.0, 2.0]`.                                                                 |
| `base_url`    | string  | derived from `provider`          | Override for proxies / Azure OpenAI / local gateways. `copilot` → `https://api.githubcopilot.com`; `openai` → `https://api.openai.com/v1`. |
| `api_key_env` | string  | `GITHUB_TOKEN`                   | Name of the env var that holds the API key. The harness **never** reads keys from frontmatter directly. |
| `retry`       | [Retry](#retry) | _harness defaults_         | Per-model retry policy for completion errors.                                            |

### `retry`

Retry policy applied to model completion calls (not tool calls). All fields
optional; absent fields fall back to harness-level defaults.

| Field                | Type   | Constraint   | Notes                                                              |
|----------------------|--------|--------------|--------------------------------------------------------------------|
| `max_retries`        | int    | `>= 0`       | `0` disables retries entirely.                                     |
| `initial_backoff_ms` | int    | `>= 0`       | First sleep before retry #1.                                       |
| `max_backoff_ms`     | int    | `>= 0`       | Upper bound on the backoff after multiplier expansion.             |
| `multiplier`         | float  | `>= 0`       | Geometric growth factor between retries.                           |

> Retry kicks in for transient completion errors and `finish_reason=length`
> truncation (see [PR #121](https://github.com/htekdev/ai-harness/pull/121)).
> `finish_reason=content_filter` is a hard error and is **not** retried.

---

## `models`

An optional list of additional model profiles available at runtime.

```yaml
models:
  - name: gpt-4o
    provider: copilot
    api_key_env: GH_TOKEN
    retry:
      max_retries: 3
  - name: gpt-4o-mini
    provider: copilot
    api_key_env: GH_TOKEN
```

Each entry has the same schema as [`model`](#model). The first entry is the
default at boot; sub-agents and tools may switch profiles by name. When
`models` is empty, the single `model` block is the only profile.

---

## `context`

Context-window management.

```yaml
context:
  max_history: 50
  max_tokens: 64000
  system_prompt: ""
```

| Field           | Type   | Default  | Notes                                                                                              |
|-----------------|--------|----------|----------------------------------------------------------------------------------------------------|
| `max_history`   | int    | `50`     | Max turns retained in the rolling history before compaction.                                       |
| `max_tokens`    | int    | `128000` | Soft budget for the assembled prompt. Compaction kicks in before this is exceeded.                 |
| `system_prompt` | string | `""`     | Inline system prompt. **Overridden by the Markdown body** if the file has one (preferred path).    |

Setting `system_prompt` in frontmatter is supported for `.yaml` configs and
for tests; in `.md` files prefer writing the prompt as the body.

---

## `tools`

Inline tool definitions. Each entry registers one tool with a single
`harness.md`-resident Starlark script. Most projects keep tools as **separate
artifacts** in `.harness/tools/<name>.md` instead — see
[Tool Artifact Schema](./tool-artifact.md) — but the inline form remains
supported for small examples and tests.

```yaml
tools:
  - name: echo
    description: Echo a message back.
    timeout_ms: 1000
    parameters:
      message:
        type: string
        description: What to echo
        required: true
    script: |
      def run(message):
          return message
```

| Field         | Type                         | Required | Notes                                                            |
|---------------|------------------------------|----------|------------------------------------------------------------------|
| `name`        | string                       | yes      | Unique within the harness. Duplicates fail validation.           |
| `description` | string                       | no       | Surfaced to the model in the tool listing.                       |
| `parameters`  | map[string][Param](#param)   | no       | Tool argument schema.                                            |
| `timeout_ms`  | int                          | no       | Must be `>= 0`. `0` means harness default.                       |
| `script`      | string                       | no       | Starlark source. Required if the tool has no other handler.      |

### `param`

| Field         | Type    | Default | Notes                                |
|---------------|---------|---------|--------------------------------------|
| `type`        | string  | —       | One of `string`, `int`, `bool`, `object`, `array`. |
| `description` | string  | `""`    | Surfaced to the model.               |
| `required`    | bool    | `false` | Validation: missing required params produce a tool error before the script runs. |

---

## `tools_policy`

Declarative governance over which registered tools the agent may invoke.
Patterns are shell-style globs evaluated against tool names (e.g.
`fs.*`, `delegate*`, `web_fetch`).

```yaml
tools_policy:
  mode: allowlist
  allow:
    - "fs.read"
    - "fs.list"
    - "web_fetch"
    - "delegate*"
  deny:
    - "fs.remove"
    - "exec"
```

| Field   | Type        | Default                         | Notes                                                                                  |
|---------|-------------|---------------------------------|----------------------------------------------------------------------------------------|
| `mode`  | string      | inferred                        | `allowlist` or `denylist`. When omitted: a non-empty `allow` ⇒ `allowlist`, else `denylist`. |
| `allow` | \[string\]  | _empty_                         | Patterns the agent may call.                                                           |
| `deny`  | \[string\]  | _empty_                         | Patterns the agent may **not** call. Deny always wins over allow.                      |

Policy is enforced at the registry level: a denied call never reaches the
tool's Starlark script, and the OTel span is marked
`tool.policy=denied`. See the [Governance & Policy](../concepts/governance.md)
concept page.

---

## `hooks`

Inline hook registrations. As with tools, most projects ship hooks as
separate artifacts in `.harness/hooks/<name>.md` (see
[Hook Artifact Schema](./hook-artifact.md)); the inline form is for small
examples and tests.

```yaml
hooks:
  - event: tool.pre
    handler: audit_pre
    when: 'payload["name"] == "fs.read"'
    priority: 100
    script: |
      def handle(event, payload):
          metrics.incr("audit.read")
          return {"action": "allow"}
```

| Field      | Type   | Required | Notes                                                                                       |
|------------|--------|----------|---------------------------------------------------------------------------------------------|
| `event`    | string | yes      | Must be a recognized event name. Validation rejects unknown events.                         |
| `handler`  | string | yes      | Stable identifier for traces and logs. Inline hooks may reuse the handler name only once.   |
| `when`     | string | no       | Starlark expression evaluated against the event payload before the hook runs.               |
| `priority` | int    | no       | Lower numbers run first. Default `0`.                                                        |
| `script`   | string | yes\*    | Starlark source. Optional only if the hook references an existing handler by name.          |

Recognized event names (full list in [Hook Artifact Schema](./hook-artifact.md)):

- `session.start`, `session.end`
- `turn.start`, `turn.end`, `agent.stop`
- `tool.pre`, `tool.post`
- `completion.pre`, `completion.post`
- `delegation.pre`, `delegation.post`, `delegation.post_verify`

---

## `delegation`

Sub-agent delegation budget.

```yaml
delegation:
  max_depth: 2
  max_concurrent: 4
  iterations_per_depth: [12, 6]
```

| Field                  | Type        | Default | Notes                                                                                    |
|------------------------|-------------|---------|------------------------------------------------------------------------------------------|
| `max_depth`            | int         | `1`     | Maximum sub-agent depth. `0` disables delegation entirely.                               |
| `max_concurrent`       | int         | `1`     | Cap on simultaneous in-flight delegations across the whole tree.                         |
| `iterations_per_depth` | \[int\]     | _none_  | Per-depth turn budget. `[12, 6]` ⇒ root agent gets 12 turns, depth-1 sub-agents get 6.   |

When `iterations_per_depth` has fewer entries than `max_depth`, the last
entry is reused for deeper levels.

---

## `meta`

Configuration for the `meta.*` Starlark built-ins (self-augmenting agents).
All fields are required when `meta` is present.

```yaml
meta:
  enabled: true
  max_tools: 20
  max_hooks: 20
  max_agents: 5
  max_call_depth: 2
```

| Field            | Type | Notes                                                                          |
|------------------|------|--------------------------------------------------------------------------------|
| `enabled`        | bool | Master switch. When `false`, every `meta.*` call returns an error.             |
| `max_tools`      | int  | Cap on dynamically registered tools across a single run.                        |
| `max_hooks`      | int  | Cap on dynamically registered hooks across a single run.                        |
| `max_agents`     | int  | Cap on dynamically registered agents across a single run.                        |
| `max_call_depth` | int  | Maximum nesting depth for `meta.*` calls (prevents recursive self-augmentation). |

Dynamically registered tools are still subject to `tools_policy` —
`meta.register_tool` cannot bypass governance.

---

## `serve`

Declarative configuration for `harness serve`. Replaces the repeated
`--source` / `--telegram-*` CLI flags. **Secrets are never embedded** —
each source references an env var via `token_env`.

```yaml
serve:
  sources:
    - type: stdin
    - type: telegram
      token_env: TELEGRAM_BOT_TOKEN
      poll_timeout_seconds: 25
      chat_allowlist: [7729308746]
      offset_path: ./.harness/state/telegram-offset.json
    - type: meshwire
      token_env: MESHWIRE_TOKEN
      mesh_id: family-mesh
      agent_id: harness-bot
      sender_allowlist: [peer-reviewer]
      poll_timeout_seconds: 30
      base_url: https://meshwire.io
```

`serve.sources` must contain at least one entry. Duplicate types are not
supported in v1. Unknown `type` values produce a validation error so a
stale binary running newer config fails loudly instead of silently dropping
sources.

### Per-source fields

#### `type: stdin`

No required fields. Reads prompts from standard input; emits replies to
standard output. Equivalent to `harness run` but participates in the
multi-source dispatch loop.

#### `type: telegram`

| Field                  | Type      | Required | Constraint     | Notes                                                  |
|------------------------|-----------|----------|----------------|--------------------------------------------------------|
| `token_env`            | string    | yes      | non-empty      | Env var holding the Bot API token.                     |
| `chat_allowlist`       | \[int64\] | yes      | non-empty      | Telegram chat IDs allowed to invoke the harness.       |
| `poll_timeout_seconds` | int       | no       | `0..50`        | Long-poll timeout. `0` ⇒ source default.               |
| `offset_path`          | string    | no       | —              | File path for durable `update_id` persistence.         |

#### `type: meshwire`

| Field                  | Type        | Required | Constraint  | Notes                                                       |
|------------------------|-------------|----------|-------------|-------------------------------------------------------------|
| `token_env`            | string      | yes      | non-empty   | Env var holding the MeshWire auth token.                    |
| `mesh_id`              | string      | yes      | non-empty   | MeshWire mesh this harness joins.                           |
| `agent_id`             | string      | yes      | non-empty   | This harness's `agent_id` within the mesh.                  |
| `sender_allowlist`     | \[string\]  | yes      | non-empty   | Peer `agent_id`s whose messages this harness will accept.    |
| `poll_timeout_seconds` | int         | no       | `0..60`     | Long-poll timeout. `0` ⇒ source default.                    |
| `base_url`             | string      | no       | —           | Default `https://meshwire.io`.                              |

---

## `network`

Network sandbox enforced by the `http.*` Starlark built-ins.

```yaml
network:
  allowed_domains:
    - api.github.com
    - "*.example.com"
```

| Field             | Type        | Default | Notes                                                                         |
|-------------------|-------------|---------|-------------------------------------------------------------------------------|
| `allowed_domains` | \[string\]  | _empty_ | When non-empty, switches to default-deny. Each entry matches the host and its sub-domains. The literal entry `"*"` disables host filtering while still rejecting non-`http(s)` schemes. |

When `network` is omitted (or `allowed_domains` is empty), scripts may
reach any host. This preserves backward compatibility with pre-5.5 configs.
See the [Network Sandboxing](../guides/network-sandbox.md) guide for full
matching rules.

---

## Defaults summary

The loader applies these defaults before validation:

| Field                  | Default          |
|------------------------|------------------|
| `model.name`           | `gpt-4o`         |
| `model.provider`       | `openai`         |
| `model.max_tokens`     | `4096`           |
| `model.temperature`    | `0.7`            |
| `model.api_key_env`    | `GITHUB_TOKEN`   |
| `model.base_url`       | derived          |
| `context.max_history`  | `50`             |
| `context.max_tokens`   | `128000`         |
| `delegation.max_depth` | `1`              |
| `delegation.max_concurrent` | `1`         |

## Validation

`harness validate` runs the same checks the runtime applies at boot:

- `model.name` non-empty
- `model.temperature` in `[0, 2]`
- `model.max_tokens > 0`
- `tool.timeout_ms >= 0`
- No duplicate tool names
- Every hook `event` is a recognized event
- `tools_policy.mode` (if set) is `allowlist` or `denylist`
- All `tools_policy.allow` / `deny` entries are non-empty strings
- `serve.sources` non-empty when `serve` is present, with per-source
  required fields enforced
- `model.retry` and per-`models[i].retry` field bounds (`max_retries >= 0`,
  backoffs `>= 0`, `multiplier >= 0`)

Validation errors are joined into one message: each individual issue is
listed so a CI run shows everything wrong in one pass.

## Worked example

The flagship [governed-agent example](../examples/governed-agent.md) ships
a complete `harness.md` exercising every governance primitive. Use it as
the copy-paste baseline:

- Two `models` profiles (primary + cheap fallback)
- `tools_policy` allowlist with explicit denies
- `delegation` budget with per-depth iteration caps
- `meta` enabled with caps
- Companion artifacts under `.harness/tools/` and `.harness/hooks/`

## See also

- [Tool Artifact Schema](./tool-artifact.md) — the per-tool `.md` shape
- [Hook Artifact Schema](./hook-artifact.md) — the per-hook `.md` shape
- [Starlark Built-ins](./starlark-builtins.md) — what scripts can call
- [CLI Reference](./cli.md) — flags and env vars that interact with this file
- [Governance & Policy](../concepts/governance.md) — how `tools_policy` and hooks compose
- [Network Sandboxing](../guides/network-sandbox.md) — full `network.allowed_domains` matching rules
