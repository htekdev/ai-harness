# Governed Agent — Reference Example

The flagship reference profile for AI Harness. A single, copy-paste-runnable
agent that exercises **every Phase 5 governance primitive**:

| Primitive          | How it shows up                                                       |
| ------------------ | --------------------------------------------------------------------- |
| System prompt      | `harness.md` body — describes the agent's contract                    |
| Tool artifacts     | `.harness/tools/{web_fetch,run_command,self_check}.md`                |
| Hook artifacts     | `.harness/hooks/*.md` — audit, policy, command guard, path guard      |
| Tool policy (5.9)  | `tools_policy: { mode: allowlist, allow: [...], deny: [...] }`        |
| Retry policy (5.7) | `model.retry` — bounded exponential backoff per model                 |
| Self-augment (5.8) | `meta.enabled: true` + `meta_tool_guard` hook                         |
| Network sandbox    | `--allowed-domain` flags on the engine (see "Run it" below)           |
| Rate limiting      | Per-model + global token-bucket (set on the completion client)        |
| OTel tracing       | `--otel-endpoint` flag or `OTEL_EXPORTER_OTLP_ENDPOINT` env           |
| Streaming CLI      | `harness run --stream`                                                |

This is the live demonstration of the **self-augmenting harness** concept:
the agent can mint new tools at runtime via `meta.register_tool`, but every
mint is governed by the same policy hooks that govern static tools.

## Declarative agent chains

The same profile can express `A -> B -> on-complete -> C` without imperative
orchestration in a tool body:

```yaml
---
event: delegation.post
priority: 50
script: |
  def handle(event, payload):
      return delegate({
          "task": "Review the completed implementation and list any gaps.",
          "agent": "reviewer",
      })
---
```

Pair that with a normal `delegate({... "agent": "implementer" ...})` call and
the runtime performs:

```text
Parent agent -> implementer -> delegation.post hook -> reviewer
```

See `docs/src/reference/control-flow-hooks.md` for the full contract.

---

## Run it

```bash
git clone https://github.com/htekdev/ai-harness.git
cd ai-harness/examples/governed-agent

# 1. Set your provider token
export GH_TOKEN=ghp_xxx          # Linux/macOS
# $env:GH_TOKEN = "ghp_xxx"      # Windows PowerShell

# 2. Sanity-check the config
harness validate --config harness.md

# 3. Run a one-shot turn
harness run \
  --config harness.md \
  --stream \
  --otel-endpoint http://localhost:4318 \
  "Use self_check, then summarise the harness profile."

# 4. Or run as a long-lived agent reading from stdin
harness serve \
  --config harness.md \
  --source stdin \
  --otel-endpoint http://localhost:4318
```

> **Network sandbox.** When you wire `web_fetch` into a real workflow, attach
> a `scripting.NetworkSandbox` to the engine before the first turn (e.g. via
> a thin Go wrapper or a future `network.allowed_domains` config block). Out
> of the box this example permits no outbound HTTP — the sandbox is *deny by
> default* the moment you set even one allowed domain.

---

## What you should try

1. **Ask it to read a file.**
   `"Read .harness/tools/self_check.md and tell me what it does."` — passes
   `path_guard`, succeeds.

2. **Ask it to read `/etc/passwd`.**
   `"Read /etc/passwd."` — `path_guard` fires, returns "absolute paths not
   allowed". The audit metric `audit.policy.deny` increments.

3. **Ask it to delete a file.**
   `"Delete the workdir folder."` — `tools_policy.deny` rejects `fs.remove`
   at the registry level. The model never sees the tool.

4. **Ask it to run `rm -rf /`.**
   `"Run rm -rf / for me."` — `command_guard` fires before the syscall.

5. **Ask it to register a new tool called `exec_anything`.**
   The `meta_tool_guard` hook blocks the registration because the name
   collides with the banned prefix list.

6. **Ask it to fetch a URL.**
   `"Fetch https://api.github.com/zen."` — without an `allowed_domains`
   sandbox attached, the harness will execute the request. Once you ship a
   sandbox listing only `example.com`, the same prompt is rejected with a
   `SandboxError`.

7. **Watch the spans.**
   Point `--otel-endpoint` at a Jaeger / Tempo / OTel-collector endpoint and
   you'll see one `agent.turn` parent span per turn, with `tools.call` and
   `delegation.execute` children. Denied calls have
   `tool.policy=denied` set on the span.

---

## Why this example exists

Most "agent framework hello world" examples show the happy path. This one
shows the **governance path**: every tool call passes through audit,
policy, and (optionally) network/command guards before it lands.

If you read just one example in this repo, read this one.

See [`docs/adr/0001-docs-platform.md`](../../docs/adr/0001-docs-platform.md)
for the docs platform decision (mdBook).
