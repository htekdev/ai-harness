# Tool Artifact Schema

A **tool artifact** is a single Markdown file under `.harness/tools/` that
turns a Starlark function into a capability the model can call by name. This
page is the exhaustive reference for the artifact format: every supported
frontmatter field, the parsing rules, the validation surface, and the
runtime contract that backs each field.

For the conceptual overview — *why* tools are files — see
[Concepts → Tools](../concepts/tools.md). For a walkthrough that builds one
end-to-end, see the [Writing a Tool](../guides/writing-a-tool.md) guide.

> **Versioning note.** Every field documented on this page is part of the
> stable artifact configuration surface under SemVer. Fields explicitly
> labeled *experimental* may change; new optional fields may be added in
> minor releases without breaking existing files.

## File shape

```markdown
---
parameters:
  command: { type: string, required: true }
  timeout_ms: { type: number, required: false }
script: |
  def run(args):
      return exec.run("bash", ["-lc", args["command"]], 15000)
timeout_ms: 30000
---

# run_command

Run a shell command. The body becomes the tool's description: it is
shipped to the model in every `tools[]` slot and is what the model reads
when deciding whether to call this tool.
```

Rules enforced by the loader (`config.ParseToolMarkdown` in
`config/markdown.go`):

1. The file **must** start with a `---` delimiter line. Files without
   frontmatter are rejected by the parser.
2. The frontmatter **must** be closed by a second `---` on its own line.
3. The **filename is the tool name.** A file at `.harness/tools/run_command.md`
   registers a tool named `run_command`. The name is taken verbatim from
   the file stem — there is no `name:` field in frontmatter.
4. The body after the closing delimiter is the tool's **description**. The
   description is what the model sees when reasoning about which tool to
   call. If the body is empty, the description falls back to the tool name.
5. The frontmatter is parsed as YAML. Unknown top-level keys are **ignored
   silently** — typos in field names produce no error. Use `harness validate`
   to confirm the runtime sees the schema you expect.
6. Fenced code blocks inside the body are **never** extracted as Starlark.
   The only executable surface is `script:` in frontmatter; everything in
   the body is model-visible prose.

