# Contributing

This page documents how to contribute to **AI Harness** — local dev setup, branching, the test/lint bar, harness-level validation, PR conventions, doc contributions, and CI expectations.

If you only want to file an issue or propose an idea, jump straight to [Issues & Discussions](#issues--discussions).

---

## Project shape

| What | Where |
|------|-------|
| Go source | `agent/`, `config/`, `harness/`, `hooks/`, `scripting/`, `tools/`, `cmd/` |
| Built-in artifacts | `.harness/builtins/`, `.harness/plugins/`, `.harness/overrides/` |
| Examples | `examples/` (notably `examples/governed-agent/`) |
| Docs (mdBook) | `docs/src/` — published to <https://htekdev.github.io/ai-harness/> |
| ADRs | `docs/adr/` |
| Specs / roadmap | [`project/roadmap.md`](./roadmap.md) plus referenced specs |
| CI | `.github/workflows/{ci,pages,release}.yml` |

The runtime is intentionally small: a single Go binary, Go 1.25, ~5 direct dependencies. Anything that can be expressed as a Markdown artifact under `.harness/` should be — keep the core minimal.

---

## Local development

### Prerequisites

- **Go 1.25 or newer** (`go version`)
- **Git**
- **mdBook** (only if you're editing docs) — `cargo install mdbook` or download from the [mdBook releases](https://github.com/rust-lang/mdBook/releases)

### Build & run

```bash
git clone https://github.com/htekdev/ai-harness
cd ai-harness
go build ./...
go run ./cmd/harness --help
```

### Run all tests

```bash
go test ./...
```

This is the canonical pre-push check. Run it before every push.

### Run with race detector and timeout (matches CI)

```bash
go test -race -timeout 5m ./...
```

CI runs the race-instrumented suite on `ubuntu-latest`, `macos-latest`, and `windows-latest` against Go 1.25. Local race runs catch the same regressions earlier.

### Lint locally (matches CI)

```bash
go vet ./...
gofmt -l .          # must produce no output
```

If `gofmt -l .` lists any file, run `gofmt -w <file>` before committing.

### Validate the test harness

```bash
go run ./cmd/harness validate -v
```

This reports the registered tools, hooks, models, and any artifact-loading errors. See [`reference/cli.md`](../reference/cli.md) for the full command surface.

---

## Branching & worktrees

The repo uses a single long-lived branch: **`main`**. All work happens on short-lived feature branches off `main`.

Recommended pattern (mirrors how the maintainers work):

1. Create an isolated worktree for the change so you don't disturb your main checkout.
2. Branch naming: `<type>/<short-slug>` where `<type>` is one of:
   - `fix/` — bug fix
   - `feat/` — new feature
   - `docs/` — docs-only change
   - `refactor/` — internal restructuring with no behavior change
   - `chore/` — tooling, deps, CI
   - `test/` — test-only changes

Examples seen in recent PRs:
- `fix/agent-finish-reason-strict-guard`
- `docs/reference-starlark-builtins`
- `feat/governed-agent-example`

3. Push the branch and open a PR against `main`.

---

## The test bar

A change is mergeable when **all** of the following hold:

1. `go test ./...` passes locally.
2. `go test -race -timeout 5m ./...` passes locally on at least one OS.
3. `go vet ./...` is clean.
4. `gofmt -l .` produces no output.
5. The 6 CI checks are green: **Lint**, **Test (Go 1.25, ubuntu-latest)**, **Test (Go 1.25, macos-latest)**, **Test (Go 1.25, windows-latest)**, **Build**, and **Build mdBook**.
6. For runtime changes that affect artifact loading, hook dispatch, tool registration, or context assembly: a fresh `go run ./cmd/harness validate -v` against `examples/governed-agent/` (or your repro fixture) is included in the PR description.
7. New or changed behavior is covered by a Go test. Bug fixes start with a failing test.

There is no "skip CI" or "merge red" path. If a check is flaky, fix it on its own PR before merging the change that surfaced it.

---

## Authoring artifacts

If your change introduces or modifies a tool, hook, or context source:

- **Tools** must follow the schema in [`reference/tool-artifact.md`](../reference/tool-artifact.md). Entry point is `def run(args)`; return shapes and the Starlark dialect are both documented there.
- **Hooks** must follow [`reference/hook-artifact.md`](../reference/hook-artifact.md). Entry point is `def handle(event, payload)`. Decisions return `block(reason)` / `allow()` / `modify(payload)` (or the equivalent dict form). Use `payload["name"]` in `when:` predicates — not bare `tool_name`.
- **Built-ins** available inside Starlark are catalogued in [`reference/starlark-builtins.md`](../reference/starlark-builtins.md). Notably: `type(v) == "string"` — there is no `isinstance`.
- **harness.md** frontmatter fields are catalogued in [`reference/harness-md.md`](../reference/harness-md.md).

When you add or change a built-in, a hook event, or a CLI flag, update the corresponding reference page in the same PR.

---

## Documentation contributions

The site is an mdBook rooted at `docs/src/` and published by `.github/workflows/pages.yml`.

### Local preview

```bash
cd docs
mdbook serve
```

Open <http://localhost:3000>. Edits hot-reload.

### Structure

- `docs/src/SUMMARY.md` is the table of contents. **Every new page must be linked from it** — orphan pages are not allowed.
- `concepts/` — what the system is and why
- `guides/` — how to do specific tasks (writing a tool, writing a hook, deployment, observability, etc.)
- `reference/` — exhaustive schemas (CLI, harness.md, tool/hook artifacts, Starlark built-ins)
- `examples/` — narrated end-to-end walkthroughs of `examples/`
- `project/` — meta documentation (this page, roadmap, ADR index)

### Style

- Cross-link liberally. Every reference page links to the relevant concepts, guides, and other reference pages.
- Code blocks should be runnable as-is when feasible.
- When documenting Starlark, prefer `type(v) == "string"` patterns and the canonical decision builtins.
- Keep a single source of truth: if a fact lives in `reference/`, link to it from concepts/guides rather than duplicating.

---

## PR conventions

### Title

Conventional Commits, scoped where useful. Examples:

- `fix(agent): detect finish_reason=length truncation`
- `docs(reference): complete Starlark built-ins reference`
- `feat(hooks): add agent.stop event`
- `chore(deps): bump actions/cache to v5`

### Description

Include, at minimum:

- **What** changed (one paragraph).
- **Why** — link the issue, ADR, or roadmap item.
- **Test evidence** — local `go test ./...` output summary, plus any harness `validate -v` snippet when relevant.
- **Backward compatibility** — call out any artifact-schema, CLI, or hook-payload changes explicitly. These are user-visible contracts.

### Size

Smaller is better. If a change touches more than ~500 lines of non-doc code, consider splitting it into a stack.

### Review

A maintainer will review every PR. The bar is correctness, contract clarity, and minimal-core discipline (see [`project/roadmap.md`](./roadmap.md) and the README's *Naming and terminology* section).

### Merge

Merges are squash-merges. The squashed commit message becomes the canonical history entry — keep PR titles clean.

---

## Releases & versioning

- The project follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and [Semantic Versioning](https://semver.org/).
- Every user-visible change must add an `Unreleased` entry to `CHANGELOG.md` in the same PR.
- Tags (`vX.Y.Z`) are cut from `main` after the changelog is finalized; `.github/workflows/release.yml` builds artifacts.
- Pre-1.0 the artifact schemas (`harness.md`, tool/hook artifacts, Starlark built-ins) are still evolving. Breaking changes are allowed on minor bumps but must be flagged in `CHANGELOG.md` and explained in a follow-up note in `docs/src/project/`.

---

## Issues & discussions

- **Bugs:** open an issue with a minimal `harness.md` + steps to reproduce. Include `harness validate -v` output and the failing command.
- **Feature ideas:** open an issue tagged `proposal` describing the use case before sending a PR. For larger changes, propose an ADR under `docs/adr/`.
- **Security:** do not file security issues in public. Email the maintainers per the SECURITY policy in the repo root.

---

## Code of conduct

Be respectful, be precise, and assume good faith. Disagreements are settled by reference to the spec, the roadmap, or a new ADR — not by volume.

---

## See also

- [`project/roadmap.md`](./roadmap.md) — phase plan and current priorities
- [`project/adr-index.md`](./adr-index.md) — architecture decisions of record
- [`reference/cli.md`](../reference/cli.md) — every CLI subcommand and flag
- [`reference/harness-md.md`](../reference/harness-md.md) — `harness.md` frontmatter
- [`reference/tool-artifact.md`](../reference/tool-artifact.md) — tool schema
- [`reference/hook-artifact.md`](../reference/hook-artifact.md) — hook schema
- [`reference/starlark-builtins.md`](../reference/starlark-builtins.md) — Starlark surface
