# Production Deployment

> A hands-on tutorial. By the end of this guide you'll have built a
> versioned `harness` binary, wired provider credentials and OTel
> through environment variables, picked the right autonomy posture for
> the workload, and supervised the process with either systemd or
> Docker.

This guide assumes you've finished the
[Quickstart](../getting-started.md) and at least one of
[Writing a Tool](./writing-a-tool.md),
[Writing a Hook](./writing-a-hook.md), or
[Writing a Context](./writing-a-context.md). Everything below is built
on top of the same `harness.md` + `.harness/` layout you already have.

The repo ships **reference recipes** under
[`deploy/`](https://github.com/htekdev/ai-harness/tree/main/deploy):
copy-pasteable systemd, Docker, and Compose configurations. This guide
walks you through using them end-to-end. When something is best
expressed as a file, we point at the recipe instead of duplicating it.

## What "deploying" actually means here

`harness` is a single static Go binary. There is no runtime, no
sidecar, no agent daemon shipped separately. A deployment is:

1. A **binary** (`/usr/local/bin/harness` or a container image).
2. A **`harness.md`** file at a known path.
3. An optional **`.harness/`** directory of tools, hooks, sub-agents.
4. **Environment variables** for provider credentials and telemetry.
5. A **process supervisor** that restarts on failure.

That's the whole footprint. Everything else — autonomy posture,
network sandbox, tool policy, claims verification — is configured
**inside the artifacts**, not at the supervisor or container layer.

```
host
├── /usr/local/bin/harness           ← binary (this guide)
├── /etc/harness/harness.env         ← secrets (this guide)
├── /etc/systemd/system/harness.service   ← supervisor (this guide)
└── /var/lib/harness/
    ├── harness.md                   ← your config (other guides)
    ├── .harness/                    ← your artifacts (other guides)
    └── data/                        ← writable state (this guide)
```

---

## 1. Get a binary

You have three options, in order of "boring and reproducible" first.

### A. Download a release (recommended)

Tagged releases publish pre-built binaries via GoReleaser for
`linux/{amd64,arm64}`, `darwin/{amd64,arm64}`, and `windows/amd64`. The
build is reproducible: `CGO_ENABLED=0`, `-trimpath`, stripped, with
version/commit/date stamped via `-ldflags`.

```bash
# Linux x86_64
curl -fsSL https://github.com/htekdev/ai-harness/releases/latest/download/harness_*_linux_amd64.tar.gz \
  | tar -xz harness
sudo install -m 0755 ./harness /usr/local/bin/harness

harness --version
```

The release archive ships `README.md`, `LICENSE`, and a top-level
`harness.md` reference alongside the binary. Checksums are published
as `checksums.txt` in the same release.

### B. `go install`

If you have Go 1.25+ on the box and trust your module cache:

```bash
go install github.com/htekdev/ai-harness/cmd/harness@latest
# or pin: ...@v0.6.0
```

This is the fastest option for a workstation. For production hosts,
prefer the release archive — it pins a known build, not whatever
`@latest` resolves to today.

### C. Build from source

For air-gapped environments or when you're carrying a local patch:

```bash
git clone https://github.com/htekdev/ai-harness && cd ai-harness
make build      # writes ./harness
```

The `Makefile` mirrors GoReleaser's flags so the binary matches the
release artefacts byte-for-byte (modulo `main.date`).

### Smoke test

Before going further, prove the binary works against your real config:

```bash
harness --version
harness validate --config /path/to/harness.md
```

`harness validate` parses every artifact, runs the schema checks, and
exits non-zero on any error. It's also what the Docker compose
healthcheck calls — a deploy that doesn't `validate` clean won't stay
up.

---

## 2. Wire credentials and telemetry through the environment

Every secret AI Harness reads comes from an environment variable.
Nothing is read from `harness.md`, and nothing should be baked into a
binary, image, or unit file.

### Provider credentials

Set whichever providers your harness actually uses:

| Variable | Used by |
|---|---|
| `OPENAI_API_KEY` | OpenAI completions |
| `ANTHROPIC_API_KEY` | Anthropic completions |
| `GITHUB_TOKEN` (or `GH_TOKEN`) | GitHub-backed sources/tools |
| `TELEGRAM_BOT_TOKEN` | Telegram source |

The exact env var your model uses is whatever the model artifact
declares — check your `harness.md` `model:` block or the
[`harness inspect`](../reference/cli.md) output.

### Logging

| Variable | Effect |
|---|---|
| `HARNESS_LOG_FORMAT` | `text` (default) or `json` for structured logs |
| `HARNESS_LOG_LEVEL`  | `debug`, `info`, `warn`, `error` |

Use `HARNESS_LOG_FORMAT=json` in production — it's what journald
parsers and log shippers expect.

### OpenTelemetry

AI Harness uses `HARNESS_`-prefixed environment variables for OTel so
nothing collides with whatever telemetry your tools or sub-processes
ship on the side. CLI flags (`--otel-endpoint`, `--otel-service`,
`--otel-protocol`, `--otel-sample-ratio`) override the env.

| Variable | Effect |
|---|---|
| `HARNESS_OTEL_ENDPOINT` | Collector URL (e.g. `http://otel-collector:4318`) |
| `HARNESS_OTEL_PROTOCOL` | `http` (default; only HTTP/protobuf is supported in v1) |
| `HARNESS_OTEL_SERVICE_NAME` | Defaults to `ai-harness` |
| `HARNESS_OTEL_SAMPLE_RATIO` | Float in `[0,1]` (e.g. `0.1` for 10%) |

If `HARNESS_OTEL_ENDPOINT` is unset, telemetry is collected in-process
but not exported — handy for development. The dedicated
[Observability](./observability.md) guide goes deeper.

### The `harness.env` file

Put all of the above in **one** file outside the repo and outside any
container image:

```sh
# /etc/harness/harness.env
OPENAI_API_KEY=sk-...
GITHUB_TOKEN=ghp_...
HARNESS_LOG_FORMAT=json
HARNESS_LOG_LEVEL=info
HARNESS_OTEL_ENDPOINT=http://otel-collector:4318
HARNESS_OTEL_SERVICE_NAME=ai-harness
HARNESS_OTEL_SAMPLE_RATIO=1.0
```

```bash
sudo install -m 0600 -o root -g harness /dev/stdin /etc/harness/harness.env <<'EOF'
...paste the env above...
EOF
```

Both the systemd unit (`EnvironmentFile=`) and the Compose file
(`env_file:`) load this exact format. The example template lives at
[`deploy/systemd/harness.env.example`](https://github.com/htekdev/ai-harness/tree/main/deploy/systemd/harness.env.example).

> **Never commit `harness.env`.** The `.example` file in the repo is
> empty on purpose. Add `harness.env` to your `.gitignore` and your
> Docker `.dockerignore` (the reference Dockerfile already does).

---

## 3. Pick an autonomy posture

AI Harness models autonomy as **harness levels** (L1–L4 in the
[README](https://github.com/htekdev/ai-harness#harness-levels-progressive-sophistication)).
Each level is a deployment posture — same binary, different artifact
mix.

| Level | What's deployed | When to ship it |
|---|---|---|
| **L1 — Prompt + Basic Tools** | `harness.md` + a handful of tools | Internal prototypes, single-author repos, dev workstations |
| **L2 — Structured Capabilities** | `.harness/` tools + sub-agents, no governance hooks | Team adoption, shared repos, opinionated workflows |
| **L3 — Governed Autonomy** | L2 + `tool.pre`/`tool.post` hooks, network sandbox, `tools_policy: allowlist`, delegation depth caps | First production rollout, anything that can touch a customer system |
| **L4 — Observable, Adaptive Operations** | L3 + OTel collector, structured eval suite, claims verification (`delegation.post_verify`), rate limits | Org-scale, multi-team, regulated, or anything that needs an audit story |

The level isn't a flag; it's a property of the bundle of artifacts you
ship. **Match your deployment recipe to your level:**

- L1 / L2 → `harness run` from a workstation, or one-shot
  `harness deploy` in CI.
- L3 → `harness serve` under systemd or Docker with hooks loaded.
- L4 → Same as L3 plus an OTel collector and a separate evals job.

Production checklist for L3+ (mirrors
[`deploy/README.md`](https://github.com/htekdev/ai-harness/tree/main/deploy/README.md)):

- [ ] `harness validate` clean against the deployed `harness.md`
- [ ] Provider keys mounted via `EnvironmentFile=` / `env_file:`, never baked into the image or unit
- [ ] [Network sandbox](./network-sandbox.md) configured if your tools call `http.*`
- [ ] `tools_policy: allowlist` set in production envs
- [ ] Rate limits set to match provider quotas
- [ ] OTel exporter pointed at a collector; `agent.turn` spans visible
- [ ] Persistence DB on a backed-up volume if you rely on session history
- [ ] Restart policy in place (`Restart=on-failure` / `restart: unless-stopped`)
- [ ] Logs shipped off-host (journald → Vector/Loki, json-file → Fluent Bit)

---

## 4. Supervise the process

### A. systemd (Linux VM / bare metal)

The repo ships a hardened reference unit at
[`deploy/systemd/harness.service`](https://github.com/htekdev/ai-harness/tree/main/deploy/systemd/harness.service).
It runs as a dedicated `harness` user with `NoNewPrivileges`,
`ProtectSystem=strict`, `MemoryDenyWriteExecute`, an empty capability
set, and a `@system-service` syscall filter — safe defaults for a
static Go binary.

End-to-end install (matches
[`deploy/systemd/README.md`](https://github.com/htekdev/ai-harness/tree/main/deploy/systemd/README.md)):

```bash
# 1. Install the binary (from §1).
sudo install -m 0755 ./harness /usr/local/bin/harness

# 2. Create the service user and state directories.
sudo useradd --system --home-dir /var/lib/harness --shell /usr/sbin/nologin harness
sudo install -d -m 0750 -o harness -g harness /var/lib/harness /var/log/harness
sudo install -d -m 0750 -o root    -g harness /etc/harness

# 3. Drop in your harness.md + .harness/ artifacts.
sudo cp -r ./harness.md ./.harness /var/lib/harness/
sudo chown -R harness:harness /var/lib/harness

# 4. Provide credentials (see §2).
sudo install -m 0600 -o root -g harness \
  deploy/systemd/harness.env.example /etc/harness/harness.env
sudoedit /etc/harness/harness.env   # paste real keys

# 5. Install and start the unit.
sudo cp deploy/systemd/harness.service /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable --now harness

# 6. Tail logs.
journalctl -u harness -f
```

The unit traps `SIGTERM`, drains in-flight turns, then exits — so a
rolling restart never tears a turn in half:

```bash
sudo systemctl reload-or-restart harness
```

If a tool needs broader filesystem access than the defaults allow,
**extend `ReadWritePaths=`** in a drop-in (`systemctl edit harness`)
rather than relaxing `ProtectSystem`. Keep the rest of the hardening.

### B. Docker / Compose (containers, dev parity, CI sidecars)

The reference image is a two-stage build:
`golang:1.25-alpine` for compilation,
`gcr.io/distroless/static-debian12:nonroot` for runtime. Final image
is ~10 MB, runs as uid 65532, has no shell, and ships only the static
binary plus CA roots.

Pull and run:

```bash
docker pull ghcr.io/htekdev/ai-harness:latest

docker run --rm -it \
  --read-only \
  --user 65532:65532 \
  --cap-drop=ALL \
  --security-opt no-new-privileges \
  --env-file ./harness.env \
  -v "$PWD/harness.md:/work/harness.md:ro" \
  -v "$PWD/.harness:/work/.harness:ro" \
  -v "$PWD/data:/work/data:rw" \
  --tmpfs /tmp:size=64m \
  ghcr.io/htekdev/ai-harness:latest \
  serve --config /work/harness.md
```

For a longer-lived deployment, the reference compose file at
[`deploy/docker/docker-compose.yml`](https://github.com/htekdev/ai-harness/tree/main/deploy/docker/docker-compose.yml)
already includes:

- `read_only: true` root filesystem
- `cap_drop: ALL` and `no-new-privileges`
- A 64 MiB tmpfs at `/tmp` for tool work
- A `harness validate` healthcheck (cheap, ~10 ms)
- Log rotation (`json-file`, 10 MiB × 5 files)
- A commented-out OTel collector you can uncomment in development

```bash
docker compose -f deploy/docker/docker-compose.yml up -d
docker compose -f deploy/docker/docker-compose.yml logs -f harness
```

The compose file expects this layout next to it:

```
.
├── harness.md     # mounted ro at /work/harness.md
├── .harness/      # mounted ro at /work/.harness
├── data/          # mounted rw at /work/data (sessions, persistence DB)
└── harness.env    # chmod 0600, NEVER commit
```

> **Why so locked down?** Distroless + read-only root + dropped
> capabilities + tmpfs is the cheapest way to honour L3 expectations.
> A compromised tool can't escalate, can't write outside `/work/data`,
> and can't fork a shell because there isn't one in the image.

---

## 5. One-shot mode (CI, scheduled jobs, scripts)

Not every harness is long-lived. For GitHub Actions runs, cron jobs,
or shell pipelines, use `harness deploy` instead of `harness serve`.
It runs the agent against a single input and exits with a
deterministic status code.

```bash
echo "summarize today's commits" | harness deploy --config harness.md
```

In a container:

```bash
echo "summarize today's commits" | docker run --rm -i \
  --env-file ./harness.env \
  -v "$PWD/harness.md:/work/harness.md:ro" \
  ghcr.io/htekdev/ai-harness:latest \
  deploy --config /work/harness.md
```

In GitHub Actions:

```yaml
- name: Run harness
  run: echo "${{ github.event.inputs.task }}" | harness deploy --config harness.md
  env:
    OPENAI_API_KEY: ${{ secrets.OPENAI_API_KEY }}
    GITHUB_TOKEN:   ${{ secrets.GITHUB_TOKEN }}
    HARNESS_LOG_FORMAT: json
```

Same artifacts, same environment contract, no supervisor needed.

---

## 6. Pre-flight: what to run before you ship

Before flipping production traffic at a new build:

```bash
# 1. Schema and artifact validation.
harness validate --config harness.md

# 2. Inspect the resolved artifact graph (what will actually load).
harness inspect --config harness.md

# 3. Show the rendered system prompt + active context.
harness context --config harness.md

# 4. Smoke a turn end-to-end against a non-prod input.
echo "ping" | harness deploy --config harness.md
```

If any of these fail, the deployment will fail in the same way. Fail
loudly here, not in `journalctl -u harness` at 02:00.

---

## What's next

- [Observability](./observability.md) — wiring the OTel collector,
  reading `agent.turn` spans, and what to alert on.
- [Network Sandboxing](./network-sandbox.md) — locking down the
  outbound surface that tools can reach.
- The reference [`deploy/`](https://github.com/htekdev/ai-harness/tree/main/deploy)
  directory — the source of truth for systemd and Docker
  configuration. Treat this guide as the tutorial; treat `deploy/` as
  the manual.
