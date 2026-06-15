# Exit Policy

`exit_policy` controls when an agent turn is allowed to stop after the model
returns a completion with no tool calls.

## Modes

- `natural` (default): stop immediately on natural completion.
- `done_tool`: require the `done` tool signal before stopping.
- `hook`: defer stop/continue to `agent.stop` hooks.
- `hybrid`: require `done` and run `agent.stop` hooks.

## Stop hook

When enabled (`hook`/`hybrid`), the runtime dispatches `agent.stop` with the
candidate turn result. A blocking hook injects its reason as the next user
message and the turn loop continues until allowed or `max_iterations` is hit.

## Config

```yaml
exit_policy:
  mode: hook
  max_iterations: 50
  on_max_iterations: error
```

`max_iterations` remains a hard cap regardless of hook decisions.
