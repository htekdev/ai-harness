# Starlark Built-ins

This page is the exhaustive catalog of every Starlark built-in registered
by AI Harness. These are the only side-effecting primitives a tool's
`run(args)` function or a hook's `handle(event, payload)` function may
call directly. Anything not listed here is not in the sandbox.

For the conceptual overview of how Starlark fits into the runtime, see
[Concepts → Tools](../concepts/tools.md) and
[Concepts → Hooks](../concepts/hooks.md). For walkthrough-style
examples, see [Writing a Tool](../guides/writing-a-tool.md) and
[Writing a Hook](../guides/writing-a-hook.md).

> **Versioning note.** Built-in names, signatures, and return shapes
> documented on this page are part of the stable scripting surface
> under SemVer. New built-ins may be added in minor releases without
> breaking existing scripts. Built-ins explicitly labeled
> *experimental* may change.

## Where built-ins are registered

All built-ins are wired into the global Starlark string-dict by
`scripting.Engine.makeBuiltins` in `scripting/engine.go`. The same
dict is shared by tools and hooks — both `CompileToolScript` and
`CompileHookScript` execute against the same global namespace.

The `meta` module is registered conditionally: it appears only when
the engine is constructed with a non-nil meta backend (production
runs always wire it; bare unit-test engines may omit it).

## Top-level value summary

Tools and hooks see the following top-level identifiers:

| Identifier   | Kind      | Purpose                                                  |
| ------------ | --------- | -------------------------------------------------------- |
| `time`       | module    | Wall-clock                                               |
| `json`       | module    | JSON encode/decode                                       |
| `math`       | module    | Numeric helpers                                          |
| `os`         | module    | Process / host inspection                                |
| `url`        | module    | URL parsing & encoding                                   |
| `uuid`       | module    | UUID generation                                          |
| `http`       | module    | Outbound HTTP (sandboxed)                                |
| `re`         | module    | Regex                                                    |
| `hash`       | module    | Non-cryptographic & cryptographic hashes                 |
| `base64`     | module    | Base64 encode/decode                                     |
| `crypto`     | module    | HMAC primitives                                          |
| `string`     | module    | Extra string helpers                                     |
| `template`   | module    | Lightweight string templating                            |
| `validate`   | module    | Format validators                                        |
| `set`        | module    | Set construction & operations                            |
| `cache`      | module    | Process-scoped key/value cache                           |
| `metrics`    | module    | Counter metrics                                          |
| `fs`         | module    | Filesystem (sandboxed)                                   |
| `ctx`        | module    | Turn-scoped key/value state                              |
| `exec`       | module    | Subprocess execution (sandboxed)                         |
| `meta`       | module    | Runtime extensibility (register/list/call) — conditional |
| `env`        | builtin   | Read environment variable                                |
| `log`        | builtin   | Diagnostic logging to stderr                             |
| `assert`     | builtin   | Hard precondition check                                  |
| `allow`      | builtin   | Hook decision: continue                                  |
| `block`      | builtin   | Hook decision: block                                     |
| `modify`     | builtin   | Hook decision: replace payload                           |
| `emit`       | builtin   | Emit a custom event into the runtime stream              |
| `random`     | builtin   | Random integer in `[min, max]`                           |
| `sleep`      | builtin   | Cancellable sleep                                        |

The Starlark standard library (`len`, `range`, `enumerate`, `dict`,
`list`, `tuple`, `set` literals, comprehensions, `type(v)`, etc.) is
also available — but `isinstance` is **not** part of Starlark; use
`type(v) == "string"` instead.

## Decision built-ins (hooks)

Hooks must return a decision. The three decision constructors below
build the canonical `{action, ...}` value that the runtime understands.
Returning a bare dict with the same shape is also accepted, but
prefer the constructors — they are typed and validated at call time.

### `allow()`

```python
allow()
```

Returns the *continue* decision. The runtime proceeds with the
original payload unmodified. Equivalent to returning
`{"action": "allow"}` from `handle`.

### `block(reason)`

```python
block(reason)            # positional
block(reason="...")      # keyword
```

Returns the *block* decision. The runtime aborts the gated operation
(tool call, completion, delegation, etc.) and surfaces `reason` as
the block reason. Equivalent to returning
`{"action": "block", "reason": "..."}`.

### `modify(payload)`

```python
modify(payload)
modify(payload={...})
```

Returns the *modify* decision. The runtime substitutes `payload` for
the original event payload. Shape and field constraints are
event-specific — see [Hook Artifact Schema](./hook-artifact.md) for
the canonical payload shape per event. Equivalent to returning
`{"action": "modify", "payload": {...}}`.

