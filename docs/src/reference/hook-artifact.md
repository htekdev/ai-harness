# Hook Artifact Schema

A **hook artifact** is a single Markdown file under `.harness/hooks/` that
subscribes a Starlark handler to a lifecycle event and returns an
`allow` / `block` / `modify` decision. This page is the exhaustive
reference for the artifact format: every supported frontmatter field, the
event catalog, payload shapes, the decision contract, and the parsing and
validation rules that back each.

For the conceptual overview — *why* hooks are files — see
[Concepts → Hooks](../concepts/hooks.md). For a step-by-step walkthrough,
see the [Writing a Hook](../guides/writing-a-hook.md) guide.

> **Versioning note.** Every field documented on this page is part of the
> stable artifact configuration surface under SemVer. Events explicitly
> labeled *experimental* may change; new optional fields and new events
> may be added in minor releases without breaking existing files.

## File shape

```markdown
---
event: tool.pre
priority: 10
when: payload["name"] == "run_command"
script: |
  def handle(event, payload):
      cmd = payload.get("args", {}).get("command", "")
      if "rm -rf /" in cmd:
          return block("dangerous command pattern blocked")
      return allow()
---

# command_guard

Hard-blocks well-known destructive shell patterns. Body is documentation
only — it is **not** sent to the model.
```

Rules enforced by the loader (`config.ParseHookMarkdown` in
`config/markdown.go`):

1. The file **must** start with a `---` delimiter line. Files without
   frontmatter are rejected by the parser.
2. The frontmatter **must** be closed by a second `---` on its own line.
3. The **filename is the hook handler name.** A file at
   `.harness/hooks/command_guard.md` registers a hook whose `Handler` is
   `command_guard`. There is no `name:` or `handler:` field in
   frontmatter.
4. `event:` is **required**. A missing or empty `event:` field fails the
   parse with `hook %q: event field is required in frontmatter`.
5. The body after the closing delimiter is **documentation only**.
   Unlike tool artifacts, hook bodies are not surfaced to the model — the
   model never sees a hook by name. Treat the body as reviewer-visible
   prose: explain *why* the hook exists, what it protects against, and
   what failure looks like when it fires.
6. Frontmatter is parsed as YAML. Unknown top-level keys are **ignored
   silently** — typos in field names produce no error. Use `harness validate`
   to confirm the runtime sees the schema you expect.
7. Fenced code blocks inside the body are **never** extracted as
   Starlark. The only executable surface is `script:` in frontmatter.

