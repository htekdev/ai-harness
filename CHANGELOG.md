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

Pending items tracked in [#98 — Phase 6.2 Launch Sequence](https://github.com/htekdev/ai-harness/issues/98).

---

## [0.6.0] — 2026-06-15

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

#### Phase 6.1 — Guides and reference (executable docs)

- **Quickstart end-to-end executable** — every command in `getting-started/quickstart.md` runs against the shipped CLI exactly as written.
  ([#113](https://github.com/htekdev/ai-harness/pull/113))
- **`guides/writing-a-tool.md`** — Markdown-tool authoring loop with Starlark `script:` bodies, parameter typing, return shapes, and validation.
  ([#112](https://github.com/htekdev/ai-harness/pull/112))
- **`guides/writing-a-hook.md`** — full hook authoring guide: event catalog, payload shapes, decision contract (`allow`/`block`/`modify`), `when:` predicates, priority bands.
  ([#114](https://github.com/htekdev/ai-harness/pull/114))
- **`guides/writing-a-context.md`** — context-source authoring guide.
  ([#117](https://github.com/htekdev/ai-harness/pull/117))
- **`guides/deployment.md`** — Docker, systemd, Compose, secret handling, and `harness serve` operational guidance.
  ([#118](https://github.com/htekdev/ai-harness/pull/118))
- **`guides/observability.md`** — local OTel collector, span tree, attribute reference, trace-correlated logs, cost telemetry, smoke checklist.
  ([#120](https://github.com/htekdev/ai-harness/pull/120))
- **`reference/cli.md`** — exhaustive per-subcommand flag tables, global flags + `HARNESS_*` env vars, exit codes, serve-source env requirements.
  ([#124](https://github.com/htekdev/ai-harness/pull/124))
- **`examples/governed-agent.md`** — flagship 7-scenario governed-agent walkthrough end-to-end (hook block, registry deny, command_guard, network sandbox, span tree).
  ([#125](https://github.com/htekdev/ai-harness/pull/125))
- **`reference/harness-md.md`** — exhaustive `harness.md` frontmatter reference: every top-level field, retry bounds, `validate()` checks, `serve:` per-source schema, network defaults.
  ([#129](https://github.com/htekdev/ai-harness/pull/129))
- **`reference/tool-artifact.md`** — exhaustive per-tool `.md` schema, Parameter sub-schema, Starlark dialect constraints, 6-step runtime lifecycle, `async` reserved (cross-linked to #104).
  ([#130](https://github.com/htekdev/ai-harness/pull/130))
- **`reference/hook-artifact.md`** — exhaustive per-hook `.md` schema, full event catalog, canonical Starlark payload shapes, decision contract, priority bands.
  ([#131](https://github.com/htekdev/ai-harness/pull/131))
- **`reference/starlark-builtins.md`** — exhaustive catalog of every builtin registered by `scripting.Engine.makeBuiltins`: decision builtins, diagnostic builtins, full per-module tables (time/json/math/os/url/uuid/http/re/hash/base64/crypto/string/template/validate/set/cache/metrics/fs/ctx/exec/meta), hook conventions, intentionally-not-exposed surface.
  ([#132](https://github.com/htekdev/ai-harness/pull/132))
- **`project/contributing.md`** — full contributor manual: local dev setup, canonical local checks, branch/PR conventions, test bar, doc rules, release policy.
  ([#133](https://github.com/htekdev/ai-harness/pull/133))
- **`project/roadmap.md` + `project/adr-index.md`** — contributor-facing roadmap (Phase 1-6 with status legend, open questions) and ADR index with authoring conventions.
  ([#134](https://github.com/htekdev/ai-harness/pull/134))

#### Phase 7.1 — Claims verification (preview)

- **Ralph-loop claims verifier at the delegation boundary** — verification primitive that re-checks a sub-agent's claims against ground-truth before the parent accepts results.
  ([#110](https://github.com/htekdev/ai-harness/pull/110))

#### Release infrastructure

- **`CHANGELOG.md`** — Keep-a-Changelog 1.1.0 + SemVer policy, full v0.1.0 → v0.6.0 backfill, public-API surface clause for the pre-1.0 schema-evolution window.
  ([#109](https://github.com/htekdev/ai-harness/pull/109))
- **CI maintenance** — bump `actions/cache` 4→5, `actions/deploy-pages` 4→5, `actions/upload-pages-artifact` 3→5.
  ([#126](https://github.com/htekdev/ai-harness/pull/126),
  [#127](https://github.com/htekdev/ai-harness/pull/127),
  [#128](https://github.com/htekdev/ai-harness/pull/128))

### Fixed

- **Pre-flight tool-message ordering validation** in completion path — prevents
  malformed tool/assistant message sequences from reaching the model.
  ([#89](https://github.com/htekdev/ai-harness/pull/89),
  [#90](https://github.com/htekdev/ai-harness/pull/90))
- **`finish_reason=length` truncation detection** — `agent.Run` / `agent.RunStream` now error retriably when the model returns `length` with degenerate `tool_calls`, instead of silently treating a truncated response as a final answer.
  ([#121](https://github.com/htekdev/ai-harness/pull/121))
- **Strict `finish_reason` guard at the agent boundary** — only `stop` / `end_turn` / `""` fall through as final answers; `content_filter` is now a hard error; unknown `finish_reason` with no `tool_calls` is a retriable error. Plus: `config/loader.go` now scans `.harness/{plugins,builtins,overrides}` for Shape A typed-artifact bundles via `ParseBundleMarkdown` (was previously ignored by `serve`/`validate` even though the artifact registry already loaded them).
  ([#123](https://github.com/htekdev/ai-harness/pull/123))

### Stats

- ~50 merged PRs since `v0.5.0` (25 Phase 6.1 docs + reference, 8 fixes / hardening, dependency bumps, claims verification preview).
- 19 Go packages, all tests passing on Linux / macOS / Windows on Go 1.25.
- 7-check CI bar: Lint, Build, Test (ubuntu/macos/windows on Go 1.25), Build mdBook.
- Public docs site live at <https://htekdev.github.io/ai-harness/> with full concept pages, all five guides, complete reference (CLI, `harness.md`, tool/hook artifact schemas, Starlark builtins), governed-agent example, contributor manual, roadmap, and ADR index.

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