> Decision built-ins are also callable from tools, but the runtime
> ignores their return value outside a hook context. Treat them as
> hook-only.

## Diagnostic built-ins

### `log(msg)`

Writes `[script] <msg>\n` to the harness's stderr stream. Returns
`None`. Used for ad-hoc diagnostics; for structured/observable
output prefer `emit()` or `metrics.incr()`, which are surfaced through
the OTel pipeline (see [Guides → Observability](../guides/observability.md)).

### `assert(condition, msg?)`

```python
assert(condition)
assert(condition, "message")
```

If `condition` is falsy, fails the script with an error message.
Mirrors the runtime's tool-precondition checks; useful in tools that
need to defend against malformed `args`.

### `emit(name, payload)`

```python
emit("custom.policy_decision", {"rule": "deny_secrets", "matched": True})
```

Emits a custom event onto the runtime event stream. The `name` must
be a string; the `payload` must be JSON-encodable. Used to surface
policy decisions, audit records, or business events. Custom events
are visible to hooks subscribed to `custom.<name>` and are exported
as OTel events on the active span.

### `env(key)`

Reads `os.Getenv(key)` from the harness process and returns it as a
Starlark string. Returns the empty string when unset — there is no
`default` parameter; supply your own with `or`:

```python
endpoint = env("HARNESS_OTEL_ENDPOINT") or "http://localhost:4318"
```

### `random(min, max)`

```python
random(min=1, max=100)
```

Returns a uniformly-random integer in the closed interval
`[min, max]`. Both arguments are required; `min` must be strictly
less than `max`.

### `sleep(seconds)`

```python
sleep(0.25)
```

Sleeps for `seconds` (float). Cancellable: respects the harness's
turn context, so a tool/hook that is cancelled (timeout, user abort)
exits the sleep promptly with an error rather than blocking the
turn.

## `time`

| Call         | Returns                                                      |
| ------------ | ------------------------------------------------------------ |
| `time.now()` | RFC3339 nanosecond timestamp string of the current wall-clock |

## `json`

| Call               | Returns                                                                  |
| ------------------ | ------------------------------------------------------------------------ |
| `json.encode(val)` | String. Encodes a Starlark value to canonical JSON. Lists/dicts/scalars. |
| `json.decode(s)`   | Starlark value. Parses a JSON string into Starlark dict/list/scalars.    |

`json.encode` is the canonical serialization path for tool return
values: a tool's `run(args)` should return a JSON string, typically
produced via `json.encode({...})`.

## `math`

| Call                | Returns                                       |
| ------------------- | --------------------------------------------- |
| `math.abs(x)`       | Absolute value (preserves int/float).         |
| `math.ceil(x)`      | Ceiling as int.                               |
| `math.floor(x)`     | Floor as int.                                 |
| `math.max(a, b)`    | Larger of two values.                         |
| `math.min(a, b)`    | Smaller of two values.                        |

## `os`

Read-only host inspection. There is no `os.exit` or `os.setenv` —
mutation of the harness process is intentionally not exposed.

| Call            | Returns                                                |
| --------------- | ------------------------------------------------------ |
| `os.args()`     | List of process arguments at harness startup.          |
| `os.cwd()`      | Working directory of the harness process.              |
| `os.hostname()` | Hostname.                                              |
| `os.platform()` | `"linux"`, `"darwin"`, `"windows"`, etc. (`runtime.GOOS`). |

## `url`

| Call                | Returns                                                                                          |
| ------------------- | ------------------------------------------------------------------------------------------------ |
| `url.encode(s)`     | URL-percent-encoded string.                                                                      |
| `url.parse(rawURL)` | Dict with keys `scheme`, `host`, `port`, `path`, `query`, `fragment`, `user`. Values are strings. |

## `uuid`

| Call        | Returns                          |
| ----------- | -------------------------------- |
| `uuid.v4()` | RFC 4122 v4 UUID string.         |
| `uuid.v7()` | Time-ordered v7 UUID string.     |

## `http`

