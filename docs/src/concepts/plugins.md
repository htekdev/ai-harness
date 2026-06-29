# Plugins

> A plugin is the slot where user-authored and third-party capability bundles
> enter the harness — tools, hooks, and prose context shipped together as a
> single reviewable artifact.

If [tools](./tools.md) are individual capabilities and
[hooks](./hooks.md) are individual policies, a plugin is the
**bundle** that ships both in the same file, scoped to a single purpose.
The `copilot-runtime` plugin
(`examples/reference/copilot-cli/plugins/copilot-runtime.md`), for example,
ships four tools (`bash`, `load_context_bundle`, `delegate_task`,
`background_start`), three hooks (`turn.start`, `tool.pre`,
`completion.pre`), and a markdown body that explains the runtime semantics —
all in one file that travels the same `git mv / git rm` lifecycle as
everything else in your harness.

## The six artifact types

Plugins are one of six typed artifact kinds in AI Harness:

| Type         | Priority | One-liner                                              |
|--------------|----------|--------------------------------------------------------|
| `override`   | 100      | Project-local escalations that supersede everything    |
| `harness`    | 80       | The root identity and policy for a project             |
| `compaction` | 70       | Context-window compaction policy and strategy pipeline |
| `builtin`    | 60       | Core capabilities shipped with the runtime             |
| `plugin`     | 40       | **User or third-party capability bundles** ← this page |
| `model`      | 20       | Provider and model onboarding definitions              |

Every artifact type uses the same YAML-frontmatter + Markdown-body file
format. The `type:` field in the frontmatter is what distinguishes them at
load time.

## What a plugin is

A plugin artifact has three jobs:

1. **Bundle capabilities** — define tools (`tools[]`) and hooks (`hooks[]`)
   that belong together as a logical unit.
2. **Contribute context** — the Markdown body is injected into the system
   prompt alongside other active artifacts. Use it to explain the domain
   the plugin covers, the conventions it assumes, or the mental model the
   model needs to use the tools well.
3. **Declare activation scope** — an optional `condition:` expression lets
   a plugin self-select based on turn context, so a `git-ops` plugin can
   activate only when the session has version control files in scope, or a
   `review-mode` plugin can activate when `ctx.get("mode") == "review"`.

