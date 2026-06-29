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
│   ├── concepts/         # Mental model, primitives, governance
│   ├── guides/           # End-to-end workflows and operations
│   ├── reference/        # CLI, schema, artifacts, event catalog
│   ├── examples/         # Narrative walkthroughs tied to runnable configs
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

The Phase 6 docs foundation is already shipped: mdBook scaffold, the
[Quickstart](./src/getting-started.md), concept pages, reference content,
guides, examples, and project docs are all live in this tree. Remaining Phase 6
work is breadth/polish, additional examples, and keeping the eventual htek.dev
mirror in sync.

## Publishing

GitHub Pages publishing is active via
[`.github/workflows/pages.yml`](../.github/workflows/pages.yml): pushes to
`main` that touch `docs/**` build the mdBook and deploy it to
<https://htekdev.github.io/ai-harness/>. For local iteration, use
`mdbook serve`.
