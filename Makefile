# AI Harness — Makefile

.PHONY: test evals evals-verbose lint build

# Run unit tests (fast, no LLM calls)
test:
	go test ./...

# Run eval suite (requires GH_TOKEN, hits real LLM API)
evals:
	go test -tags=eval -timeout=5m ./evals/

# Run eval suite with verbose output
evals-verbose:
	go test -tags=eval -v -timeout=5m ./evals/

# Run a single eval by name pattern
# Usage: make eval-one NAME=simple-completion
eval-one:
	go test -tags=eval -v -timeout=2m -run "TestEvals" ./evals/ -args -filter=$(NAME)

# Lint
lint:
	go vet ./...

# Build CLI binary
build:
	go build -ldflags "-X main.version=$(shell git describe --tags --always) -X main.commit=$(shell git rev-parse --short HEAD) -X main.date=$(shell date -u +%Y-%m-%dT%H:%M:%SZ)" -o bin/harness ./cmd/harness

# Build example binary
build-example:
	go build -o bin/harness-example ./cmd/example

# Install harness CLI to GOPATH/bin
install:
	go install -ldflags "-X main.version=$(shell git describe --tags --always) -X main.commit=$(shell git rev-parse --short HEAD) -X main.date=$(shell date -u +%Y-%m-%dT%H:%M:%SZ)" ./cmd/harness
