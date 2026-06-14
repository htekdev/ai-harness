# syntax=docker/dockerfile:1.7

# ---- build stage ----------------------------------------------------------
FROM golang:1.25-alpine AS build

WORKDIR /src

# Cache module downloads independently of source changes.
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download

COPY . .

ARG VERSION=dev
ARG COMMIT=unknown
ARG DATE=unknown

# CGO disabled → fully static binary; suitable for `FROM scratch`.
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=linux \
    go build \
      -trimpath \
      -ldflags "-s -w -X main.version=${VERSION} -X main.commit=${COMMIT} -X main.date=${DATE}" \
      -o /out/harness \
      ./cmd/harness

# ---- runtime stage --------------------------------------------------------
# Distroless static gives us a minimal image (~2 MB base) with TLS roots,
# /etc/passwd, and timezone data already wired up — no shell, no package
# manager, smaller attack surface than alpine.
FROM gcr.io/distroless/static-debian12:nonroot AS runtime

LABEL org.opencontainers.image.title="ai-harness" \
      org.opencontainers.image.description="Harness as Code — declarative AI agent runtime" \
      org.opencontainers.image.source="https://github.com/htekdev/ai-harness" \
      org.opencontainers.image.licenses="MIT"

# `nonroot` is uid/gid 65532 in the distroless base.
USER nonroot:nonroot
WORKDIR /work

COPY --from=build /out/harness /usr/local/bin/harness

# Default to long-running serve; override for one-shot deploy/run usage:
#   docker run --rm ai-harness deploy --input "..."
ENTRYPOINT ["/usr/local/bin/harness"]
CMD ["serve", "--config", "/work/harness.md"]
