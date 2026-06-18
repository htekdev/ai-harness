# ADR 0002 — Artifact-first naming; defer "extensions" as the primary abstraction

**Status:** Accepted
**Date:** 2026-06-18
**Phase:** 6.2 — Community & Launch
**Decider:** ai-harness domain agent

## Context

`ai-harness` ships several user-extensible building blocks: tools, hooks,
sub-agents, contexts, policies, and event sources. They are all loaded from
Markdown files under `.harness/{builtins,plugins,overrides}/` with YAML
frontmatter, and they all participate in the same per-turn evaluation loop.

Throughout 2026 the project — and the broader agent-harness ecosystem — has
casually called these things "extensions". That word is convenient but it
**hides the most important property of the system**: every one of these
files is a typed, declarative *artifact* that is composed at run time, not a
runtime extension point in the traditional plugin sense.

We need to commit to public language **before** v1.0 so that:

1. The README, docs site, CHANGELOG, and CLI help all use one vocabulary.
2. Future schemas (`harness_artifact/v1alpha1`, sub-agent artifact schema)
   can build on a coherent noun set instead of fighting the term
   "extension".
3. Contributors writing guides, blog posts, or third-party integrations
   reach for the same words.

## Options considered

### Option A — Keep "extensions" as the primary noun

- Familiar from VS Code, Chrome, Pi, OpenHarness, etc.
- Easy onboarding hook ("write a Harness extension!").
- But: collides with VS Code/Chrome mental model where extensions are
  imperative code that hooks into runtime APIs. Our artifacts are
  **declarative Markdown** evaluated each turn — closer to Kubernetes
  manifests than to a Chrome extension.
- Locks us into a noun that fights the bias-framework positioning ("Harness
  as Code", "Infrastructure-as-Code for agents").

### Option B — Coin a new umbrella term

- E.g. "definitions", "modules", "capabilities", "bundles", "manifests".
- Most precise option, but every term has prior art elsewhere and forces
  contributors to learn a new word with no upside over the actual concept.
- Bikeshed risk; defers the real decision.

### Option C — Artifact-first language with descriptive sub-nouns

- **Artifacts** is the umbrella for any Markdown-defined unit
  (`harness_artifact/v1alpha1`).
- Sub-nouns are descriptive and already in use:
  - **tools**, **hooks**, **agents**, **contexts**, **policies**,
    **sources** — what they *are*.
  - **definitions**, **bundles** — for grouped artifacts in one file.
  - **events**, **triggers**, **watchers**, **schedules**, **runtimes** —
    for the runtime surface they bind to.
- "Extension" stays available as compatibility / colloquial wording but is
  **not** the primary abstraction in new copy.

## Decision

**Adopt Option C — artifact-first language.**

Specifically:

1. **Public surface (README, docs site, CHANGELOG, release notes,
   blog posts):** primary noun is **artifact** with descriptive sub-nouns
   (tool / hook / agent / context / policy / source). Avoid "extension" as
   the lead term in new copy.
2. **CLI help / `harness validate` / `harness *` subcommands:** keep using
   the descriptive sub-nouns directly (`harness tools list`,
   `harness hooks list`, `harness agents list`). Do not introduce
   `harness extensions list`.
3. **Schemas:** the canonical kind name is `harness_artifact/v1alpha1`.
   Bundle files use the same kind. Frontmatter does not introduce
   `kind: extension`.
4. **Internal code, types, package names:** continue using the precise
   noun for the thing (`tools.Definition`, `hooks.Definition`,
   `agents.Spec`). Do not introduce a generic `Extension` interface.
5. **Compatibility:** the word "extensions" may appear in legacy doc
   anchors, old release notes, and informal conversation. We do not rename
   historical artifacts; we only change *new* writing.

## Consequences

- README, docs site landing pages, and concept pages already use
  artifact-first language. New copy must continue this convention.
- Reviewers should push back on PRs that reintroduce "extension" as the
  primary abstraction in user-facing copy.
- Third-party authors writing about `ai-harness` are not bound by this
  ADR, but our own writing (and any official "what to call it" guidance)
  follows artifact-first language.
- Future ADRs that propose new artifact kinds (e.g. evaluators, verifiers,
  routes) extend this vocabulary instead of inventing a new umbrella.

## Revisit triggers

We revisit this decision if **any** of:

- A user-research signal (issues, discussions, conference feedback) shows
  the word "artifact" actively confuses newcomers more than it informs
  them.
- We adopt a packaging/distribution surface (e.g. a registry) where
  "extension" is the dominant ecosystem term and resisting it costs
  adoption.
- We unify with another harness project whose schemas already use a
  different umbrella term and convergence is more valuable than naming
  purity.

## References

- README §"Naming and terminology direction"
- `data/specs/ai-harness-product-spec.md` — artifact model
- `data/specs/ai-harness-domain-agent-v1.md` — positioning notes
