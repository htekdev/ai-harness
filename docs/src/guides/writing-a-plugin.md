# Writing a Plugin

> A hands-on tutorial. By the end of this guide you'll have written,
> validated, and tested a plugin that bundles two git tools, a governance
> hook, and a context block — and you'll understand the design decisions
> that make plugins the right unit of composition for capability bundles.

This guide assumes you finished the [Quickstart](../getting-started.md)
and have `harness` on your `PATH`. If not, do that first — it gets you to
a working binary and a provider token in five minutes.

We'll build a `code-review` plugin that a team could drop into any
repository. Along the way we'll cover every plugin feature:

- the required fields the validator enforces,
- tools and hooks inside a plugin,
- the markdown context body,
- conditional activation,
- `depends_on` for cross-artifact dependencies,
- and the naming rules that keep plugins composable.

## 1. Set up a workspace

```bash
mkdir -p my-agent && cd my-agent
harness init .
```

You should have:

```
my-agent/
├── harness.md
└── .harness/
    ├── tools/
    │   └── …
    └── hooks/
        └── …
```

Create the plugins directory — the loader only picks up files here:

```bash
mkdir -p .harness/plugins
```

## 2. Write the plugin

Create `.harness/plugins/code-review.md`:

```markdown
---
name: code-review
type: plugin
version: "0.1.0"
description: "Code review tools and enforcement hooks for pull request workflows"
author: "your-team"
tags: [code-review, pr, quality]
tools:
  - name: list-pr-files
    description: "List files changed in the current pull request"
    parameters:
      pr_number:
        type: number
        required: true
        description: Pull request number
    script: |
      def run(args):
          pr = args.get("pr_number")
          if not pr:
              return {"error": "pr_number is required"}
          result = exec.run("gh", ["pr", "diff", "--name-only", str(int(pr))], 15000)
          if result.get("exit_code", 1) != 0:
              return {"error": result.get("stderr", "gh failed")}
          files = [f for f in result.get("stdout", "").split("\n") if f.strip()]
          return {"success": True, "files": files, "count": len(files)}

  - name: post-review-comment
    description: "Post a review comment on the pull request"
    parameters:
      pr_number:
        type: number
        required: true
        description: Pull request number
      body:
        type: string
        required: true
        description: Comment body (Markdown supported)
    script: |
      def run(args):
          pr = args.get("pr_number")
          body = args.get("body", "")
          if not pr or not body:
              return {"error": "pr_number and body are required"}
          result = exec.run("gh", ["pr", "comment", str(int(pr)), "--body", body], 15000)
          return {
              "success": result.get("exit_code", 1) == 0,
              "exit_code": result.get("exit_code", 1),
          }

hooks:
  - event: tool.pre
    handler: guard-pr-comment-length
    priority: 10
    script: |
      def handle(event, payload):
          if payload.get("name") != "post-review-comment":
              return allow()
          body = payload.get("arguments", {}).get("body", "")
          if len(body) > 4000:
              return block("review comment exceeds 4000-character limit; summarize")
          return allow()
---

# Code Review Plugin

Provides pull request review tools for agents operating in code review
workflows. Use `list-pr-files` to enumerate the changed files before
deciding which sections to review, then use `post-review-comment` to
leave structured feedback.

The `guard-pr-comment-length` hook blocks comments over 4000 characters
before they reach the GitHub API — the model should summarize rather than
dump raw diffs into a comment.

## Prerequisites

Requires the `gh` CLI on the PATH and an authenticated session
(`gh auth status`).
```

Four things to notice before moving on:

- **`type: plugin` and `description:` are both required.** The harness
  validator rejects any plugin without a non-empty description — it must
  say what it does in one sentence.
- **`name:` is `code-review`, not `CodeReview` or `code_review`.** Plugin
  names must be lowercase kebab-case (pattern `^[a-z][a-z0-9-]*[a-z0-9]$`).
  Other artifacts reference this plugin by name in `depends_on:`; a
  well-formed name is a stability contract.
- **Tools and hooks coexist.** The `guard-pr-comment-length` hook governs
  `post-review-comment` in the same file. You don't need a separate hook
  artifact; bundle related policy with the capability it guards.
- **The markdown body is model context.** Everything after the closing
  `---` is composed into the system prompt when the plugin is active. Use
  it to explain when to reach for the tools and any prerequisites or
  conventions the model should know.

## 3. Validate before running

```bash
harness validate
```

Expected output:

```
✅ harness.md valid
   2 tools, 1 hook (plugin: code-review) (4 ms)
```

If `description:` is missing you'll see:

```
❌ artifact "code-review": plugin must have a description
```

If the name violates the pattern you'll see:

```
❌ artifact "code-review": name must be lowercase alphanumeric with hyphens (e.g. 'my-artifact')
```

Fix and re-run until the output is green. The validator catches all schema
and Starlark compile errors offline — no model calls, no token spend.

You can also list the plugin:

```bash
harness plugins
```

`code-review` should appear with its description, version, and the tools
and hooks it ships.

## 4. Run one turn

Ask the agent to use the new tools:

```bash
harness run "List the files changed in PR 42."
```

The model will call `list-pr-files` with `pr_number: 42`, receive the
structured file list, and report it. To watch the lifecycle:

```bash
harness run --stream "List the files changed in PR 42."
```

The streaming output shows parameter coercion, the tool call, and the
return value — the same trace a `tool.pre` hook sees.

