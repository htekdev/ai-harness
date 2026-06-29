# Deployment recipes

Reference deployments for running AI Harness in production. Pick the one
that matches your platform — they're mutually exclusive but configured
the same way.

| Recipe | When to use | See |
|---|---|---|
| **systemd** | Long-lived VM / bare metal Linux host | [`systemd/`](systemd/) |
| **Docker (run / compose)** | Container platforms, dev parity, CI sidecars | [`docker/`](docker/) |
| **One-shot CLI** (`harness deploy`) | Scheduled jobs, GitHub Actions, scripts | top-level [`README.md`](../README.md) |

All three share the same configuration model:

1. A **`harness.md`** file (YAML frontmatter + body system prompt).
2. An optional **`.harness/`** directory of tools, hooks, sub-agents.
3. Provider credentials in environment variables (`OPENAI_API_KEY`,
   `ANTHROPIC_API_KEY`, `GITHUB_TOKEN`, …).
4. Optional OpenTelemetry vars (`HARNESS_OTEL_ENDPOINT`,
   `HARNESS_OTEL_SERVICE_NAME`, `HARNESS_OTEL_SAMPLE_RATIO`).

For v0.6.0 we intentionally use **Option A**: deployment recipes use
`HARNESS_OTEL_*` env vars only. This prefix is by design to avoid
accidental capture from ambient OTel SDK environment variables.

## Sizing

`harness serve` is a single Go process. Memory is dominated by in-flight
HTTP request buffers and (if persistence is enabled) the SQLite page cache.

| Workload | Suggested |
|---|---|
| 1 concurrent session, occasional turns | 128 MiB RAM, 1 vCPU |
| 10 concurrent sessions, steady traffic | 512 MiB RAM, 2 vCPU |
| 100+ concurrent sessions | 2 GiB RAM, 4 vCPU, tune `LimitNOFILE` |

CPU cost is dominated by JSON marshalling and any in-process tool work —
LLM inference happens upstream and only consumes a goroutine while waiting
on the HTTP response.

## Production checklist

- [ ] `harness validate` clean against the deployed `harness.md`.
- [ ] Provider keys mounted via `EnvironmentFile` / `env_file`, never baked
      into the image or unit.
- [ ] Phase 5.5 network sandbox configured if your tools call `http.*`.
- [ ] Phase 5.9 `tools_policy` set to allowlist mode in production envs.
- [ ] Phase 5.6 rate limits set to match your provider quotas.
- [ ] OTel exporter pointed at your collector; `agent.turn` spans visible.
- [ ] Persistence DB on a backed-up volume if you rely on session history.
- [ ] Restart policy in place (`Restart=on-failure` / `restart: unless-stopped`).
- [ ] Logs shipped off-host (journald → vector / Loki, or json-file → Fluent Bit).

## Where to go next

- Per-recipe READMEs in [`systemd/`](systemd/) and [`docker/`](docker/)
- [Top-level README](../README.md) for the Harness as Code model
- [`harness.md`](../harness.md) for the worked example used in this repo
