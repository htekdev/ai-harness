# Docker deployment

Two ways to run AI Harness in a container.

## A. Pull the published image

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

## B. Build from source

```bash
# From repo root:
docker build \
  -f deploy/docker/Dockerfile \
  --build-arg VERSION=$(git describe --tags --always) \
  --build-arg COMMIT=$(git rev-parse --short HEAD) \
  --build-arg DATE=$(date -u +%Y-%m-%dT%H:%M:%SZ) \
  -t ai-harness:local .
```

The Dockerfile is a two-stage build: `golang:1.25-alpine` for compilation,
`gcr.io/distroless/static-debian12:nonroot` for runtime. Final image is
~10 MB, runs as uid 65532, has no shell, and ships only the static binary
plus CA roots.

## C. Compose

`deploy/docker/docker-compose.yml` is a reference compose file with
read-only root filesystem, dropped capabilities, a tmpfs for `/tmp`, and a
`harness validate` healthcheck. Copy it next to your `harness.md` and run:

```bash
docker compose -f deploy/docker/docker-compose.yml up -d
docker compose -f deploy/docker/docker-compose.yml logs -f harness
```

## One-shot mode

For CI/CD or scripting, override the entrypoint args to use `harness deploy`:

```bash
echo "summarize today's commits" | docker run --rm -i \
  --env-file ./harness.env \
  -v "$PWD/harness.md:/work/harness.md:ro" \
  ghcr.io/htekdev/ai-harness:latest \
  deploy --config /work/harness.md
```

## Notes

- The container expects `harness.md` at `/work/harness.md`. Override with
  `--config` if you mount it elsewhere.
- `data/` must be writable — that's where session state and the persistence
  DB live. Everything else is mounted read-only.
- Provider keys go in `harness.env` (chmod 0600, never commit).
- Phase 5.5 network sandbox and Phase 5.9 tool policy are configured inside
  `harness.md` / `.harness/`, not via container flags.