Plugins are loaded from `.harness/plugins/*.md` (see [Loading](#loading)).
They are never loaded from subdirectories; the flat-file convention is
intentional.

## Anatomy of a plugin

A complete, real plugin from the `artifact/testdata/plugins/` directory:

```markdown
---
name: git-ops
type: plugin
version: "0.2.0"
description: Git operations plugin for version control workflows
author: htekdev
tags: [git, vcs, development]
depends_on: [core-tools]
tools:
  - name: git-status
    description: Show the working tree status
    parameters:
      path:
        type: string
        required: false
        description: Repository path (defaults to cwd)
  - name: git-diff
    description: Show changes between commits or working tree
    parameters:
      ref:
        type: string
        required: false
        description: Git ref to diff against
hooks:
  - event: onPostToolUse
    handler: advisory
    when: "tool.name == 'exec' and 'git push' in tool.args.command"
    reason: Consider using git-ops tools instead of raw git commands
---

# Git Operations Plugin

Provides safe, governed git operations that integrate with the harness
hook system for audit and policy enforcement.
```

Four things to notice:

- **`type: plugin` declares the kind.** Without it the loader rejects the
  file as malformed.
- **`description:` is required.** The validator returns an error for any
  plugin without a non-empty description. This is a deliberate design
  choice: every plugin that enters a harness must be able to say what it
  does in one sentence.
- **Tools and hooks coexist.** A plugin is a *bundle*, not a pure tool
  list. You can ship a hook that audits the tool in the same file as the
  tool itself.
- **The markdown body is context.** It is composed into the system prompt
  for every turn when the plugin is active. Keep it short, precise, and
  useful to the model.

## Required and optional fields

### Required

| Field         | Type     | Constraint                                      |
|---------------|----------|-------------------------------------------------|
| `name`        | string   | Lowercase, kebab-case (e.g. `git-ops`), ≥ 2 chars; must match `^[a-z][a-z0-9-]*[a-z0-9]$` |
| `type`        | string   | Must be `plugin`                                |
| `description` | string   | Non-empty; validated by the artifact validator  |

### Optional

| Field       | Type       | Purpose                                                  |
|-------------|------------|----------------------------------------------------------|
| `version`   | string     | Semver (e.g. `"1.0.0"`); recommended for published plugins |
| `author`    | string     | Attribution; surfaced by `harness plugins`               |
| `tags`      | []string   | Free-form labels for filtering and search                |
| `depends_on`| []string   | Names of other artifacts this plugin requires            |
| `condition` | string     | Starlark expression controlling per-turn activation      |
| `tools`     | []ToolDef  | Tool definitions (name, description, parameters, script) |
| `hooks`     | []HookDef  | Hook subscriptions (event, handler, script)              |

A plugin may define only tools, only hooks, only a markdown body (for pure
context injection), or any combination of the three. The validator requires
a description but places no constraint on the capability shape.

## Composition and priority

Plugin artifacts compose at **priority 40**. In the full priority stack:

```
override  (100)  ← always wins
harness   ( 80)  ← project identity
compaction( 70)  ← context-window policy
builtin   ( 60)  ← core capabilities
plugin    ( 40)  ← ← you are here
model     ( 20)  ← provider onboarding
```

This placement means:

- **Plugins cannot silently override builtins.** A tool named `bash` in a
  plugin does not replace the `bash` tool in a builtin at priority 60.
  The harness raises a registration conflict instead of silently
  substituting. This is intentional: the only way to override a builtin is
  to declare `type: override`, which is visible in code review and audited
  separately.
- **Multiple plugins compose cleanly.** All active plugins are merged into
  the same composed harness in registration order. Tool names must be
  unique across all loaded artifacts.
- **Priority is not per-file scheduling.** The priority number governs how
  conflicts between artifact types are resolved during composition, not
  the order in which hook handlers fire within an event. Hook execution
  order is controlled by the `priority:` field *inside* each hook
  definition.

## Conditional activation

The `condition:` field is a Starlark expression evaluated once per turn
against the current context. When it returns a falsy value the entire
plugin — its tools, hooks, and context — is excluded from that turn's
composed harness.

```yaml
condition: 'ctx.get("mode") == "review"'
```

The expression may read any key set on `ctx` by a previous hook or tool,
including keys injected by a `turn.start` hook. It may not have side
effects. Common patterns:

```yaml
# Activate only when a mode context key is set to a specific value
condition: 'ctx.get("mode") == "review"'

# Activate only when the runtime key is absent or matches
condition: 'not ctx.has("runtime") or ctx.get("runtime") == "copilot-cli"'

# Activate based on a feature flag context key
condition: 'ctx.get("feature_git_ops", "false") == "true"'
```

An absent or empty `condition:` means the plugin is always active.

## What plugins can ship

### Tools

A tool definition inside a plugin follows the same shape as a standalone
tool artifact, except it lives inside the `tools:` list instead of a
dedicated `.harness/tools/` file.

```yaml
tools:
  - name: git-commit
    description: "Stage all changes and create a signed commit"
    parameters:
      message:
        type: string
        required: true
        description: Commit message
      sign:
        type: boolean
        required: false
        description: GPG-sign the commit (default false)
    script: |
      def run(args):
          message = args.get("message", "")
          sign = args.get("sign", False)
          if not message:
              return {"error": "message is required"}
          flags = ["-S"] if sign else []
          result = exec.run("git", ["commit", "-am", message] + flags, 15000)
          return {
              "success": result.get("exit_code", 1) == 0,
              "stdout": result.get("stdout", ""),
              "exit_code": result.get("exit_code", 1),
          }
```

The same Starlark sandbox applies: `exec.run`, `fs.read`, `fs.write`,
`http.get`, `cache.get/set`, `log.info` — and nothing outside that set.
See the [Starlark Built-ins reference](../reference/starlark-builtins.md)
for the full catalog.

### Hooks

A hook definition inside a plugin follows the same shape as a standalone
hook artifact, except it lives inside the `hooks:` list.

```yaml
hooks:
  - event: tool.pre
    handler: git_push_guard
    priority: 15
    script: |
      def handle(event, payload):
          tool_name = payload.get("name", "")
          if tool_name not in ["git-push", "git-commit"]:
              return allow()
          remote = payload.get("arguments", {}).get("remote", "origin")
          if remote == "upstream":
              return block("direct push to upstream is not allowed; open a PR instead")
          return allow()
```

Hook definitions in plugins participate in the same priority-ordered chain
as standalone hook artifacts. A hook defined in a plugin at `priority: 15`
runs between a standalone hook at `priority: 10` and one at `priority: 20`.

For the full hook event catalog see
[Concepts → Hooks](./hooks.md#the-four-lifecycle-events).

### Context (markdown body)

Everything after the closing `---` of the frontmatter is the plugin's
**context** — Markdown that is composed into the agent's system prompt
when the plugin is active. This is the place to:

- Explain the domain the plugin covers.
- Document conventions the tools expect (e.g. "always pass an absolute
  path to `git-commit`").
- Give the model the mental model it needs to call the tools correctly.

The body is injected verbatim (as Markdown) after the harness identity
context and any active builtin context, but before any override context.

## Loading

Plugins are loaded by the harness from a conventionally named directory:

```
.harness/
  plugins/
    git-ops.md
    my-team-plugin.md
    …
```

The loader (`artifact.LoadTree`) scans `.harness/plugins/*.md` and parses
each file as an artifact. Files are parsed in filesystem order; use
`depends_on:` if you require a deterministic load ordering.

You can also drop plugins into a shared directory and point the harness
at it via configuration. See the CLI reference for `--plugin-dir`.

### Validation rules

Every plugin is validated at load time before registration. A plugin fails
validation if:

- `name` is empty, shorter than two characters, or fails the pattern
  `^[a-z][a-z0-9-]*[a-z0-9]$`.
- `type` is not `plugin`.
- `description` is empty or whitespace-only.
- Any tool name is duplicated within the same artifact.
- Any `(event, handler)` pair is duplicated within the same artifact.
- `version`, when present, does not match basic semver (`X.Y.Z` or
  `X.Y`).

Validation errors are surfaced immediately by `harness validate` and at
startup; the harness will not start with an invalid artifact.

### Registration order

Within a single harness, artifacts of the same type compose in the order
they were registered. For plugins, that is the order `LoadTree` encounters
files — alphabetical by filename within `.harness/plugins/`. If two plugins
define tools with the same name, the second registration raises a conflict
error rather than silently overriding the first.

## Naming conventions

Plugin names follow the same conventions as all artifact names:

- **Lowercase, kebab-case.** `git-ops`, `copilot-runtime`, `review-tools`.
- **Domain-first.** Name the capability area, not the implementation:
  `code-review` not `review-helper-v2-final`.
- **No `plugin-` prefix.** The type field already declares the kind; the
  name should be the logical identity, not the file category.
- **Stable.** Other artifacts reference this plugin by name in
  `depends_on:`; renaming it is a breaking change.

## Why plugins can't override builtins silently

The priority-40 ceiling is a deliberate governance decision.

Builtins (priority 60) are the capabilities the harness runtime considers
part of its own contract. If a plugin could silently override a builtin
tool, a third-party plugin added to `.harness/plugins/` could replace
`exec` with a version that exfiltrates every command. The override would
be invisible to anyone reading the `plugins/` directory.

By enforcing a hard priority gap, AI Harness makes the following guarantee:
*a tool or hook with a given name in a plugin coexists with, but does not
replace, any same-named builtin.* To intentionally supersede a builtin you
must declare `type: override`, which:

1. Lives in `.harness/overrides/`, not `plugins/` — reviewers see it in a
   dedicated directory.
2. Runs the stricter `override` validation rules.
3. Shows up as `override` in `harness plugins` output, not `plugin`, making
   the intent auditable.

## What to read next

- **Guide:** [Writing a Plugin](../guides/writing-a-plugin.md) walks through
  a plugin from blank file to a working, validated bundle.
- **[Tools](./tools.md)** — the individual capability primitive that plugins
  bundle.
- **[Hooks](./hooks.md)** — the policy and observability primitive, including
  the full event catalog and the `allow / block / modify` contract.
- **Reference:** [Starlark Built-ins](../reference/starlark-builtins.md) —
  every built-in available inside plugin tool and hook scripts.
