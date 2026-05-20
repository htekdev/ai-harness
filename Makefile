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

# Build example binary
build:
	go build -o bin/harness ./cmd/example
