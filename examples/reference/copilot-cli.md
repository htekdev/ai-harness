# Copilot CLI Runtime → AI Harness Artifacts (Reference)

This reference reverse-engineers core Copilot CLI runtime concepts into AI Harness artifacts.

## Replayable Reference Harness

A runnable artifact set lives at:

- `/tmp/workspace/htekdev/ai-harness/examples/reference/copilot-cli/identity.md`
- `/tmp/workspace/htekdev/ai-harness/examples/reference/copilot-cli/plugins/copilot-runtime.md`

To inspect or replay in a project harness tree:

1. Copy files into a `.harness/` tree matching AI Harness conventions.
2. Run `harness validate`.
3. Run `harness context`, `harness tools`, and `harness hooks` to inspect active composition.

## Concept Mapping

| Copilot CLI concept | AI Harness expression | Reference artifact location |
|---|---|---|
| System runtime identity | `type: harness` artifact with runtime guidance context | `identity.md` |
| Named tools (including wrapper-over-built-in patterns) | `tools[]` definitions, including `bash` wrapper over `exec.run` | `plugins/copilot-runtime.md` |
| Hook governance and policy interception | `hooks[]` on lifecycle + meta events (`tool.pre`, `completion.pre`, `delegation.pre`, `meta.register_tool`) | `plugins/copilot-runtime.md` |
| Context loading and mode switching | `condition` + turn-start hook that initializes runtime context keys | `plugins/copilot-runtime.md` |
| Sub-agent delegation | Delegation tool + `delegation.pre` policy hook + `delegation.post` telemetry hook | `plugins/copilot-runtime.md` |
| Long-running/background behavior | Background start/status/cancel tools + custom events (`custom.background.*`) | `plugins/copilot-runtime.md` |

## Gaps Found (Missing / Partial Primitives)

The reference is intentionally explicit about where AI Harness is not yet first-class enough to match Copilot CLI semantics directly.

1. No first-class `schedules` primitive in artifact schema (`tools`, `hooks`, `models` only).
2. No first-class `watchers`/file-trigger artifact primitive.
3. Background jobs are modeled cooperatively with cache + custom events; no durable task runtime contract (e.g., restart-safe queue, lifecycle API, guaranteed cancellation).
4. Trigger bindings beyond hook events (for external clocks/webhooks) are not yet schema-level artifacts.
5. Runtime-level named-tool override precedence is achievable by policy + naming, but not yet represented as an explicit conflict-resolution primitive.

## Follow-on Issues to Open from this Reference

- Add schema-level `schedules` support to artifact definitions.
- Add schema-level `watchers` support (filesystem/event triggers).
- Add first-class long-running runtime primitives (durable queue, status stream, cancellation).
- Add explicit trigger artifact type for cron/webhook/external events.
- Add explicit named-tool precedence policy (override/bind semantics).