The same fields can also be authored inline in `harness.md` under the
top-level `hooks:` list, or inside a Shape A
[bundle artifact](../examples/governed-agent.md#bundles) under
`.harness/{plugins,builtins,overrides}/`. The schema is identical in all
three cases.

## Top-level fields

| Field      | Type                       | Default | Required |
|------------|----------------------------|---------|----------|
| `event`    | string (see [Events](#events)) | _none_  | **yes**  |
| `script`   | string (Starlark source)   | _empty_ | no\*     |
| `when`     | string (Starlark expr)     | _empty_ (always match) | no |
| `priority` | integer                    | `0`     | no       |

\* A hook with no `script` parses and registers, but has no handler body
to dispatch — it is a no-op. This is occasionally useful as a
placeholder during development; for production, every hook should ship a
`script:`.

There is no `name:` or `handler:` field in hook frontmatter — the handler
name is derived from the filename.

---

## `event`

The lifecycle event the hook subscribes to. The harness validates the
event name at load time and rejects unknown values with
`hooks[%d].event %q is invalid`.

### Events

The full catalog supported by `hooks.IsValidEvent`:

| Event                     | Fires when                                                          | Typical payload (Starlark dict)                                              |
|---------------------------|---------------------------------------------------------------------|------------------------------------------------------------------------------|
| `session.start`           | A new agent session begins.                                         | `None` — informational only.                                                  |
| `session.end`             | The session terminates (clean or error).                            | `None` — informational only.                                                  |
| `turn.start`              | Before the model is called for a new turn.                          | The user message as a string.                                                 |
| `turn.end`                | After the model produces its turn output.                           | The turn result (text + tool calls).                                          |
| `tool.pre`                | After argument validation, before `run(args)`.                      | `{id, name, arguments}`. Use `payload["args"]` once decoded.                  |
| `tool.post`               | After `run(args)` returns.                                          | `{call_id, name, content, is_error, result}`.                                 |
| `completion.pre`          | Before the completion request is sent to the provider.              | Provider request object (model, messages, tools).                             |
| `completion.post`         | After the provider returns a completion response.                   | Provider response (choices, usage, finish_reason).                            |
| `delegation.pre`          | Before a sub-agent delegation starts.                               | `{agent, prompt, depth, ...}`.                                                |
| `delegation.post`         | After a sub-agent delegation completes.                             | `{agent, result, depth, ...}`.                                                |
| `delegation.post_verify`  | After `delegation.post` when the delegation declares `verify:`. Hooks may `block(reason)` to trigger a Ralph-loop retry up to `MaxVerifyRetries`. See [#103](https://github.com/htekdev/ai-harness/issues/103). | Same shape as `delegation.post` plus `attempt` count. |
| `error`                   | An unrecoverable error surfaces in the agent loop.                  | Error envelope.                                                              |

In addition, two **prefixes** are accepted as valid event names:

- `custom.*` — user-defined custom events. Anything matching
  `^custom\.[a-z0-9_]+$` validates and can be dispatched from a tool via
  `events.emit("custom.my_event", payload)`.
- `meta.*` — meta built-in events fired by self-augmenting agents
  (`meta.tool_register`, `meta.hook_register`, ...). See
  [Concepts → Governance](../concepts/governance.md#meta-events).

#### Canonical payload shapes (Starlark)

The Go runtime dispatches typed structs; the Starlark bridge flattens
them into plain dicts. The shapes hooks should code against:

```python
# tool.pre
{
    "id":        "call_abc123",        # provider-assigned call id
    "name":      "run_command",        # tool name
    "arguments": "{\"command\": ...}", # raw JSON string from the model
    "args":      {"command": "..."},   # decoded dict (populated by harness)
}

# tool.post
{
    "call_id":   "call_abc123",
    "name":      "run_command",
    "content":   "stdout: ...",         # JSON-encoded tool return value
    "is_error":  False,
    "result":    {"stdout": "...", "exit_code": 0},  # decoded dict
}

# turn.start
"the user message text"

# turn.end
{
    "text":       "final assistant message",
    "tool_calls": [{"name": "...", "args": {...}}, ...],
    "usage":      {"input_tokens": 1234, "output_tokens": 567},
}
```

> **Gotcha.** `payload["arguments"]` for `tool.pre` is the *raw JSON
> string* sent by the model; `payload["args"]` is the decoded dict. Use
> `args` for inspection — it is what the validated, type-coerced
> parameters look like.

---

## `script`

The hook's implementation, written in Starlark. The script must define a
top-level function:

```python
def handle(event, payload):
    # ...
    return allow()
```

> **Canonical entry point.** It is `handle(event, payload)` — **not**
> `def run(...)` (which is the tool entry point) and **not**
> `def main(...)`. A hook script that defines the wrong function name
> will load successfully but produce a runtime error on first dispatch.

### Decision constructors

Every `handle` invocation must return one of three decisions:

| Constructor                          | Meaning                                                       |
|--------------------------------------|---------------------------------------------------------------|
| `allow()`                            | Pass through. Equivalent to `{"action": "allow"}`.            |
| `block(reason)`                      | Reject. Short-circuits the chain. The `reason` string is surfaced to the agent as the tool error / turn rejection message. Equivalent to `{"action": "block", "reason": "..."}`. |
| `modify(new_payload)`                | Rewrite the payload in place; downstream hooks and the underlying operation see the new value. Equivalent to `{"action": "modify", "payload": {...}}`. |

A dict return is also accepted:

```python
return {"action": "block", "reason": "path traversal not allowed"}
```

Any other return (a string, a number, `None`) is treated as `allow()`
with a runtime warning.

### Composition rules

- Hooks for an event run in **priority order** (low to high).
- The **first `block` wins** — the chain short-circuits and subsequent
  hooks are skipped.
- `modify` rewrites the payload *in place* for downstream hooks and the
  underlying operation.
- `allow` is a no-op pass.

There is no "after-the-fact override" and no implicit rule that lets a
later hook silently undo an earlier `block`. The order is the rule.

### Built-ins available inside `handle`

The exhaustive matrix lives in [Starlark Built-ins](./starlark-builtins.md);
the categories hooks use most often:

| Built-in                                          | Purpose                                            |
|---------------------------------------------------|----------------------------------------------------|
| `allow()` / `block(reason)` / `modify(payload)`   | Decision constructors.                             |
| `metrics.incr(name)` / `metrics.set(name, value)` | Counters and gauges visible to `metrics.snapshot()`. |
| `log.info(msg)` / `log.warn(msg)`                 | Structured logs that flow into `turn.end` payloads. |
| `cache.get(key)` / `cache.set(key, value)`        | Per-run KV cache, shared with tools.               |
| `http.get(url)` / `http.post(url, body)`          | Outbound HTTP, gated by `network.allowed_domains`. |
| `json.encode` / `json.decode`                     | Structured payload helpers.                        |
| `re.match` / `re.search` / `re.findall`           | Bounded regex.                                     |
| `string.truncate`                                 | Bounded string helpers.                            |
| `type(value)`                                     | Type discrimination. **No `isinstance`** — use `type(v) == "string"`. |

Hooks deliberately do **not** receive `exec.run` or `fs.write`. Policy
code that can shell out is policy code an attacker can pivot through. If
a hook needs to mutate state, do it through a named tool the hook calls
explicitly — that call re-enters the lifecycle and inherits all the same
audit guarantees.

---

## `when`

A static Starlark expression evaluated against the payload **before**
`handle` is called. It is the cheap path for scoping a hook to a single
tool, model, or turn shape without paying the cost of executing the
body.

```yaml
# Scope to one tool
when: payload["name"] == "run_command"

# Scope to a set of tools
when: payload["name"] in ["read_file", "write_file", "edit_file"]

# Scope to errors only
when: payload["is_error"] == True

# Scope to large outputs
when: len(payload.get("content", "")) > 4000
```

The expression has full access to:

- `payload` — the same dict that `handle` will receive.
- `event` — the event name as a string.
- All built-in identifiers (`len`, `type`, `True`, `False`, `None`, ...).

`when` does **not** have access to `metrics`, `cache`, `http`, `fs`, or
`exec` — it is a pure predicate. Any side-effecting work belongs in
`handle`.

If `when` is empty or omitted, the hook matches every dispatch of the
subscribed event. If `when` raises an exception, the hook is treated as
non-matching for that dispatch and a warning is logged.

> **Gotcha.** Use bracket access (`payload["name"]`) inside `when`, not
> attribute access (`payload.name`) — the payload is a dict, not a struct.

---

## `priority`

An integer that determines execution order within an event. **Lower
numbers run first.** Hooks with equal priority run in registration
order, which is deterministic across loads (sorted by source path).

Conventional priority bands used across the example agents:

| Band      | Use                                                              |
|-----------|------------------------------------------------------------------|
| `1`–`9`   | Audit / observability — must see *every* dispatch.               |
| `10`–`19` | Hard policy — deny dangerous patterns, jail filesystem, etc.     |
| `20`–`29` | Soft policy — prefer-named-tools, rate limits, soft caps.        |
| `30`–`39` | Meta — guard the harness itself (block edits to `.harness/`).    |
| `40`+     | Trimming / shaping — completion window caps, output redaction.   |

These bands are conventions, not enforcement. Anything goes as long as
the ordering tells a coherent story when listed top-to-bottom — that
ordering *is* the policy.

If `priority` is omitted, it defaults to `0`, which makes the hook the
earliest in its event chain. Prefer setting an explicit priority for
every production hook.

---

## Validation surface

Hook artifacts are validated by `Config.Validate()` at load time. The
checks that fire on the hook slice are:

- `hooks[%d].event %q is invalid` — the `event:` value is not in the
  static catalog and does not match `custom.*` or `meta.*`.
- `hook %q: event field is required in frontmatter` — surfaces during
  parse (before `Validate()`) when `event:` is missing or empty.

Invalid frontmatter YAML, missing `---` delimiters, or a non-string
`script:` surface as parse errors:

```
parse hook command_guard.md: yaml: line 4: ...
```

`harness validate` exits non-zero on any of the above.

There is **no** schema-level check that a hook actually returns a valid
decision shape — a hook that returns `42` will load fine and warn at
dispatch time. The Starlark sandbox is intentionally permissive here so
hook authoring stays fast; rely on the [Writing a Hook](../guides/writing-a-hook.md)
guide's testing patterns to catch decision-shape bugs.

---

## Hook execution lifecycle

For any event, the harness runs this five-step pipeline:

1. **Filter.** Evaluate each hook's `when:` expression against the
   payload; drop the ones that don't match.
2. **Sort.** Order surviving hooks by `priority` ascending.
3. **Dispatch.** Call `handle(event, payload)` for each, in order, with
   a fresh Starlark module scope per call.
4. **Compose.** Apply `modify` rewrites in place for downstream hooks
   and the underlying operation; short-circuit on the first `block`;
   treat `allow` as pass-through.
5. **Return.** Hand the final decision and (possibly modified) payload
   back to the caller — the tool dispatcher, the turn loop, or whichever
   subsystem fired the event.

This pipeline is identical for every event. There is no privileged hook,
no built-in policy that runs outside the chain, and no way for a tool or
sub-agent to bypass it.

---

## Authoring conventions

These are not enforced by the loader, but they are the conventions used
by every built-in and example hook in the repository.

### One concern per hook

Resist packing two policies into one file. Two priority-10 files that
each block one pattern are easier to review, diff, and remove than one
file that blocks both — and the audit log reads more clearly.

### Always set an explicit `priority`

A hook with no `priority:` is a hook that will surprise the next person
who adds an audit at priority 1. Picking from the
[conventional bands](#priority) keeps the policy stack legible.

### Use `when:` to scope cheaply

Every `handle` call costs Starlark setup. If a hook only applies to one
tool, gate it with `when: payload["name"] == "..."` instead of branching
inside `handle`. The static gate is faster and the intent is visible at
a glance in the frontmatter.

### Return early, return explicit

```python
def handle(event, payload):
    if not _should_inspect(payload):
        return allow()
    reason = _scan(payload)
    if reason:
        return block(reason)
    return allow()
```

Every branch ends in an explicit decision. Hooks that fall off the end
of `handle` produce a runtime warning and pass through.

### Treat the body as reviewer documentation

Unlike tool artifacts, the Markdown body of a hook is **not** loaded
into the model's context. It is reviewer-visible documentation only.
Use it to explain what the hook protects against, what failure looks
like when it fires, and any operational notes (paired metrics, dashboard
panels, runbook links).

### Stack hooks instead of growing them

A hook stack that reads like English is its own documentation:

```
.harness/hooks/
├── audit_tool_pre.md          # priority 1   — count + log every call
├── audit_tool_post.md         # priority 1   — count + log every result
├── command_guard.md           # priority 10  — deny dangerous shell patterns
├── path_guard.md              # priority 10  — jail filesystem writes
├── prefer_named_tools.md      # priority 20  — reject raw exec.run
├── meta_tool_guard.md         # priority 30  — block tools editing .harness/
└── completion_window_guard.md # priority 40  — cap output size per turn
```

Each file is a 30-line Markdown artifact. The whole governance posture
is a `git log`.

---

## See also

- [Concepts → Hooks](../concepts/hooks.md) — the conceptual overview.
- [Guides → Writing a Hook](../guides/writing-a-hook.md) — step-by-step
  walkthrough of building a hook from scratch.
- [Tool Artifact Schema](./tool-artifact.md) — sister reference for the
  capabilities every hook regulates.
- [Starlark Built-ins](./starlark-builtins.md) — exhaustive built-in
  reference for `script:` authors.
- [`harness.md` Frontmatter](./harness-md.md) — the inline `hooks:` list
  uses this same schema.
- [Examples → Governed Agent](../examples/governed-agent.md) — flagship
  example where every concept on this page is in production use.
