# Changelog

All notable changes to **AI Harness** are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

> **Public API surface (stable):** CLI flags & subcommands, hook event catalog,
> Starlark builtins, the harness config YAML schema, and process exit codes.
> Everything else (internal Go packages outside `pkg/`, OTel attribute names,
> example tool implementations) may evolve between minor releases until v1.0.0.

---

## [Unreleased]

Pending Phase 6 work is tracked in
[#95 — Phase 6: Community & Launch (kickoff)](https://github.com/htekdev/ai-harness/issues/95),
with launch-sequence specifics tracked in
[#98 — Phase 6.2 Launch Sequence](https://github.com/htekdev/ai-harness/issues/98).

---

## [0.6.0] — 2026-06-14

> **Theme:** *Production hardening, declarative event sources, and the public docs site.*
>
> v0.6.0 closes Phases 4 (Event Sources), 5 (Production Hardening), and the
> documentation half of Phase 6 (mdBook scaffold, Quickstart, and the five
> concept pages). The harness is now usable as a long-running, observable,
> sandboxed agent runtime — not just a CLI demo. The first public docs site
> goes live alongside this tag at
> <https://htekdev.github.io/ai-harness/>.

### Added

#### Phase 4 — Event Sources

- **Telegram input source** as the first concrete `Source` primitive — bidirectional,
  durable offsets, `harness serve` runtime, replier contract.
  ([#74](https://github.com/htekdev/ai-harness/pull/74))
- **MeshWire input source** — `Source` + `Replier` over the MeshWire long-poll API,
  parity with the Telegram source.
  ([#75](https://github.com/htekdev/ai-harness/pull/75))
- **Declarative `serve:` config block** — replaces repeated CLI flags; one harness
  can declare multiple sources with per-source policy.
  ([#76](https://github.com/htekdev/ai-harness/pull/76))
- **Phase 4 eval coverage** — Telegram, MeshWire, serve, durable-offsets primitives
  exercised by the eval suite.
  ([#79](https://github.com/htekdev/ai-harness/pull/79))

#### Phase 5 — Production Hardening

- **5.1 — Structured logging via `log/slog`** — first-class structured logs across
  agent, completion, tools, hooks, and delegation. Closes the first item of
  [#77](https://github.com/htekdev/ai-harness/issues/77).
  ([#78](https://github.com/htekdev/ai-harness/pull/78))
- **5.2 — OpenTelemetry tracing** — tracing primitives + `slog`↔trace bridge,
  span instrumentation on `agent.Run`, `delegation.Execute`, `tools.Call`,
  `source.pump`, and a Jaeger Compose example.
  ([#80](https://github.com/htekdev/ai-harness/pull/80),
  [#81](https://github.com/htekdev/ai-harness/pull/81),
  [#82](https://github.com/htekdev/ai-harness/pull/82))
- **5.3 — Typed error taxonomy** (`harness/errs`) — stable `errs.Kind*` codes,
  hot-path conversions in `agent`/`completion`/`tools`/`hooks`, then in
  `input`/`persistence`/`config`, then in `serve`/`evals`, plus a retry-policy
  example consuming the taxonomy.
  ([#83](https://github.com/htekdev/ai-harness/pull/83),
  [#84](https://github.com/htekdev/ai-harness/pull/84),
  [#85](https://github.com/htekdev/ai-harness/pull/85))
- **5.4 — Streaming CLI mode** — live token output via SSE in interactive mode.
  ([#91](https://github.com/htekdev/ai-harness/pull/91))
- **5.5 — Network sandbox for Starlark `http.*` builtins** — explicit allowlist
  per artifact, deny-by-default outbound network from scripted hooks/tools.
  ([#87](https://github.com/htekdev/ai-harness/pull/87))
- **5.6 — Rate limiting** — per-model and global token-bucket limits with
  config-driven policy.
  ([#92](https://github.com/htekdev/ai-harness/pull/92))
- **5.7 — Configurable retry/backoff policy per model** — declarative retry
  envelope per registered model.
  ([#86](https://github.com/htekdev/ai-harness/pull/86))
- **5.8 — Self-augmenting harness** — runtime `meta.register_tool` /
  `meta.list_tools` / `meta.remove_tool` builtins + `Reload` hook for safe
  in-process re-composition.
  ([#88](https://github.com/htekdev/ai-harness/pull/88))
- **5.9 — Config-driven tool allow/denylists** — `tools_policy:` block for
  per-harness allow/deny enforcement at composition time.
  ([#93](https://github.com/htekdev/ai-harness/pull/93))
- **5.10 — Production deployment recipes** — `deploy/systemd/` and
  `deploy/docker/` blueprints with health checks, restart policy, and
  log shipping.
  ([#94](https://github.com/htekdev/ai-harness/pull/94))

#### Phase 6 — Documentation, Examples, Launch

- **`examples/governed-agent/`** — end-to-end reference example showing the
  full hook stack (audit, deny-list, channel narrowing, shape enforcement,
  command guard, path guard, delegation policy) plus an ADR formalizing
  mdBook as the docs platform.
  ([#96](https://github.com/htekdev/ai-harness/pull/96))
- **mdBook scaffold + Quickstart guide** — `docs/` directory, `book.toml`,
  full SUMMARY.md outline, and the canonical 5-minute Quickstart.
  ([#97](https://github.com/htekdev/ai-harness/pull/97))
- **Concept pages (5/5):** the public articulation of *Harness as Code* and
  its four primitives.
  - `concepts/harness-as-code.md` — the thesis page.
    ([#99](https://github.com/htekdev/ai-harness/pull/99))
  - `concepts/tools.md` — anatomy, sandbox, lifecycle.
    ([#100](https://github.com/htekdev/ai-harness/pull/100))
  - `concepts/hooks.md` — policy & observability primitive.
    ([#105](https://github.com/htekdev/ai-harness/pull/105))
  - `concepts/delegation.md` — sub-agents as a governed primitive.
    ([#106](https://github.com/htekdev/ai-harness/pull/106))
  - `concepts/governance.md` — the four-layer governance stack.
    ([#107](https://github.com/htekdev/ai-harness/pull/107))
- **GitHub Pages publishing pipeline** — `.github/workflows/pages.yml`
  (mdBook v0.4.40, cached binary, paths-filter on `docs/**`,
  `actions/upload-pages-artifact@v3` + `actions/deploy-pages@v4`,
  single `pages` concurrency). Pages enabled via API; deploy in ~10s after
  merge to `main`. Site live at <https://htekdev.github.io/ai-harness/>.
  ([#108](https://github.com/htekdev/ai-harness/pull/108),
  closes [#101](https://github.com/htekdev/ai-harness/issues/101))

### Fixed

- **Pre-flight tool-message ordering validation** in completion path — prevents
  malformed tool/assistant message sequences from reaching the model.
  ([#89](https://github.com/htekdev/ai-harness/pull/89),
  [#90](https://github.com/htekdev/ai-harness/pull/90))

### Stats

- 25 merged PRs since `v0.5.0`.
- 19 Go packages, all tests passing on Linux / macOS / Windows on Go 1.25.
- Five publicly browsable concept pages + Quickstart on the live docs site.

**Full changelog:** <https://github.com/htekdev/ai-harness/compare/v0.5.0...v0.6.0>

---

## [0.5.0] — 2026-06-06

> **Theme:** *Typed artifacts, context observability, and per-turn evaluation.*

### Added

- **Typed artifact registry** — five-type taxonomy (`override`, `harness`,
  `builtin`, `plugin`, `model`) with deterministic priority composition.
- **Context observability** — `harness context` subcommand with token-budget
  tracking, provenance, and JSON export.
- **Per-turn evaluation engine** — Starlark conditions evaluated every turn,
  with thread-safe `UpdateConditions` on the registry.
- **Compose options pattern** — `ComposeWith()` with `WithIncludeInactive`,
  `WithTypeFilter`, `WithTagFilter`, `WithEvalFn` functional options.
- **Comprehensive eval suite** — 50 cases, 94–100% passing against live models.
- **Statewright-compatible governance** — state-machine engine with temporal
  governance extensions.
- **Compaction artifact type** — context-management as a first-class composable
  artifact.
- **CLI journey** — `deploy`, `scaffold`, `inspect` subcommands.
- **Default working artifacts** — `harness init` scaffolds usable tools and hooks
  out of the box.
- `harness_artifact/v1alpha1` SCHEMA design spec.
- Examples directory + 16-file Quickstart guide.
- Pi-benchmark positioning doc with source-backed differentiation.
- Copilot CLI runtime reference re-articulated as artifacts.

### Fixed

- Seven harness bugs found and fixed via eval-driven development.
- CI lint formatting normalized across the entire codebase.

### Stats

- 65 commits since `v0.4.0`.
- 17 packages, all tests passing on Linux / macOS / Windows on Go 1.25.

**Full changelog:** <https://github.com/htekdev/ai-harness/compare/v0.4.0...v0.5.0>

---

## [0.4.0] — 2026-05-24

> **Theme:** *Per-turn evaluation engine + active-aware composition.*

### Added

- **Per-turn artifact evaluation engine**
  ([#35](https://github.com/htekdev/ai-harness/pull/35))
  - Artifacts with `condition` expressions are evaluated every turn via Starlark.
  - `Active` field on `Artifact` updated dynamically each turn.
  - `Registry.UpdateConditions` — thread-safe, non-fatal per-artifact errors.
  - `Composer.EvaluateConditions` bridges turn state into Starlark.
  - Agent loop integration: turn counter + composer call at start of each `Run()`.
  - 7 unit tests + 2 integration tests (343 lines added).
- **Active-aware composition & options pattern**
  ([#36](https://github.com/htekdev/ai-harness/pull/36),
  closes [#6](https://github.com/htekdev/ai-harness/issues/6))
  - `Compose(nil)` respects pre-computed `Active` field (backward-compatible).
  - `ComposeWith()` with `WithIncludeInactive`, `WithTypeFilter`, `WithTagFilter`,
    `WithEvalFn`.
  - `Registry.Active()` returns only active artifacts in priority order.
  - 12 new tests; all 14 packages pass.

### Changed

- README documents harness artifacts and `harness context` CLI commands.
- New "Typed Artifact System" and "Composition & Options Pattern" sections.

**Full changelog:** <https://github.com/htekdev/ai-harness/compare/v0.3.0...v0.4.0>

---

## [0.3.0] — 2026-05-22

### Added

- **Typed artifact registry & schema model**
  ([#32](https://github.com/htekdev/ai-harness/pull/32))
- **Context observability package + CLI subcommand**
  ([#33](https://github.com/htekdev/ai-harness/pull/33))

### Changed

- `gofmt` normalization across the entire codebase
  ([#31](https://github.com/htekdev/ai-harness/pull/31)).

**Full changelog:** <https://github.com/htekdev/ai-harness/compare/v0.2.0...v0.3.0>

---

## [0.2.0] — 2026-05-22

### Added

- **Composable harness architecture**
  ([#4](https://github.com/htekdev/ai-harness/pull/4)).
- **CLI entry point** — `run`, `init`, `validate`, `tools`, `hooks`, `agents`
  ([#25](https://github.com/htekdev/ai-harness/pull/25)).
- **GoReleaser + GitHub Actions CI/CD pipeline**
  ([#26](https://github.com/htekdev/ai-harness/pull/26)).

**Full changelog:** <https://github.com/htekdev/ai-harness/compare/v0.1.0...v0.2.0>

---

## [0.1.0] — 2026-05-20

> *Harness as Code* — the first stable release. Declarative AI agent governance
> in Go.

### Added

- **Markdown-first configuration** — `harness.md` (YAML frontmatter + body as
  system prompt) defines agents declaratively.
- **`.harness/` directory convention** — file-based tools, hooks, and agents
  auto-discovered on load.
- **Agent loop** — parallel tool execution, context management, streaming support.
- **Hook system** — eight lifecycle events (`session.start/end`, `turn.start/end`,
  `tool.pre/post`, `completion.pre/post`).
- **Starlark scripting engine** — 50+ built-ins (`fs`, `http`, `crypto`, `cache`,
  `metrics`, `regex`, `exec`).
- **Recursive delegation** — depth-limited agent trees with iteration budgets
  per depth.
- **Retry guards** — auto-block tools after two consecutive errors.
- **Path jailing** — filesystem operations sandboxed to working directory.
- **Secret detection** — hook-based credential scanning.
- **Completion client** — OpenAI-compatible with streaming SSE support.
- **Multi-model registry** — named models for delegation (Copilot, OpenAI,
  custom).
- **Eval framework** — YAML-driven LLM evaluation suite for CI.
- **CLI example** — interactive harness runner (`go run ./cmd/example/`).

### Design notes

- Single Go binary, ~5 dependencies.
- `tools.Handler` is `func(ctx, args) (string, error)` — no magic.
- Works with GitHub Copilot, OpenAI, or any compatible chat completions API.
- No Python; compiles in seconds.

[Unreleased]: https://github.com/htekdev/ai-harness/compare/v0.6.0...HEAD
[0.6.0]: https://github.com/htekdev/ai-harness/compare/v0.5.0...v0.6.0
[0.5.0]: https://github.com/htekdev/ai-harness/compare/v0.4.0...v0.5.0
[0.4.0]: https://github.com/htekdev/ai-harness/compare/v0.3.0...v0.4.0
[0.3.0]: https://github.com/htekdev/ai-harness/compare/v0.2.0...v0.3.0
[0.2.0]: https://github.com/htekdev/ai-harness/compare/v0.1.0...v0.2.0
[0.1.0]: https://github.com/htekdev/ai-harness/releases/tag/v0.1.0
