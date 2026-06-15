# Contributing to AI Harness

Thank you for your interest in contributing! AI Harness is a minimal,
governance-first runtime for coding agents. We welcome bug reports, feature
ideas, documentation improvements, and code contributions.

---

## Getting started

### Prerequisites

- Go 1.25+
- `git`

### Clone and build

```bash
git clone https://github.com/htekdev/ai-harness.git
cd ai-harness
go build ./...
go test ./...
```

### Running the linter / vet

```bash
go vet ./...
```

---

## How to contribute

### Reporting a bug

1. Search [existing issues](https://github.com/htekdev/ai-harness/issues) first.
2. If it's new, open an issue with:
   - A clear title and description
   - Reproduction steps (config files, commands run, output)
   - Expected vs. actual behavior
   - Go version and OS

### Requesting a feature

Open an issue using the **Feature** template and explain:
- The problem it solves
- Your proposed interface (CLI flags, config keys, events, etc.)
- Any alternatives you considered

### Submitting a pull request

1. Fork the repo and create a feature branch from `main`.
2. Keep changes focused — one logical change per PR.
3. Add or update tests for any changed behavior.
4. Run the full check suite before pushing:

   ```bash
   go build ./...
   go test ./... -cover
   go vet ./...
   ```

5. Write a clear PR description: what changed, why, and any relevant issue
   numbers (`Closes #N`).
6. PRs targeting governance, hook, or delegation semantics should include an
   eval case in `evals.yaml` when practical.

---

## Code style

- Standard `gofmt` formatting (enforced in CI).
- `tools.Handler` signature is `func(ctx context.Context, args map[string]any) (string, error)` — keep it that way.
- Error types live in `harness/errs`; use `errs.Kind*` codes for new public errors.
- Structured log calls use `log/slog` with lowercase snake_case attribute keys.
- OTel span names are `package.Operation` (e.g., `agent.Run`, `tools.Execute`).
- No new global state; inject dependencies via options structs.

---

## Commit messages

Use the [Conventional Commits](https://www.conventionalcommits.org/) format:

```
feat(hooks): add delegate.pre blocking semantics
fix(completion): handle empty SSE delta chunks
docs(readme): update status table to v0.6.0
```

Types: `feat`, `fix`, `docs`, `test`, `refactor`, `chore`, `ci`.

---

## Public API stability

Before v1.0.0, the stable surfaces are:

- CLI flags and subcommands
- Hook event catalog
- Starlark builtins
- The `harness.md` YAML config schema
- Process exit codes

Everything else (internal Go packages outside `pkg/`, OTel attribute names,
example tool implementations) may evolve between minor releases. See
[`docs/src/project/semver.md`](docs/src/project/semver.md) for the full policy.

---

## Community

- **GitHub Discussions** — questions, ideas, and show-and-tell live at
  [Discussions](https://github.com/htekdev/ai-harness/discussions).
- **Issues** — bug reports and roadmap tracking.

Please follow our [Code of Conduct](CODE_OF_CONDUCT.md) in all interactions.
