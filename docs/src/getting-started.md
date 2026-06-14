# Quickstart

A working AI Harness agent in **five minutes**. By the end you will have:

- Installed the `harness` binary.
- Written a one-file `harness.md` that defines an agent, a tool, and a hook.
- Run a one-shot turn against a real model.
- Validated the governance path (the agent will *refuse* a dangerous tool call).

> **Time budget:** ~5 minutes if you already have a `GH_TOKEN` or `OPENAI_API_KEY`.
> Add a minute or two if you need to mint one.

---

## 1. Install

### Option A — Pre-built binary (recommended)

Download the latest release from
[github.com/htekdev/ai-harness/releases](https://github.com/htekdev/ai-harness/releases)
and put `harness` on your `PATH`.

```bash
# Linux / macOS
curl -fsSL https://github.com/htekdev/ai-harness/releases/latest/download/harness-$(uname -s)-$(uname -m).tar.gz \
  | tar -xz -C /usr/local/bin harness
harness --version
```

### Option B — Build from source

Requires Go 1.25 or later.

```bash
git clone https://github.com/htekdev/ai-harness.git
cd ai-harness
go install ./cmd/harness
harness --version
```

### Option C — Docker

```bash
docker run --rm -it \
  -e GH_TOKEN=$GH_TOKEN \
  -v $(pwd):/work -w /work \
  ghcr.io/htekdev/ai-harness:latest run \
  --config harness.md "Hello!"
```

See [Production Deployment](./guides/deployment.md) for hardened systemd /
Docker recipes.

---

## 2. Get a provider token

AI Harness speaks the OpenAI chat-completions wire format. Any compatible
provider works; the two most common are:

| Provider                | Env var          | How to mint                                                  |
| ----------------------- | ---------------- | ------------------------------------------------------------ |
| **GitHub Models / Copilot** | `GH_TOKEN`       | `gh auth token` (with `models:read` scope), or PAT.          |
| **OpenAI**              | `OPENAI_API_KEY` | <https://platform.openai.com/api-keys>                       |

```bash
export GH_TOKEN="ghp_xxx"        # Linux / macOS
# $env:GH_TOKEN = "ghp_xxx"      # Windows PowerShell
```

---

## 3. Write your first `harness.md`

Create an empty directory and drop these files in.

### `harness.md`

````markdown
---
model:
  provider: github
  name: gpt-4o-mini
  retry:
    max_attempts: 3
    initial_backoff_ms: 500

context:
  files: []

tools_policy:
  mode: allowlist
  allow:
    - greet
    - fs.read
  deny:
    - exec
    - fs.remove

delegation:
  max_depth: 1
---

You are a friendly demo agent for AI Harness.

When the user greets you, call the `greet` tool with their name and return
its output verbatim. If they ask you to run shell commands or delete files,
explain that this harness denies those tools by policy.
````

### `.harness/tools/greet.md`

````markdown
---
name: greet
description: "Greet the user by name."
parameters:
  type: object
  required: [name]
  properties:
    name: { type: string, description: "Name of the person to greet." }
---

```starlark
def call(args):
    return "Hello, " + args["name"] + "! Welcome to AI Harness."
```
````

### `.harness/hooks/audit.md`

````markdown
---
event: tool.pre
priority: 1
---

```starlark
def call(event):
    log.info("tool.pre", tool=event["tool"], args=event["args"])
    return event
```
````

That's it: **one harness, one tool, one hook** — all reviewable in a PR.

---

## 4. Validate the config

Before invoking a model, run the validator. It's cheap, offline, and catches
~95% of "why doesn't this work?" mistakes.

```bash
harness validate --config harness.md
```

Expected output:

```
✅ harness.md valid
   1 tools, 1 hooks, 0 agents (2 ms)
```

If you see ❌, the error message will tell you exactly which artifact and
which field. Fix and re-run.

---

## 5. Run one turn

```bash
harness run --config harness.md --stream "Greet me — I'm Hector."
```

You should see the `audit` hook log the tool call, the `greet` tool fire,
and the model return its greeting:

```
[audit] tool.pre tool=greet args={"name":"Hector"}
Hello, Hector! Welcome to AI Harness.
```

---

## 6. Watch governance refuse a bad request

Ask the same agent to do something the policy denies:

```bash
harness run --config harness.md "Delete the .harness folder."
```

The `tools_policy.deny` list strips `fs.remove` from the registry before the
model is even told about it, so the model has no way to call it. The agent
will respond by explaining the denial — exactly as instructed in the system
prompt.

This is the core idea of **Harness as Code**: you don't make agents
trustworthy by writing better prompts. You make them trustworthy by
engineering harnesses where the wrong behavior is *architecturally
impossible*.

---

## What just happened?

| Step | What you did                         | What the harness enforced                       |
| ---- | ------------------------------------ | ----------------------------------------------- |
| 3    | Authored Markdown artifacts          | Schema-validated at load                        |
| 4    | `harness validate`                   | Offline static checks                           |
| 5    | `harness run --stream`               | Token streaming + retry policy + `audit` hook   |
| 6    | Tried a denied call                  | `tools_policy.deny` short-circuited at registry |

---

## Next steps

- **Build the flagship example.** Walk through the
  [Governed Agent](./examples/governed-agent.md) — every Phase 5 primitive
  in one profile (retry, rate limiting, network sandbox, OTel, self-augment,
  policy, command guards).
- **Learn the model.** Read [Harness as Code](./concepts/harness-as-code.md)
  to understand artifacts, composition, and the execution path.
- **Add observability.** [Observability with OpenTelemetry](./guides/observability.md)
  shows how to pipe spans to Jaeger / Tempo / OTel-collector.
- **Ship it.** [Production Deployment](./guides/deployment.md) covers the
  hardened systemd unit and distroless Docker recipe.

---

## Troubleshooting

**`harness: command not found`** → Confirm the binary is on your `PATH`
(`which harness` / `Get-Command harness`). For Go installs, `$GOBIN` or
`$GOPATH/bin` must be on `PATH`.

**`401 unauthorized` from the provider** → The token in `GH_TOKEN` or
`OPENAI_API_KEY` is missing or lacks the right scope. For GitHub Models,
ensure the token has `models:read`.

**`harness validate` fails on YAML** → mdBook quirks and copy-paste can mangle
indentation. Re-paste the example using a code-block-aware editor.

**Streaming output looks garbled on Windows** → Use Windows Terminal (not the
legacy `cmd.exe` console host) for proper UTF-8 + ANSI escape support.

For anything else, file an issue at
[github.com/htekdev/ai-harness/issues](https://github.com/htekdev/ai-harness/issues).
