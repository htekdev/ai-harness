# AI Harness

**Harness as Code — declarative AI agent governance in Go.**

> Like Infrastructure as Code, but for AI agent behavior. Every prompt ships
> with its governance. Every agent behavior is reproducible, reviewable, and
> testable.

AI Harness is a minimal, governance-first runtime for coding agents, where
behavior lives in composable, versioned Markdown artifacts.

## Three pillars

1. **Minimal core.** A small, inspectable Go runtime — a single binary with
   a handful of dependencies. The harness is the thinnest layer between your
   model and your tools.
2. **Composable artifacts.** Your `harness.md` (system prompt + frontmatter)
   plus a `.harness/` directory of one-file-per-capability tools, hooks, and
   sub-agents. Every artifact is reviewable in a PR.
3. **Governance by default.** Hooks, tool policies, delegation limits, network
   sandboxes, and command guards live in the execution path — not bolted on
   after the fact.

## Who this is for

- Engineers building agents that need to survive code review.
- Platform teams who want **the same harness behavior across dev, CI, and
  production** with no hidden state.
- Anyone who has felt that current agent frameworks make the wrong things
  easy (200-line YAML files) and the right things hard (auditing what a tool
  call actually did).

## Where to go next

- **Just want to run something?** → [Quickstart](./getting-started.md)
- **Want the conceptual model?** → [Harness as Code](./concepts/harness-as-code.md)
- **Want the flagship reference?** → [Governed Agent example](./examples/governed-agent.md)

## Status

AI Harness is approaching **v1.0.0** through Phase 6 — Community & Launch.
SemVer stability commitments will be documented in the `v1.0.0` release notes.
Until then, the public surfaces tracked in [the roadmap](./project/roadmap.md)
are stabilizing.