Try triggering the length guard:

```bash
harness run "Post a review comment on PR 42 with this text: $(python3 -c "print('x' * 5000)")"
```

The hook fires, blocks the call before it reaches the GitHub API, and the
model receives a structured error with the reason. The `post-review-comment`
Starlark function never executed.

## 5. Add conditional activation

Suppose the plugin should only be active when the agent is in `review`
mode. Add `condition:` to the frontmatter:

```yaml
---
name: code-review
type: plugin
version: "0.1.0"
description: "Code review tools and enforcement hooks for pull request workflows"
condition: 'ctx.get("mode") == "review"'
…
---
```

Now the plugin's tools, hooks, and context are excluded from every turn
unless a hook (typically a `turn.start` hook) has called
`ctx.set("mode", "review")`. To test:

```bash
harness run "List the files changed in PR 42."
# → model won't find list-pr-files; it's not in the composed harness

harness run --context mode=review "List the files changed in PR 42."
# → plugin is active; model calls list-pr-files
```

Common condition patterns:

```yaml
# Activate only for a specific runtime environment
condition: 'ctx.get("runtime") == "copilot-cli"'

# Activate when a feature flag is set
condition: 'ctx.get("feature_code_review", "off") == "on"'

# Always active (empty or absent condition — the default)
# condition: ""
```

## 6. Add `depends_on`

If your plugin relies on a tool or hook shipped by another artifact, list
it in `depends_on:`:

```yaml
depends_on: [core-tools]
```

The loader validates that each listed artifact exists and is registered
before registering the plugin. This prevents silent failures where a tool
the plugin wraps isn't available.

## 7. Walk through a full reference plugin

The `examples/reference/copilot-cli/plugins/copilot-runtime.md` plugin is
the most complete reference in the repository. It ships four tools
(`bash`, `load_context_bundle`, `delegate_task`, `background_start`) and
three hooks (`turn.start`, `tool.pre`, `completion.pre`) with a real
conditional:

```yaml
condition: 'not ctx.has("runtime") or ctx.get("runtime") == "copilot-cli"'
```

That condition reads: *"activate unless a runtime key is already set to
something other than `copilot-cli`."* It lets multiple runtime plugins
coexist: only the one matching the active runtime activates.

The `turn.start` hook shows the initialization pattern — a plugin can
set its own context keys at turn start so downstream hooks and tools see a
stable contract:

```yaml
hooks:
  - event: turn.start
    handler: init_reference_runtime
    priority: 20
    script: |
      def handle(event, payload):
          if not ctx.has("runtime"):
              ctx.set("runtime", "copilot-cli")
          if not ctx.has("delegation_depth"):
              ctx.set("delegation_depth", 0)
          return allow()
```

Load the reference harness to inspect it:

```bash
harness --harness-dir examples/reference/copilot-cli plugins
harness --harness-dir examples/reference/copilot-cli validate
```

Reading this file alongside the
[Concepts → Plugins](../concepts/plugins.md) page gives you the full
mental model.

## 8. Authoring checklist

A plugin is ready for review when:

- [ ] **`harness validate` passes.** No schema, validation, or Starlark
      compile errors.
- [ ] **Name is lowercase kebab-case.** Matches
      `^[a-z][a-z0-9-]*[a-z0-9]$`. No underscores, no uppercase.
- [ ] **`description:` is present and informative.** One sentence; says
      what the plugin does, not what it contains.
- [ ] **`version:` is set.** Especially if you intend to share or publish
      the plugin.
- [ ] **Tools return structured data**, not bare strings. Always return a
      dict with named fields. Errors return `{"error": "..."}`.
- [ ] **Each tool has a `tool.pre` guard for inputs you don't want.**
      The `guard-pr-comment-length` pattern above is the template.
- [ ] **The markdown body explains when to use the tools.** Treat it as
      the user manual the model reads.
- [ ] **`depends_on:` lists any external artifacts the plugin assumes.**
- [ ] **Condition is tested both active and inactive.** Run
      `harness run` with and without the context key to confirm.

## What you've learned

You've now built every layer of a governed plugin:

- **File format** — YAML frontmatter with `type: plugin`, `name:`, and
  `description:`; Markdown body as model context.
- **Tools inside a plugin** — same Starlark shape as standalone tool
  artifacts, bundled in the `tools:` list.
- **Hooks inside a plugin** — same event/priority/script shape as
  standalone hook artifacts, bundled in the `hooks:` list.
- **Conditional activation** — `condition:` Starlark expression evaluated
  per turn; empty means always active.
- **`depends_on:`** — explicit load-time dependency on other artifacts.
- **Priority 40** — plugins compose below builtins (60); the only way to
  override a builtin is `type: override`.

## What to read next

- **[Plugins (concept)](../concepts/plugins.md)** — the design rationale,
  full field reference, composition model, and why priority-40 exists.
- **[Writing a Tool](./writing-a-tool.md)** — the symmetric tutorial for
  standalone tool artifacts; the Starlark sandbox is identical.
- **[Writing a Hook](./writing-a-hook.md)** — deeper coverage of
  `allow / block / modify`, event payloads, and hook composition.
- **Reference:** [Starlark Built-ins](../reference/starlark-builtins.md) —
  every built-in available inside plugin tool and hook scripts.
- **Reference:** [Hook Artifact Schema](../reference/hook-artifact.md) —
  the full hook event catalog and payload shapes.
