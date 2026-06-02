# tracker: AI Harness vNext backlog (2026-05-21 brainstorm)

## Context
This tracker captures Hector's 2026-05-21 AI Harness brainstorm and the follow-up work required to turn it into a real product backlog.

## Key thesis
- minimal core, extreme extensibility
- typed/composable artifacts instead of vague extensions
- sub-agents, async, hooks, monitoring, and event-driven persistence as primitives
- context observability as a differentiator
- model onboarding without rebuilding the core

## Linked issues
- https://github.com/htekdev/ai-harness/issues/5
- https://github.com/htekdev/ai-harness/issues/6
- https://github.com/htekdev/ai-harness/issues/7
- https://github.com/htekdev/ai-harness/issues/8
- https://github.com/htekdev/ai-harness/issues/9
- https://github.com/htekdev/ai-harness/issues/10
- https://github.com/htekdev/ai-harness/issues/11
- https://github.com/htekdev/ai-harness/issues/12
- https://github.com/htekdev/ai-harness/issues/13
- https://github.com/htekdev/ai-harness/issues/14
- https://github.com/htekdev/ai-harness/issues/15
- https://github.com/htekdev/ai-harness/issues/16

### Long-running primitive follow-ups
- https://github.com/htekdev/ai-harness/issues/18 (event store)
- https://github.com/htekdev/ai-harness/issues/19 (runtime handles)
- https://github.com/htekdev/ai-harness/issues/20 (triggers)
- https://github.com/htekdev/ai-harness/issues/21 (scheduling primitive)
- https://github.com/htekdev/ai-harness/issues/22 (watcher adapters)

## Working design direction
Durable event stream + projections + triggers + leased runtimes + schedule data.

Initial scaffold target: a file-backed events package on the `copilot/long-running-primitives` branch.

## Notes
Pi is the closest public benchmark for the minimal-harness philosophy, but AI Harness should go further on typed artifacts, per-turn evaluation, conditional composition, and explicit context observability.

## Governance engine (Phase 2+)

Statewright evaluation complete (see `governance/STATEWRIGHT_EVAL.md`).

**Decision: Hybrid** — Go-native state machine engine with Statewright-compatible JSON workflow schema.

- `governance/` package scaffolded: workflow types, evaluator, trace log, enforcement engine.
- Schema is 100% source-compatible with Statewright JSON format.
- AI Harness extensions: `trace_rules` (temporal patterns), `blocked_tools`, `enter_hook`/`exit_hook`, `fact_refs`.

Next steps for Phase 2:
- Wire `governance.EnforcementEngine` into the `tool.pre` hook
- Load `.harness/workflows/*.json` artifacts in `config/loader.go`
- Fork/join and sub-machine invoke evaluator logic
- Connect `observe/event_store.go` events to `governance.TraceLog`
