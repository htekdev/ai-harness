# Control-Flow Hooks

`agent.stop` and `delegation.post` are the two built-in lifecycle hooks that
can redirect execution into a new sub-agent **without** imperative orchestration
in a tool body.

Use them when you want artifact-only chains like:

```text
Agent A -> delegate B -> on complete -> delegate C
```

They share one primitive: `delegate(request)`.

## Supported scopes

| Event | Scope | Fires when | May `delegate(...)` |
| --- | --- | --- | --- |
| `agent.stop` | Primary agent | Right before the agent accepts a final no-tool-call response and exits the turn loop | Yes |
| `delegation.post` | Sub-agent | Right after a delegated sub-agent completes and before the parent accepts the result | Yes |

Both events also still support `allow()`, `block(reason)`, and
`modify(payload)`.

## Correlation fields

Delegation payloads now carry stable IDs:

- `delegation.Request.id`
- `delegation.Request.parent_id`
- `delegation.Result.id`
- `delegation.Result.parent_id`

When tracing is active, the runtime uses the current OTel span ID where
possible. When it is not, the runtime falls back to generated IDs. The same
delegation keeps the same `id` across `delegation.pre`, `delegation.post_verify`,
and `delegation.post`.

## The `delegate(request)` decision

Inside a hook:

```python
def handle(event, payload):
    return delegate({
        "task": "Review the generated patch",
        "agent": "reviewer",
    })
```

Equivalent dict form:

```python
return {
    "action": "delegate",
    "request": {
        "task": "Review the generated patch",
        "agent": "reviewer",
    },
}
```

The request shape is the same as the built-in `delegate` tool input:
`task`, `agent`, `model`, `tools`, `hooks`, `system_prompt`, `verify`,
`max_verify_retries`.

## Side-by-side examples

### `agent.stop`: continue with a reviewer after the root agent says "done"

```yaml
---
event: agent.stop
priority: 50
script: |
  def handle(event, payload):
      if "ready for review" not in payload.get("response", "").lower():
          return allow()
      return delegate({
          "task": "Review the completed work and report any gaps.",
          "agent": "reviewer",
      })
---
```

### `delegation.post`: chain a second sub-agent after the first one completes

```yaml
---
event: delegation.post
priority: 50
script: |
  def handle(event, payload):
      if payload.get("response", "") == "":
          return block("delegate returned no response")
      return delegate({
          "task": "Verify the completed work against the live filesystem.",
          "agent": "verifier",
      })
---
```

## Safety rules

Hook-driven delegation is guarded by:

- the normal delegation depth limit
- a control-flow chain budget
- cycle detection for repeated hook-driven delegation requests

If a hook keeps redirecting forever, the runtime fails closed instead of
silently looping.

## Related

- [Hook Artifact Schema](./hook-artifact.md)
- [Starlark Built-ins](./starlark-builtins.md)
- [Delegation](../concepts/delegation.md)