Outbound HTTP. Subject to the harness's
[network sandbox](./harness-md.md#network) — when
`network.allowed_domains` is non-empty, every request's hostname is
matched against the allowlist before the socket is opened. When
`network` is omitted (or `allowed_domains` is empty), requests are
allowed to any host for backward compatibility with pre-5.5 configs.
See the [Network Sandboxing](../guides/network-sandbox.md) guide for
the full posture, matching rules, and migration recipe.

| Call                                                       | Returns                                                                                                  |
| ---------------------------------------------------------- | -------------------------------------------------------------------------------------------------------- |
| `http.get(url, headers=None, timeout_seconds=None)`        | Dict `{status: int, headers: dict, body: string}`. `headers` keys are lowercased.                        |
| `http.post(url, body=None, headers=None, timeout_seconds=None)` | Dict `{status: int, headers: dict, body: string}`. `body` may be a string or a JSON-encodable value. |

`timeout_seconds` defaults to a conservative per-request limit
(currently 30s); set explicitly for long-running endpoints. Errors
(DNS, sandbox rejection, TLS, timeout) raise as Starlark errors —
guard with `try`-style flow by structuring tool logic to return
`{"error": ...}` on caller-visible failures.

> Network sandbox rejections are reported with the exact denied
> hostname, which is useful for diagnosing missing
> `network.allowed_domains` entries during development.

## `re`

| Call                         | Returns                                                                                                |
| ---------------------------- | ------------------------------------------------------------------------------------------------------ |
| `re.match(pattern, s)`       | List of match groups (`[full, group1, group2, ...]`) or `None` if no match. Anchored at start.         |
| `re.find_all(pattern, s)`    | List of all non-overlapping matches. Each match is itself a list of groups.                            |
| `re.replace(pattern, repl, s)` | String with all matches of `pattern` replaced by `repl`. Supports `$1`, `$2` backreferences.         |

Regex syntax is Go's `regexp` (RE2) — no backreferences in *patterns*,
no lookaround.

## `hash`

| Call                | Returns                          |
| ------------------- | -------------------------------- |
| `hash.md5(s)`       | Hex-encoded MD5.                 |
| `hash.sha1(s)`      | Hex-encoded SHA-1.               |
| `hash.sha256(s)`    | Hex-encoded SHA-256.             |
| `hash.sha512(s)`    | Hex-encoded SHA-512.             |

> MD5/SHA-1 are exposed for compatibility (e.g. ETag, file fingerprints).
> Do **not** use them for authentication or signatures — use
> `crypto.hmac_sha256` instead.

## `base64`

| Call                | Returns                                                                          |
| ------------------- | -------------------------------------------------------------------------------- |
| `base64.encode(s)`  | Standard base64-encoded string of the raw bytes of `s`.                          |
| `base64.decode(s)`  | Decoded string. Errors on invalid base64.                                        |

## `crypto`

| Call                              | Returns                                  |
| --------------------------------- | ---------------------------------------- |
| `crypto.hmac_sha256(key, msg)`    | Hex-encoded HMAC-SHA-256 of `msg` with `key`. |
| `crypto.hmac_sha512(key, msg)`    | Hex-encoded HMAC-SHA-512 of `msg` with `key`. |

## `string`

Starlark's string type already exposes `.upper()`, `.lower()`,
`.strip()`, `.split()`, `.startswith()`, `.endswith()`, etc. as
methods. The `string` module adds a small set of harness-specific
helpers, mostly for fixed-width formatting and bounded log lines.

| Call                                  | Returns                                          |
| ------------------------------------- | ------------------------------------------------ |
| `string.upper(s)`                     | Upper-cased copy.                                |
| `string.lower(s)`                     | Lower-cased copy.                                |
| `string.trim(s)`                      | Whitespace stripped both ends.                   |
| `string.split(s, sep)`                | List of substrings.                              |
| `string.join(parts, sep)`             | Joined string.                                   |
| `string.truncate(s, n, ellipsis="…")` | At most `n` characters, with ellipsis appended if truncated. |
| `string.pad_left(s, width, char=" ")` | Right-aligned padded string.                     |
| `string.pad_right(s, width, char=" ")`| Left-aligned padded string.                      |

## `template`

| Call                       | Returns                                                              |
| -------------------------- | -------------------------------------------------------------------- |
| `template.render(tmpl, vars)` | String. Renders `tmpl` (Go `text/template` syntax) with `vars` dict. |

Use for lightweight string assembly. For prompt assembly, prefer
context artifacts (`harness_context/v1alpha1`) — templates here are
for tool/hook output, not for system-prompt construction.

## `validate`

Pure-string validators. Each returns a bool.

| Call                  | Returns | Validates                                          |
| --------------------- | ------- | -------------------------------------------------- |
| `validate.email(s)`   | bool    | RFC 5322 mail address (mailbox form).              |
| `validate.url(s)`     | bool    | Absolute URL with scheme + host.                   |
| `validate.json(s)`    | bool    | Parses as JSON without error.                      |

## `set`

Process-scoped set values. `set.new` returns an opaque set value;
the rest of the API operates on those values.

| Call                       | Returns / effect                                              |
| -------------------------- | ------------------------------------------------------------- |
| `set.new(items=[])`        | New set value seeded with `items`.                            |
| `set.contains(s, item)`    | bool.                                                         |
| `set.size(s)`              | int.                                                          |
| `set.values(s)`            | List of items (insertion-ordered).                            |
| `set.union(a, b)`          | New set.                                                      |
| `set.intersect(a, b)`      | New set.                                                      |
| `set.diff(a, b)`           | New set: items in `a` not in `b`.                             |

## `cache`

Process-scoped key/value cache, **not** persisted across runs. Values
must be JSON-encodable. Cleared on harness restart.

| Call                                | Returns / effect                                         |
| ----------------------------------- | -------------------------------------------------------- |
| `cache.set(key, value)`             | Stores `value` under `key`. Returns `None`.              |
| `cache.get(key, default=None)`      | Returns the value or `default` if missing.               |
| `cache.has(key)`                    | bool.                                                    |
| `cache.delete(key)`                 | Removes the key. Returns `None`.                         |
| `cache.clear()`                     | Empties the cache. Returns `None`.                       |

For per-turn state (cleared between turns) use `ctx`. For
cross-process or durable storage, write a tool that talks to your
chosen backend.

## `metrics`

In-process counter metrics, exported through the OTel meter (see
[Guides → Observability](../guides/observability.md)). Names should
be dotted, lowercase, and stable.

| Call                            | Returns / effect                                     |
| ------------------------------- | ---------------------------------------------------- |
| `metrics.incr(name, delta=1)`   | Increments counter `name` by `delta`. Returns `None`. |
| `metrics.get(name)`             | Returns current counter value as int.                |
| `metrics.reset(name=None)`      | Resets one counter, or all if `name` omitted.        |
| `metrics.snapshot()`            | Dict of `{name: value}` for all counters.            |

## `fs`

Filesystem access, scoped to the harness's working directory.
Symlinks that escape the working directory are rejected. All paths
are normalized to OS-native separators internally; pass them as
forward-slash strings for portability.

| Call                                              | Returns / effect                                                     |
| ------------------------------------------------- | -------------------------------------------------------------------- |
| `fs.read(path)`                                   | File contents as string.                                             |
| `fs.write(path, content)`                         | Writes `content`, creating parent dirs as needed. Returns `None`.    |
| `fs.append(path, content)`                        | Appends to existing or new file. Returns `None`.                     |
| `fs.exists(path)`                                 | bool.                                                                |
| `fs.remove(path)`                                 | Deletes a file. Returns `None`.                                      |
| `fs.mkdir(path)`                                  | Creates directory tree. Returns `None`.                              |
| `fs.list(path)`                                   | List of entry dicts `{name, is_dir, size, modified}`.                |
| `fs.stat(path)`                                   | Dict `{name, size, is_dir, modified}` or error if missing.           |
| `fs.glob(pattern)`                                | List of matching paths (Go `filepath.Match` semantics).              |
| `fs.copy(src, dst)`                               | Copies a file. Returns `None`.                                       |
| `fs.move(src, dst)`                               | Renames/moves. Returns `None`.                                       |
| `fs.diff(a, b)`                                   | Unified-diff string of `a` vs `b`.                                   |
| `fs.replace(path, old, new)`                      | Replaces the **first** occurrence of `old` with `new`. Errors if `old` is not unique. |
| `fs.replace_all(path, old, new)`                  | Replaces every occurrence.                                           |
| `fs.read_lines(path, start=1, end=None)`          | List of lines `[start, end]`, 1-indexed inclusive.                   |
| `fs.line_count(path)`                             | int.                                                                 |
| `fs.insert_at(path, line, content)`               | Inserts `content` before `line`. Returns `None`.                     |
| `fs.replace_lines(path, start, end, content)`     | Replaces lines `[start, end]` with `content`. Returns `None`.        |
| `fs.delete_lines(path, start, end)`               | Deletes lines `[start, end]`. Returns `None`.                        |
| `fs.find(path, pattern)`                          | List of `{line, text}` dicts of matches (regex).                     |

> **Hook convention.** Hooks should be effect-light: avoid `fs.write`
> / `fs.append` / `fs.remove` / `fs.move` / `fs.copy` /
> `fs.replace*` from inside `handle()`. Hooks fire on every gated
> operation; mutating disk on every turn is almost always a bug.
> Use a dedicated audit tool instead and call it from the hook via
> `meta.call_tool` if needed.

## `ctx`

Turn-scoped key/value state. Values live for the duration of a
single turn and are cleared at `turn.end`. Use this for hook → tool
→ hook coordination within one turn (e.g. to record a precondition
in `tool.pre` and consult it in `tool.post`).

| Call                                | Returns / effect                                  |
| ----------------------------------- | ------------------------------------------------- |
| `ctx.get(key, default=None)`        | Value or `default`.                               |
| `ctx.set(key, value)`               | Sets the key. Returns `None`.                     |
| `ctx.has(key)`                      | bool.                                             |
| `ctx.delete(key)`                   | Removes the key. Returns `None`.                  |
| `ctx.clear()`                       | Drops all turn state. Returns `None`.             |
| `ctx.snapshot()`                    | Dict of all current `{key: value}` pairs.         |
| `ctx.agent.done_flag()`             | bool completion flag set by `done` tooling.       |
| `ctx.agent.set_done_flag(summary="", claims=[])` | Marks done flag and stores completion metadata. |
| `ctx.agent.run_verification_chain()` | Dict `{ok, reason}` from the turn's verification state. |

## `exec`

Subprocess execution. Subject to the same network and filesystem
sandbox as the rest of the harness.

| Call                                                   | Returns                                                                                                            |
| ------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------ |
| `exec.run(cmd, args=[], stdin="", timeout_seconds=30, env=None, cwd=None)` | Dict `{stdout, stderr, exit_code, timed_out}`. Non-zero exit codes do **not** raise — inspect `exit_code`. |

> **Hook convention.** `exec.run` from inside a hook is almost always
> wrong: hooks fire frequently and synchronously. Use
> `command_guard`-style policy hooks to *gate* `exec.run` invocations
> from tools, not to perform them.

## `meta`

Runtime extensibility. Lets a tool or hook discover, register, or
invoke other tools — the foundation for sub-agents and dynamic
artifact composition.

The `meta` module is registered only when the engine has a meta
backend wired in. In production runs that is always the case; in
isolated tests it may be absent.

| Call                                                           | Effect                                                                                                                          |
| -------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------- |
| `meta.list_tools(pattern="")`                                  | Returns a list of tool descriptors (name + description). When `pattern` is set, restricts to matching names.                    |
| `meta.call_tool(name, args, timeout_seconds=None)`             | Invokes another registered tool by name with the given args dict. Returns the tool's JSON result string. Subject to `tools_policy`. |
| `meta.register_tool(name, description, parameters, script)`    | Registers a new tool at runtime. The new tool is visible to subsequent turns.                                                   |
| `meta.register_hook(name, event, script, when="", priority=20)`| Registers a hook at runtime.                                                                                                    |
| `meta.register_agent(name, ...)`                               | Registers a sub-agent definition. See [Concepts → Delegation](../concepts/delegation.md).                                       |

> Calls into `meta.call_tool` go through the same `tools_policy`
> evaluation as a model-issued tool call. A hook-issued
> `meta.call_tool` that is denied by policy returns the policy's
> denial message instead of raising — design accordingly.

## What is intentionally not exposed

- `print` is not a global — use `log` so output is namespaced.
- `os.setenv`, `os.exit` — mutation of the harness process is denied.
- Direct socket / TCP / UDP — outbound traffic must go through `http`.
- File handles / streaming I/O — reads return whole-file strings;
  for streamed work, write a Go-side tool.
- `fs` access outside the working directory or via symlinks that
  escape it.
- Goroutines / threads — Starlark scripts are single-threaded;
  parallelism is the runtime's job, not the script's.

## Authoring conventions

1. Keep tool/hook scripts pure where possible. Use `ctx` for
   intra-turn coordination and `cache` for cross-turn memoization;
   avoid `fs.write`/`exec.run` from hooks.
2. Always JSON-encode tool return values. The runtime expects a
   JSON string from `run(args)` — produce it via `json.encode`.
3. Prefer `metrics.incr` and `emit` over `log` for anything you want
   to query later. `log` is best-effort stderr.
4. When checking types, write `type(v) == "string"` — Starlark has
   no `isinstance`.
5. Treat decision built-ins as the canonical hook return path. Bare
   dicts work, but `allow()` / `block(reason)` / `modify(payload)`
   are typed and harder to misuse.
6. Keep names stable. `metrics.incr` names, `emit` event names, and
   `cache` keys form a public contract with dashboards and other
   artifacts.
