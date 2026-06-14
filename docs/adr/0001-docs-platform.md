# ADR 0001 — Documentation platform: mdBook

**Status:** Accepted
**Date:** 2026-06-14
**Phase:** 6.1 — Documentation Site
**Decider:** ai-harness domain agent

## Context

Phase 6.1 ships a documentation site at `docs/`. The roadmap leans toward
mdBook, but Astro Starlight has been a recurring alternative because the
htek.dev marketing site already runs on Astro. Before we start writing
content under `docs/`, we need to commit to one platform so authors can
write Markdown without re-templating later.

## Options considered

### Option A — mdBook (Rust, single binary)

- Single static binary, no Node toolchain required.
- Default audience for `htekdev/ai-harness` is the Go community; mdBook is
  the default in that ecosystem (Rust Book, GitHub CLI manual, many Go
  proposals).
- Built-in search, themes, code highlighting, redirects.
- CI is a one-step `cargo install mdbook && mdbook build`.
- Output is plain HTML — trivial to mirror at `htek.dev/ai-harness/docs`.
- No JSX, no MDX, no React component story. Pure Markdown.

### Option B — Astro Starlight (Node, framework)

- Beautiful out of the box, MDX components for embedded demos.
- Same toolchain as `htek.dev` so we get free dev-server reuse.
- Heavier: pulls in Node, npm, hundreds of dependencies in CI.
- MDX is great for marketing pages, overkill for a CLI/library reference.
- Blends two audiences (marketing + Go reference docs) on one stack —
  bigger blast radius if either side breaks.

### Option C — README-only

- Free, but already cracking under the weight (~58 KB README).
- No nav, no per-section search, no per-version deep links.

## Decision

**We will use mdBook.**

Rationale:

1. **Audience fit.** The Go/Rust developer audience expects mdBook-style
   docs. Pi, Cobra, and most Go-proposals-style projects use it.
2. **Toolchain simplicity.** No Node dependency means CI builds the docs in
   under 30 seconds and contributors don't need npm. This matters because
   we are *not* shipping interactive demos at v1.0.0 — we're shipping
   reference material.
3. **Lower coupling to htek.dev.** htek.dev is Astro/MDX; we *want* the
   ai-harness docs to be independently buildable so OSS contributors don't
   need access to the marketing repo.
4. **Mirroring is cheap.** mdBook outputs static HTML. We can mirror at
   `htek.dev/ai-harness/docs/` via a build artifact upload — no need to
   port content into Astro.
5. **Future flexibility.** If we ever need MDX-grade interactive components
   (live config validators, in-page playgrounds), we can add them as
   embedded sub-pages on htek.dev without ripping out mdBook.

## Consequences

- A new `docs/` tree with `book.toml` will land in Phase 6.1.
- CI gains an `mdbook build` step (or GitHub Pages workflow).
- Authors write plain Markdown; no React components in docs.
- The Phase 6.1 deliverables in `data/specs/ai-harness-roadmap.md` (`getting-started.md`,
  `concepts/*`, `guides/*`, `reference/*`, `examples/*`) are written
  directly as `docs/SUMMARY.md` entries with no transformation.

## Revisit triggers

We revisit this decision if **any** of:

- We need interactive in-page demos (config playground, hook simulator) for
  v1.x marketing pushes.
- Contributor friction reports show mdBook is blocking writers (unlikely —
  it's plain Markdown).
- We merge the docs site with htek.dev's content pipeline.
