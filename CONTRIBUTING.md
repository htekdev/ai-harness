# Contributing to AI Harness

Thanks for your interest in AI Harness! This project is built around a simple
idea: **keep the core tiny, make the edges powerful**. Contributions that align
with that philosophy — typed artifacts, composable governance, strong
observability — are very welcome.

This guide covers what you need to start contributing code, docs, or examples.

---

## Table of Contents

- [Ways to Contribute](#ways-to-contribute)
- [Getting Started](#getting-started)
- [Development Workflow](#development-workflow)
- [Pull Request Checklist](#pull-request-checklist)
- [Testing](#testing)
- [Code Style](#code-style)
- [Documentation](#documentation)
- [Commit Messages](#commit-messages)
- [Code of Conduct](#code-of-conduct)

---

## Ways to Contribute

You can help in several ways:

- **Bug reports** — file an issue with a minimal reproduction
- **Feature requests** — open a discussion before large changes
- **Pull requests** — bug fixes, new artifacts, docs, examples
- **Docs improvements** — guides, tutorials, clarifications
- **Examples** — new sample artifacts under `examples/`
- **Community support** — answering questions in Discussions

If you are unsure whether something fits, open a Discussion first. We would
rather help shape an idea early than reject a finished PR.

---

## Getting Started

### Prerequisites

- **Go 1.25+** (matches CI matrix on Linux, macOS, Windows)
- **Git**
- **mdBook** (only if you are editing `docs/`)

### Clone and Build

```bash
git clone https://github.com/htekdev/ai-harness.git
cd ai-harness
go build ./...
```

### Verify Your Setup

```bash
go test ./...
```

All 19 packages should pass on a clean checkout. If they don't, please open an
issue with your `go version` and platform.

### Optional: Install the CLI Locally

```bash
go install ./cmd/harness
harness --help
```

---

## Development Workflow

1. **Find or open an issue** describing what you intend to change.
2. **Fork** the repo and create a topic branch from `main`:
   ```bash
   git checkout -b feat/short-description
   ```
3. **Make focused changes.** One logical change per PR. Smaller is better.
4. **Run the test suite** locally (`go test ./...`).
5. **Update docs** alongside code — `docs/src/` and example artifacts.
6. **Open a PR** against `main` with a clear description and linked issue.

### Branch Naming

| Prefix     | Use for                              |
|------------|--------------------------------------|
| `feat/`    | New features or capabilities         |
| `fix/`     | Bug fixes                            |
| `docs/`    | Documentation-only changes           |
| `refactor/`| Internal refactors with no behavior change |
| `test/`    | Adding or improving tests            |
| `chore/`   | Build, deps, tooling                 |

### Drafts

Open a **draft PR** early if you want feedback on direction. CI will run on
drafts so you can iterate publicly.

---

## Pull Request Checklist

Before marking a PR ready for review, please confirm:

- [ ] The PR is focused on a single logical change.
- [ ] `go test ./...` passes locally on your platform.
- [ ] `go vet ./...` is clean.
- [ ] New code paths have unit tests where reasonable.
- [ ] Public behavior changes are documented in `docs/src/`.
- [ ] User-visible changes are mentioned in `CHANGELOG.md` under `Unreleased`.
- [ ] The PR description links the issue it closes (`Closes #123`).
- [ ] Commit messages follow [Conventional Commits](#commit-messages).

CI must be green before a maintainer will merge.

---

## Testing

AI Harness tests live alongside the code:

```bash
# Full suite
go test ./...

# Specific package
go test ./internal/runtime/...

# With race detector (recommended for runtime/concurrent code)
go test -race ./...

# Verbose
go test -v ./...
```

### What to Test

- **New tools** — exercise both happy path and policy-denied path.
- **New hooks** — assert the decision (`allow` / `block` / `modify`) for each
  intended trigger condition.
- **New artifacts** — round-trip parse → validate → execute.
- **Bug fixes** — add a regression test that fails before the fix.

### Eval Suite

If you change agent-facing behavior, consider adding a case under
`evals/testdata/`. See `docs/src/guides/testing.md` for the eval schema and
runner usage.

---

## Code Style

We rely on standard Go tooling — please run these before opening a PR:

```bash
gofmt -s -w .
go vet ./...
```

Additional conventions:

- Prefer **small, well-named packages** over large catch-alls.
- Public APIs need doc comments (`// FunctionName does ...`).
- Errors are values: wrap with `fmt.Errorf("context: %w", err)`.
- No global state in core packages; pass dependencies explicitly.
- Avoid `panic` outside `init` and clearly programmer-error paths.
- Keep imports grouped: stdlib, third-party, internal — gofmt handles this.

### Artifact Conventions

When adding or modifying artifact types:

- Schema changes belong in `internal/artifact/`.
- New artifact kinds need a parser test and at least one example file.
- Keep artifact frontmatter explicit and typed — avoid stringly-typed enums.

---

## Documentation

Docs live under `docs/src/` and are built with mdBook.

```bash
# Install mdBook (one-time)
cargo install mdbook

# Build
cd docs && mdbook build

# Serve locally with live reload
mdbook serve
```

### When You Need a Docs Update

- New CLI flag → update `docs/src/cli/`.
- New artifact kind or field → update the relevant guide under
  `docs/src/guides/` and add a snippet to `docs/src/concepts/`.
- New example → add it under `examples/` and link from the matching guide.

If you add a new page, also add it to `docs/src/SUMMARY.md`.

---

## Commit Messages

We use **[Conventional Commits](https://www.conventionalcommits.org/)** so the
changelog and release notes can be generated mechanically.

Format:

```
<type>(<scope>): <short summary>

<body — optional, wrap at 72 chars>
```

Common types:

| Type       | Use for                                  |
|------------|------------------------------------------|
| `feat`     | New user-facing capability               |
| `fix`      | Bug fix                                  |
| `docs`     | Documentation only                       |
| `refactor` | Internal change, no behavior change      |
| `test`     | Adding or improving tests                |
| `chore`    | Build, deps, tooling                     |
| `perf`     | Performance improvement                  |

Examples:

```
feat(hooks): add tool.pre priority ordering
fix(cli): handle missing harness.md gracefully
docs(guides): add testing-with-evals guide
```

Keep the summary line under 72 characters and in the imperative mood
(`add`, not `added`).

---

## Code of Conduct

Participation in this project is governed by our
[Code of Conduct](./CODE_OF_CONDUCT.md). By contributing, you agree to uphold
its terms.

---

## Questions?

- **Bugs / feature requests:** open a GitHub Issue.
- **Design discussion:** start a thread in GitHub Discussions.
- **Casual questions:** Discussions → Q&A.

Thank you for helping make AI Harness better. 🚀
