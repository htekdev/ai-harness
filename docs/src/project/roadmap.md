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
| 4 | Event Sources (Extension Parity) | ✅ Shipped |
| 5 | Production Hardening | ✅ Shipped |
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

## Phase 4 — Event Sources (Extension Parity) ✅

**Goal:** close the gap between what Copilot-style extensions can do at the
edge (external input, durable offsets, long-running serve loops) and what the
harness supports natively.

Shipped:

- Telegram and MeshWire input sources with durable offsets, replier contracts,
  and `harness serve` runtime integration.
- Declarative `serve:` config so a harness can describe source wiring in
  `harness.md` instead of via repeated CLI flags.
- Eval coverage for source runtime behavior, especially durable offsets and
  serve-mode orchestration.

Follow-on work:

- Additional timer / webhook / file-watcher style sources remain good
  community contributions, but they are no longer blockers for launch.

---

## Phase 5 — Production Hardening ✅

**Goal:** make the harness safe to run as a long-lived, observable, governed
service instead of only as a local CLI demo.

Shipped:

- 5.1 typed errors.
- 5.2 structured logging (`slog`).
- 5.3 OpenTelemetry tracing — spans per tool call, delegation, completion. See
  the [observability guide](../guides/observability.md).
- 5.4 streaming CLI output.
- 5.5 network sandbox with default-deny domain allowlists for `http.*`. See
  the [`harness.md` reference](../reference/harness-md.md#network).
- 5.6 rate limiting.
- 5.7 configurable retry/backoff policy.
- 5.8 self-augmenting harness primitives.
- 5.9 config-driven tool allow/deny lists.
- 5.10 deployment recipes.

Status:

- Phase 5 closed on 2026-06-14 03:00 CT with all ten production-hardening
  milestones merged.
- The live Telegram bot was rebuilt on `main` at 2026-06-14 03:09 CT.

---

## Phase 6 — Community & Launch 🚧

**Goal:** move AI Harness from “fully featured, production-grade Go harness” to
“the reference implementation of Harness as Code with real adopters.”

Kickoff status:

- Phase 5 is fully closed.
- The Phase 6 foundation is already live on `main`: mdBook docs site, the
  Quickstart, concept pages, the governed-agent example, `CHANGELOG.md`, and
  GitHub Pages publishing at <https://htekdev.github.io/ai-harness/>.

Tracks:

### 6.1 Documentation site

- Finish the remaining guides, reference pages, examples, and deployment
  coverage needed for a complete mdBook that can also mirror onto htek.dev.

### 6.2 Launch sequence

- Cut the release/tag sequence once docs + examples are complete, publish the
  launch post, distribute to core Go / agent communities, and prepare the
  tutorial/video, blueprint bundle, and conference submissions.
- Detailed execution for the launch sequence is tracked in issue #98.

### 6.3 Reference content

- Publish the article/tutorial set that explains the thesis, governance model,
  migration story, and production patterns around Harness as Code.

### 6.4 Future extension points (post-launch)

- Plugin packages (Go modules) for external tool bundles.
- Hook packs as reusable Markdown governance artifacts.
- Harness inheritance / overlays.
- VS Code validation + Starlark-highlighting support.
- Community marketplace patterns.

Sequencing:

1. **Week 1:** docs/getting-started, concepts/tools, concepts/hooks, and one
   full governed-agent example.
2. **Week 2:** fill remaining concepts/guides/reference pages, polish
   examples, and cut the first release candidate decision.
3. **Week 3:** publish the launch post, cut the public release tag, and post to
   r/golang, Hacker News, and Go Discord.
4. **Week 4+:** tutorial series, blueprint bundle, and conference proposals.

Open questions:

- 🤔 **Docs platform long-term.** mdBook is the current repository source of
  truth; revisit Astro/Starlight only if the htek.dev mirror eventually needs a
  different publishing stack.
- 🤔 **v1.0.0 SemVer commitments.** Likely-stable surfaces: CLI flags &
  subcommands, hook event catalog, Starlark builtins, config schema, exit
  codes. Likely-unstable: internal Go APIs.
- 🤔 **Naming / trademark.** Decide whether “Harness as Code” stays a descriptive
  phrase or becomes a formalized wordmark before launch.

---

## Open questions across phases

| # | Question | Current leaning |
|---|----------|-----------------|
| 1 | Compaction as hook event vs dedicated engine? | Dedicated engine. |
| 2 | Memory persistence — SQLite vs flat files? | Flat files. |
| 3 | CLI `--watch` mode? | Yes, Phase 1 stretch. |
| 4 | Hook packs — Go modules or MD bundles? | MD bundles. |
| 5 | Event sources — config-only or runtime-registrable? | Both (config primary). |
| 6 | Which surfaces are stable for v1.0.0? | CLI / hooks / Starlark / config / exit codes. |

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
