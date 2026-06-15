# Roadmap

This page describes where `ai-harness` is going and how you can help. It is a
**summary for contributors** — the canonical, fully detailed plan lives in the
project's internal spec (`data/specs/ai-harness-roadmap.md` in the planning
repository); this page extracts the parts that matter for OSS contributors and
keeps them in sync with what is actually shipped.

> **Status legend**
>
> | Symbol | Meaning |
> |--------|---------|
> | ✅ | Shipped on `main` |
> | 🚧 | In progress (PRs open, scoped) |
> | 📋 | Planned, design accepted, not yet started |
> | 🤔 | Open question — feedback wanted |

If a row is marked 🚧 or 📋 and you want to take it on, open a discussion or
comment on the linked tracking issue **before** opening a PR — most items have
non-obvious design constraints captured in their issue threads.

---

## Phases at a glance

| Phase | Theme | Status |
|-------|-------|--------|
| 1 | CLI & Developer Experience | ✅ Shipped |
| 2 | Dynamic Context & Memory | 🚧 In progress |
| 3 | Async Tool Calling | 📋 Planned |
| 4 | Event Sources (Extension Parity) | 📋 Planned |
| 5 | Production Hardening | 🚧 In progress |
| 6 | Community & Launch | 🚧 In progress |

The phases are **sequenced**, not strict gates: hardening and community work
run in parallel with later feature phases.

---

## Phase 1 — CLI & Developer Experience ✅

**Goal:** make `harness` a standalone binary anyone can install and use without
writing Go.

Shipped:

- `harness run`, `harness eval`, `harness validate`, `harness init`,
  `harness tools list`, `harness hooks list`, `harness agents list`,
  `harness serve` — see the [CLI reference](../reference/cli.md).
- `harness init` scaffolding — `harness.md` plus `.harness/{tools,hooks,agents}`.
- GoReleaser-based releases for Linux/macOS/Windows on amd64 + arm64.
- GitHub Pages-hosted docs (this site) built with mdBook.
- CI matrix on Go 1.25 across ubuntu/macos/windows, plus lint and mdBook build.

Where to contribute:

- Polish `harness init` templates — additional starter kits live well as
  community PRs.
- Improve error messages from `harness validate`. Open an issue with the
  validation case before sending a PR so we can agree on the message shape.

---

## Phase 2 — Dynamic Context & Memory 🚧

**Goal:** make `Context` a first-class primitive — declarative, conditional,
runtime-loaded knowledge that replaces hard-coding context into system prompts.

In progress:

- **2.1 Context source registry** (issue [#69]) — `context.sources` in
  `harness.md` with `when:` Starlark predicates.
- **2.2 Compaction engine** — `summarize` strategy with retention rules for
  system prompt, last-N turns, tool results, and dynamic context.
- **2.3 Memory tiers** — `core` / `working` / `long-term` / `events` loaded
  from flat files under `.harness/memory/`.

Open questions:

- 🤔 Should compaction be a hook event or a dedicated engine?
  *Current leaning: dedicated engine — too complex to model as a hook.*
- 🤔 Memory persistence — flat files or SQLite?
  *Current leaning: flat files (git-friendly, simpler).*

Where to contribute:

- Eval cases that exercise `when:` predicates over real session state are the
  highest-leverage contribution right now — the engine will land first, then
  evals lock in the contract.
- Example harness configs that show good context patterns (PR-mode,
  multi-language, quiet-hours) are great PR candidates once 2.1 lands.

---

## Phase 3 — Async Tool Calling 📋

**Goal:** parallel tool execution within a turn via a dependency graph,
synchronized at the agent loop boundary.

Design highlights:

- **Loop-boundary barrier:** the agent loop itself is the synchronization
  point — there is no explicit `await` from Starlark.
- **Starlark primitives:** `async.launch`, `async.wait_all`, `async.wait_any`,
  `async.race`, plus `depends_on=[...]` for dependency edges.
- **DAG cycle detection** at declaration time, not at runtime.
- **Backward compatible:** existing sync tools are unchanged; async is opt-in.

Where to contribute:

- The async design is documented but not implemented. We will accept design
  feedback issues now and code PRs once `async/` package skeleton lands.
- See issue [#104] for the related `agent.stop` hook event work, which is a
  prerequisite for clean async cancellation.

---

## Phase 4 — Event Sources (Extension Parity) 📋

**Goal:** close the gap between what Copilot CLI extensions can do (timers,
HTTP servers, file watchers, secrets, databases) and what the harness supports
natively.

Planned event sources:

| Type | Purpose |
|------|---------|
| `timer` | Cron / interval triggers. |
| `http` | Inbound webhook routes. |
| `fs` | File watcher with hot-reload. |

Planned Starlark modules:

- `secrets.*` — typed secret access (replaces raw `env()` for sensitive values).
- `db.*` — SQLite query/exec primitives.
- `session.*` — durable cross-restart state.
- `server.*` — HTTP server registration.
- `timer.*` — interval / one-shot timers.

Where to contribute:

- File-watcher prior art exists in the rocha-family extensions; PRs that port
  one event source at a time (timer first) are very welcome once the
  `events/` package skeleton lands.

---

## Phase 5 — Production Hardening 🚧

Mostly shipped — what remains is incremental polish.

Shipped:

- Structured logging (`slog`).
- OpenTelemetry tracing — spans per tool call, delegation, completion. See the
  [observability guide](../guides/observability.md).
- Network sandbox with default-deny domain allowlists for `http.*`. See the
  [`harness.md` reference](../reference/harness-md.md#network).
- `finish_reason` strict guard — `length` triggers retry, `content_filter` is a
  hard error, unknown reasons are retriable errors.
- Shape A typed artifact bundle loader for `.harness/{plugins,builtins,overrides}`.
- Claims verification — Ralph loop at the delegation boundary.

In progress:

- Streaming mode polish for the CLI (token-by-token output).
- Per-model and per-tool rate limiting.
- Tool allow/deny lists at the config level (today: hooks-only enforcement).

---

## Phase 6 — Community & Launch 🚧

You are reading part of this phase right now.

Shipped:

- mdBook docs site at <https://htekdev.github.io/ai-harness/>.
- All concept pages: harness-as-code, tools, hooks, delegation, governance,
  verification.
- All guides: writing a tool, writing a hook, writing a context, deployment,
  observability.
- All reference pages: `harness.md` frontmatter, tool artifact, hook artifact,
  CLI, Starlark built-ins.
- Examples: governed-agent flagship walkthrough.
- `CHANGELOG.md` (Keep-a-Changelog v1.1.0).
- Contributing guide — see [Contributing](./contributing.md).

In progress:

- This page (Roadmap).
- [ADR Index](./adr-index.md).
- Network sandboxing guide (stretch).
- v0.6.0 release tag — pending versioning decision (see open questions).

Open questions:

- 🤔 **v0.6.0 vs v1.0.0-rc1.** All Phase 6.1/6.2 work is accumulated on `main`;
  the question is whether the next tag is a 0.x release or our first
  release-candidate for 1.0.

---

## Open questions across phases

| # | Question | Current leaning |
|---|----------|-----------------|
| 1 | Compaction as hook event vs dedicated engine? | Dedicated engine. |
| 2 | Memory persistence — SQLite vs flat files? | Flat files. |
| 3 | CLI `--watch` mode? | Yes, Phase 1 stretch. |
| 4 | Hook packs — Go modules or MD bundles? | MD bundles. |
| 5 | Event sources — config-only or runtime-registrable? | Both (config primary). |
| 6 | v0.6.0 vs v1.0.0-rc1? | Open. Feedback welcome. |

---

## How to contribute

1. **Pick something marked 🚧 or 📋** that you want to take on.
2. **Open or comment on the tracking issue** before sending a PR. Most items
   have non-obvious design constraints in the issue thread.
3. **Read the [Contributing guide](./contributing.md)** for local dev,
   branch naming, the test bar, and PR conventions.
4. **Run the full local check** before pushing:

   ```bash
   go test ./...
   go vet ./...
   gofmt -l .
   harness validate -v
   ```

5. **Keep the core small.** When in doubt, prefer a hook, a Starlark builtin,
   or a typed artifact over adding magic to the harness core. The project
   motto is *"keep the core tiny, make the edges powerful."*

If you're not sure where to start, look at issues tagged `good-first-issue`
or open a discussion describing what you want to build — we'll point you at
the closest existing primitive.

---

[#69]: https://github.com/htekdev/ai-harness/issues/69
[#104]: https://github.com/htekdev/ai-harness/issues/104