The same fields can also be authored inline in `harness.md` under the
top-level `tools:` list, or inside a Shape A
[bundle artifact](../examples/governed-agent.md#bundles) under
`.harness/{plugins,builtins,overrides}/`. The schema is identical in all
three cases.

## Top-level fields

| Field         | Type                                         | Default      | Required |
|---------------|----------------------------------------------|--------------|----------|
| `parameters`  | `map<string,` [Parameter](#parameter)`>`     | _empty map_  | no       |
| `script`      | string (Starlark source)                     | _empty_      | no\*     |
| `timeout_ms`  | integer                                      | `0` (no cap) | no       |
| `async`       | boolean                                      | `false`      | no\*\*   |

\* A tool with no `script` parses successfully and can be registered, but
the agent has no implementation to call. This is useful for declaring a
tool whose handler is supplied later in code (Go-side `tools.Register`)
while still using artifact-driven discovery, parameter validation, and
hook gating.

\*\* `async` is **reserved**: it is parsed by `ParseToolMarkdown` but is
not yet propagated through `ToolConfig`, so setting it has no runtime
effect today. The field is documented here so authors can adopt the
forward-compatible shape; it will activate alongside the long-running
primitives work tracked in
[issue #104](https://github.com/htekdev/ai-harness/issues/104).

There is no `name:` or `description:` field in tool frontmatter — those
are derived from the filename and the Markdown body, respectively.

---

## `parameters`

The contract the model sees. Every key is the parameter name; every
value is a [Parameter](#parameter) entry. The harness validates and
coerces arguments against this schema **before** `script` is invoked, so
tool code never has to defend against missing required fields or type
mismatches.

```yaml
parameters:
  path:
    type: string
    description: Workspace-relative path to read.
    required: true
  encoding:
    type: string
    description: Output encoding. Defaults to utf-8 when omitted.
    required: false
  max_bytes:
    type: number
    required: false
```

Parameters appear in the JSON Schema sent to the model in the order they
are listed in YAML. Required fields are aggregated into the schema's
`required` array automatically.

> **Tip.** YAML's flow form (`{ type: string, required: true }`) is the
> compact convention used across the example tools. Block form (the
> three-line shape above) is functionally identical and reads better
> when descriptions are long.

### Parameter

| Sub-field      | Type    | Required | Notes                                         |
|----------------|---------|----------|-----------------------------------------------|
| `type`         | string  | yes      | One of `string`, `number`, `boolean`, `object`, `array`. |
| `description`  | string  | no       | Shown to the model. Be explicit about units, formats, and bounds. |
| `required`     | boolean | no       | Defaults to `false`. Required parameters are enforced before `run` is called. |

The `type` values map to JSON Schema primitives. Nested object/array
shapes (`properties`, `items`) are not declarable from the artifact
frontmatter today — for richer schemas, register the tool in Go via
`tools.Definition` where the full `ParameterSchema` graph is available.

#### Type semantics

| `type`    | Accepted JSON              | Surfaces in `args` as |
|-----------|----------------------------|------------------------|
| `string`  | JSON string                | Starlark `string`      |
| `number`  | JSON number (int or float) | Starlark `int` or `float` |
| `boolean` | JSON `true` / `false`      | Starlark `bool`        |
| `object`  | JSON object                | Starlark `dict`        |
| `array`   | JSON array                 | Starlark `list`        |

#### Validation rules

- A required parameter that is missing is rejected before `script`
  executes; the tool returns an error result without firing `tool.pre`
  hooks beyond the validation point.
- Extra keys the model sends that are not declared in `parameters` are
  passed through to `args` as-is. Use a `tool.pre` hook to strip them if
  your policy requires strict mode.
- Type coercion is intentionally narrow: a JSON string is **not**
  silently parsed into a number. Authors should prefer `type: string`
  for fields that may legitimately arrive as either form (file sizes,
  IDs) and parse inside `run`.

---

## `script`

The tool's implementation, written in Starlark. The script must define a
top-level function:

```python
def run(args):
    # ...
    return {"ok": True}
```

The harness invokes `run(args)` per call, where `args` is a Starlark
`dict` populated from the model's JSON arguments after schema validation.
The return value is converted back to JSON and shipped to the model as a
tool result.

### Starlark dialect

The dialect is intentionally minimal:

- No `import` statements. All capabilities arrive through harness-owned
  built-ins (see [Starlark Built-ins](./starlark-builtins.md)).
- No I/O at the language level. `print` is captured into harness logs;
  there is no `open`, no `os`, no `sys`.
- No recursion. The default Starlark configuration disables it; rewrite
  recursive shapes as iteration.
- No global mutable state across calls. Each invocation gets a fresh
  module scope.
- No `isinstance(...)`. Use `type(value) == "string"` etc.

### Built-ins available inside `run`

The exhaustive matrix lives in [Starlark Built-ins](./starlark-builtins.md);
the headline categories are:

| Built-in          | Purpose                                                     |
|-------------------|-------------------------------------------------------------|
| `exec.run`        | Execute a process under the active command sandbox.         |
| `fs.read` / `fs.write` / `fs.exists` / `fs.stat` | Filesystem ops, jailed to the workspace. |
| `http.get` / `http.post` | HTTP calls, gated by `network.allowed_domains`.      |
| `json.encode` / `json.decode` | Structured payload helpers.                     |
| `re.match` / `re.search` / `re.findall` | Bounded regex.                       |
| `string.truncate` | Bounded string helpers.                                     |
| `cache.get` / `cache.set` | Per-run KV cache.                                   |
| `delegate(...)`   | Hand control to a sub-agent.                                |
| `meta.tool_register` / `meta.hook_register` | Self-augmenting agents (gated). |
| `log.info` / `log.warn` | Structured logs that flow into hooks.                 |
| `block(reason)` / `allow()` | Convenience helpers for hook returns (in hook scripts). |

### Return shape

`run` should return a Starlark `dict` (which becomes a JSON object), a
`list`, a string, or a number. The harness JSON-encodes the value before
posting it back to the model. Errors should be returned as an explicit
error shape — the convention across the built-in tools is:

```python
def run(args):
    if not args.get("command"):
        return {"error": "command is required"}
    ...
```

Raising a Starlark exception (`fail(...)`) also surfaces as an error
tool result, but the explicit dict form is preferred because it lets
`tool.post` hooks introspect the error consistently.

---

## `timeout_ms`

A wall-clock cap, in milliseconds, on a single `run` invocation. The
harness enforces the cap by cancelling the Starlark thread and any
sandboxed I/O it owns when the budget is exhausted; the model sees a
timed-out tool result rather than a hang.

| Value      | Behavior                                                  |
|------------|-----------------------------------------------------------|
| omitted    | No tool-level cap (other than the global agent budget).   |
| `0`        | Same as omitted — explicitly opt out of the cap.          |
| positive   | Hard cap in milliseconds.                                 |
| negative   | Rejected by `validate()`: `tool %q timeout_ms must be >= 0`. |

`timeout_ms` is also exposed as a *parameter* on most built-in tools
(`run_command`, `http_get`, ...) so the model can request a tighter
deadline per call. Those parameter-level caps are independent of the
artifact-level `timeout_ms`: the tool author decides whether to use the
artifact cap, the per-call cap, or `min(both)`.

---

## `async` *(reserved)*

`async: true` declares that the tool is safe to run on the harness's
async work queue rather than blocking the agent loop. The field is
parsed today but not yet wired through to the executor; setting it has
no runtime effect.

When activated (tracked in
[issue #104](https://github.com/htekdev/ai-harness/issues/104) and the
long-running primitives spec), tools marked `async: true` will:

- Return an opaque `task_id` to the model immediately.
- Continue executing under the same sandbox.
- Surface results via a `task.poll` / `task.await` built-in or via the
  `tool.post` hook event when complete.

Authoring a tool with `async: true` today is forward-compatible: the
field is preserved through parsing and ignored at runtime.

---

## Validation surface

Tool artifacts are validated by `Config.Validate()` at load time. The
checks that fire on the tool slice are:

- `tools[%d].name cannot be empty` — guards the synthesized name; only
  trips for malformed bundles, never for `.harness/tools/*.md` files
  (the filename is always non-empty).
- `tool %q is defined more than once` — duplicate names across all
  sources (single-tool files, inline `harness.md` `tools:`, bundles).
- `tool %q timeout_ms must be >= 0` — rejects negative caps; `0` is
  always allowed and means "no cap".

Invalid frontmatter YAML, missing `---` delimiters, or a non-map
`parameters` block surface as parse errors before validation runs:

```
parse tool run_command.md: yaml: line 4: ...
```

`harness validate` exits non-zero on any of the above.

---

## Runtime lifecycle

Per invocation, the harness runs the same six-step pipeline for every
tool — there is no fast path that skips hooks or validation:

1. **Resolve.** Look up the tool by name; reject if not registered.
2. **Validate.** Coerce and check arguments against `parameters`.
3. **`tool.pre` hooks.** Fire, in priority order, every hook subscribed
   to `tool.pre` whose `when:` predicate matches. Hooks may inspect
   args, modify them (`{"action": "modify", "payload": {...}}`), or
   veto the call (`{"action": "block", "reason": "..."}`).
4. **Execute.** Run `script`'s `run(args)` under the Starlark sandbox
   with the active `timeout_ms`.
5. **`tool.post` hooks.** Fire, in priority order, every hook subscribed
   to `tool.post`. Hooks see the final return value and can amend or
   redact it.
6. **Return.** Serialize the (possibly hook-modified) result back to the
   model.

Hook payloads are documented in [Hook Artifact Schema](./hook-artifact.md).
Tools are oblivious to whether hooks exist — the contract is one-way.

---

## Authoring conventions

These are not enforced by the loader, but they are the conventions used
by every built-in and example tool in the repository. Following them
makes a tool easier to govern with hooks and easier for the model to
pick.

### Verb-first, snake_case names

`run_command`, `read_file`, `search_code`, `send_telegram`. The model
parses these like English; nouns and CamelCase confuse routing.

### Wrap raw built-ins under a domain name

Don't expose `exec` directly. Wrap it as `run_command`, `git_diff`,
`pytest_run`. Each wrapper:

- Gives the model a **named** capability it can be governed against.
- Gives `tool.pre`/`tool.post` hooks a **stable hook point**.
- Gives reviewers a **stable file** to audit.

The `prefer_named_tools` hook in the
[Governed Agent example](../examples/governed-agent.md) enforces exactly
this distinction at runtime.

### Keep `parameters` flat

The model is much better at picking flat schemas than nested ones. If a
tool needs structured input, prefer multiple flat fields with shared
prefixes (`branch_name`, `branch_base`, `branch_force`) over a single
`branch: { name, base, force }` object.

### Bound every output

Truncate large strings explicitly with `string.truncate` before
returning them. The harness enforces a global tool-output cap, but
returning early with a 4–8 KB summary is almost always a better model
experience than a 200 KB stdout dump.

### Treat the body as model-visible context

The Markdown body of a tool artifact is loaded into the active context
and concatenated with the system prompt. Use it to explain *when* to
reach for the tool and *when not to* — the model reads it on every turn.

---

## See also

- [Concepts → Tools](../concepts/tools.md) — the conceptual overview.
- [Guides → Writing a Tool](../guides/writing-a-tool.md) — step-by-step
  walkthrough of building a tool from scratch.
- [Hook Artifact Schema](./hook-artifact.md) — sister reference for the
  hook lifecycle that every tool flows through.
- [Starlark Built-ins](./starlark-builtins.md) — exhaustive built-in
  reference for `script:` authors.
- [`harness.md` Frontmatter](./harness-md.md) — the inline `tools:` list
  uses this same schema.
- [Examples → Governed Agent](../examples/governed-agent.md) — flagship
  example where every concept on this page is in production use.
