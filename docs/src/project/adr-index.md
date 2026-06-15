# Architecture Decision Records

Architecture Decision Records (ADRs) capture the **why** behind significant,
hard-to-reverse decisions in `ai-harness`. Each ADR is a short Markdown file
in [`docs/adr/`][adr-dir] with a stable number, a status, and an explicit
*revisit triggers* section.

We use ADRs because the harness has a **deliberately tiny core** and a wide
extensibility surface — getting the boundary right matters more than any one
feature, and we want the rationale on record for future contributors.

---

## When to write an ADR

Open an ADR PR when a change:

- Picks a tool or platform we will be locked into for a year or more
  (docs platform, observability backend, scripting engine).
- Defines a public artifact contract (`harness.md` schema, tool-artifact
  schema, hook-artifact schema).
- Changes the harness boundary (what belongs in the core vs an artifact vs a
  hook vs a builtin).
- Sets a cross-cutting policy (network defaults, retry semantics,
  `finish_reason` handling).

Skip the ADR if the change is:

- A bug fix or refactor that does not change a contract.
- A docs-only update.
- A new tool, hook, or builtin that does not require new core wiring.

When in doubt, open the PR with the change *and* an ADR — reviewers will tell
you if the ADR is unnecessary.

---

## Index

| # | Title | Status | Date |
|---|-------|--------|------|
| [0001](https://github.com/htekdev/ai-harness/blob/main/docs/adr/0001-docs-platform.md) | Documentation platform: mdBook | Accepted | 2026-06-14 |

> The table is hand-maintained. When you add a new ADR file, update this
> table in the same PR. CI does not yet enforce this, but reviewers will.

---

## Authoring conventions

- **Filename:** `NNNN-kebab-case-title.md`, where `NNNN` is the next
  zero-padded number. Numbers are never reused, even for superseded ADRs.
- **Status:** one of `Proposed`, `Accepted`, `Superseded by ADR-NNNN`,
  or `Deprecated`. ADRs are immutable once accepted — to change a decision,
  write a new ADR that supersedes the old one.
- **Required sections:**
  - `Context` — what forced the decision.
  - `Options considered` — at least two, with honest trade-offs.
  - `Decision` — one sentence, then rationale.
  - `Consequences` — what becomes easier and what becomes harder.
  - `Revisit triggers` — the concrete conditions that would make us
    re-open this ADR. This is the most important section. If you cannot
    name a trigger, the decision is probably premature.

A minimal template:

```markdown
# ADR NNNN — Short title

**Status:** Proposed
**Date:** YYYY-MM-DD
**Phase:** <roadmap phase, e.g. 6.1>
**Decider:** <agent or human>

## Context

What problem are we solving? What forces are at play?

## Options considered

### Option A — ...
- Pros / cons

### Option B — ...
- Pros / cons

## Decision

**We will <do X>.**

Rationale:

1. ...
2. ...

## Consequences

- What becomes easier.
- What becomes harder.
- What new work this implies.

## Revisit triggers

We revisit this decision if **any** of:

- <concrete trigger 1>
- <concrete trigger 2>
```

---

## Process

1. **Open a PR** that adds the ADR file under `docs/adr/` and updates the
   index table on this page.
2. **Mark the ADR `Proposed`** until it merges.
3. **Flip to `Accepted`** in the same PR if reviewers approve, or in a
   follow-up commit on the same PR if the discussion landed somewhere
   different.
4. **Never edit an Accepted ADR** except to add `Superseded by ADR-NNNN` to
   its status — write a new ADR instead.

This keeps the historical record honest: future contributors can see what we
believed at each point, not just what we believe now.

---

## See also

- [Contributing guide](./contributing.md) — branch naming, test bar, PR
  conventions.
- [Roadmap](./roadmap.md) — where the project is going and which open
  questions are likely to spawn the next ADRs.

[adr-dir]: https://github.com/htekdev/ai-harness/tree/main/docs/adr
