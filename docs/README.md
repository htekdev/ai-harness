# `docs/` — AI Harness Documentation

This is the source tree for the AI Harness documentation site, built with
[**mdBook**](https://rust-lang.github.io/mdBook/). See
[ADR-0001](./adr/0001-docs-platform.md) for why we chose mdBook.

## Local preview

```bash
# Install once
cargo install mdbook

# Live preview at http://localhost:3000
cd docs
mdbook serve --open
```

## Build static site

```bash
cd docs
mdbook build
# Output: docs/book/
```

## Layout

```
docs/
├── book.toml             # mdBook config
├── src/
│   ├── SUMMARY.md        # Sidebar / TOC
│   ├── introduction.md   # Landing page
│   ├── getting-started.md
│   ├── concepts/         # (Phase 6.1 follow-ups)
│   ├── guides/
│   ├── reference/
│   ├── examples/
│   └── project/          # ADRs, roadmap, contributing
└── adr/                  # ADRs (source of truth, also linked from /project)
```

## Authoring notes

- Use plain Markdown. No MDX, no React. This is intentional — see ADR-0001.
- Code blocks with language tags get syntax highlighting via highlight.js.
- mdBook supports `{{#include path}}` for re-using example snippets.
- Internal links use **relative paths to `.md` files** — mdBook rewrites them
  to the rendered `.html` automatically.

## Status

Phase 6.1 ships the scaffold + the [Quickstart](./src/getting-started.md).
Remaining pages (`concepts/*`, `guides/*`, `reference/*`, `examples/*`,
`project/*`) are stubbed in `SUMMARY.md` and will be filled in over the rest
of Phase 6.

## Why not auto-publish in this PR?

CI publishing (GitHub Pages) is a Phase 6.1 follow-up — once the scaffold is
on `main`, we wire a `pages.yml` workflow that runs `mdbook build` and
publishes the artifact. Until then, contributors preview locally with
`mdbook serve`.
