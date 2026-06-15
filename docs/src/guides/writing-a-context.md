# Writing a Context

> A hands-on tutorial. By the end of this guide you'll have written
> three real context artifacts — a root identity, a conditional plugin
> that switches the agent into PR-review mode, and an override that
> tightens behavior in production — and you'll have inspected exactly
> how they assemble into the prompt with `harness context`.

This guide assumes you finished the [Quickstart](../getting-started.md)
and have `harness` on your `PATH`. The [Writing a Tool](./writing-a-tool.md)
and [Writing a Hook](./writing-a-hook.md) guides are useful background
but not required.

## What "context" actually is

In AI Harness, **context is the markdown body of an artifact**. There
is no separate `context:` field, no special directory, and no second
prompting language. Anything you write below the YAML frontmatter
becomes a chunk of model-visible text that the harness composes into
the system prompt every turn.

That's it. The whole context system is:

1. Each artifact contributes its body as a context block.
2. Active artifacts are merged in priority order each turn.
3. The `harness` artifact's body becomes the **identity** (root prompt).
4. `plugin` and `override` bodies become additional **context blocks**
   appended after identity.
5. Conditions decide which artifacts are active for *this* turn.

Composition is governed by the typed artifact contract. The default
priorities are:

| Kind       | Priority | Role                                       |
|------------|----------|--------------------------------------------|
| `plugin`   | 40       | Conditional, per-mode context              |
| `builtin`  | 60       | Stable, shipped-with-the-runtime context   |
| `harness`  | 80       | The root identity — exactly one per project |
| `override` | 100      | Final word — environment or policy clamps  |

Higher priority runs **last**, so override blocks see and follow
everything before them. Identity is special: only the `harness`
artifact's body becomes `Identity`; everyone else's body is appended
as a `ContextBlock`.

The same shape that defines tools and hooks defines context. That is
the whole "harness as code" idea: one file, one capability bundle,
governed by the same rules.

## 1. Set up

If you don't already have a workspace:

```bash
mkdir -p my-agent && cd my-agent
harness init .
```

You should see:

```
my-agent/
├── harness.md
└── .harness/
    ├── tools/
    └── hooks/
```

The top-level `harness.md` is your **identity context**. Open it —
the body below the frontmatter is the root system prompt the model
sees every turn.

## 2. Write the identity

Replace the body of `harness.md` with something specific to your
project. The frontmatter stays — just rewrite the markdown body:

```markdown
---
model:
  provider: copilot
  name: gpt-4o
  max_tokens: 4096
  temperature: 0.7
  api_key_env: GH_TOKEN
context:
  max_history: 50
  max_tokens: 128000
---

# Repo Maintainer

You are the maintainer agent for **my-agent**. Your job is to keep
this repository tidy, well-tested, and shippable.

## Operating principles

- Read before you write. Always inspect a file with `read_file`
  before modifying it.
- Prefer small, reviewable diffs. If a change touches more than five
  files, stop and ask the user to confirm scope.
- Never commit or push without explicit user approval.

## What "done" looks like

- Tests pass.
- Lint passes.
- Diff is small enough to review in under five minutes.
```

Validate the project loads:

```bash
harness validate
```

Then look at the assembled prompt:

```bash
harness context
```

You should see one **Identity** section sourced from `harness.md`, no
context blocks yet, and a small token budget reading.

## 3. Add a conditional plugin context

Now we'll add behavior that **only** turns on when the agent is doing
PR review. Create the directory and file:

```bash
mkdir -p .harness/plugins
```

`.harness/plugins/pr-review.md`:

```markdown
---
name: pr-review
type: plugin
version: "1.0.0"
description: "Activates PR review rules when mode == pull_request"
tags: ["context", "pr"]
condition: 'ctx.get("mode") == "pull_request"'
---

# PR Review Mode

You are reviewing a pull request. Hold every change to this bar:

## What to look for

- **Correctness:** does the change do what the description says?
- **Tests:** are there tests for the new behavior, and do they
  actually exercise it?
- **Risk:** does this touch auth, payments, data migrations, or
  anything else that fails loudly?
- **Diff hygiene:** unrelated changes get called out and asked to
  split.

## How to write comments

- Quote the code you're commenting on.
- Suggest a concrete fix, not just a complaint.
- If something is fine but you want to flag it, say "nit:" so the
  author knows it isn't blocking.

Approve only when correctness, tests, and risk all clear.
```

Two things are doing the work here:

- **`type: plugin`** puts this artifact at priority 40 — it loads
  *after* identity, so the model sees PR-review rules as an addition
  to the root persona, not a replacement.
- **`condition:`** is a [Starlark](https://github.com/google/starlark-go)
  expression evaluated **every turn**. When `ctx.get("mode")` returns
  `"pull_request"`, the artifact is active and its body is included.
  When it doesn't, the artifact is silently dropped from the prompt.

Validate and inspect again:

```bash
harness validate
harness context --verbose
```

By default `mode` is unset, so you should see `pr-review` listed but
**INACTIVE**:

```
⚪ pr-review (plugin, priority 40, INACTIVE)
   Condition: ctx.get("mode") == "pull_request" → False
```

Now activate it. The CLI's `--agent` flag passes runtime values into
the condition context; for arbitrary values, set them via your
runtime entry point or a hook. The cleanest way to test from the
shell is to wrap the inspector in a small script that seeds runtime
state, but for now the simplest verification is to read the
condition expression and confirm it parses.

When the plugin is active, `harness context` will show:

```
✅ pr-review (plugin, priority 40, ACTIVE)
   Condition: ctx.get("mode") == "pull_request" → True
```

…and the body of `pr-review.md` will be appended to the assembled
prompt right after the identity block.

## 4. Add a production override

Plugins are *additive*. Sometimes you need a context that **clamps**
behavior — a final-word block that lands at priority 100 and is meant
to be obeyed no matter what came before.

Create the directory and file:

```bash
mkdir -p .harness/overrides
```

`.harness/overrides/production.md`:

```markdown
---
name: production-mode
type: override
version: "1.0.0"
description: "Tightens behavior when running against production"
condition: 'ctx.get("env") == "production"'
---

# Production Mode

You are operating against **production** infrastructure. The
following rules override anything earlier in this prompt:

- **No destructive actions without explicit confirmation.** Even if
  an earlier rule says "be proactive," in production you wait.
- **No schema or data migrations.** Surface the SQL, do not run it.
- **Every external call is logged.** Use the `log` builtin before
  invoking any tool that hits a network or filesystem outside this
  repo.
- **If you are unsure, stop.** Returning "I need confirmation" is
  always a valid action in production.
```

Why this lives in `overrides/` instead of `plugins/`:

- `type: override` runs at priority **100**, after every plugin and
  after the harness identity itself. The model reads it last, so it
  shapes the final reasoning.
- It's the right place for environmental clamps — production gates,
  read-only modes, "this branch is frozen" rules.
- Reviewers can scan one directory to know all the places policy can
  tighten on top of identity. Governance is observable.

Run `harness context --verbose` again to see the override's
priority, condition, and source line up exactly with what you wrote.

## 5. Verify the assembled prompt

The whole point of context-as-code is that you can audit what the
model actually sees. Three commands you should know:

```bash
# Summary view: which artifacts are active, total tokens, budget.
harness context

# Detailed view: per-artifact priority, condition, and source path.
harness context --verbose

# Machine-readable: pipe into your own linters or CI checks.
harness context --json
```

The `--json` form is the one to wire into CI. A small workflow that
runs `harness context --json` on every PR and diffs the active
artifact list catches accidental drift — for example, a plugin whose
condition silently broke after a refactor and is no longer firing.

A useful self-check: any time you change a context file, ask
yourself *"would I want this in the system prompt for every turn it
matches?"* If the answer is "only sometimes," you probably want a
tighter `condition:`. If the answer is "always, no matter what,"
it belongs in identity, not a plugin.

## 6. Patterns worth knowing

**Mode-based context.** Use a single `mode` runtime value
(`pull_request`, `incident`, `interactive`, `nightly`) and let
plugins key off it. One central knob, many conditional artifacts.

```yaml
condition: 'ctx.get("mode") == "incident"'
```

**Repo-scoped context.** Plugins that only apply when the agent is
operating on a specific repo or path:

```yaml
condition: 'ctx.get("repo") == "htekdev/ai-harness"'
```

**Time-windowed context.** Useful for quiet hours, daily-summary
windows, or "do not page humans before 8 AM":

```yaml
condition: '8 <= time.now() % 86400 / 3600 < 18'
```

`time.now()` returns UTC; if you need local time, set a timezone
offset in runtime state and add it before the modulo.

**Composing conditions.** Starlark supports `and`, `or`, `not`:

```yaml
condition: 'ctx.get("mode") == "review" and ctx.get("lang") == "go"'
```

**Explicit priority.** When two artifacts of the same kind disagree,
override the default with `priority:`:

```yaml
---
name: hotfix-mode
type: plugin
priority: 75   # higher than other plugins (40), lower than identity (80)
condition: 'ctx.get("hotfix") == True'
---
```

Stick to multiples of 10 in the 1–200 range; that keeps mental room
for inserting things later without renumbering everything.

## 7. Common mistakes

- **Putting policy in identity.** Identity is a stable persona.
  Anything that should change with environment, mode, or repo
  belongs in a plugin or override — otherwise you'll keep editing
  `harness.md` and re-deploying when you really want a condition
  flip.
- **Silently broken conditions.** A typo in a Starlark expression
  doesn't crash — the artifact just stays inactive. `harness
  context --verbose` shows the parsed condition and its current
  result, so make a habit of running it after edits.
- **Two artifacts trying to be identity.** Only one `type: harness`
  artifact contributes the identity block. If you have multiples,
  the registry will reject the duplicate at load time.
- **Override used for *adding* context.** Overrides are for clamping
  and final-word policy. If your block is purely additive ("here is
  another helpful tip"), make it a plugin. Overrides at priority 100
  should be rare and load-bearing.

## 8. Where to go next

- [Writing a Tool](./writing-a-tool.md) — give the agent capabilities.
- [Writing a Hook](./writing-a-hook.md) — gate and audit those
  capabilities.
- [Governance & Policy](../concepts/governance.md) — how identity,
  plugins, and overrides compose into a governable system.
- [Harness as Code](../concepts/harness-as-code.md) — the design
  philosophy behind the typed artifact model.

When you find yourself reaching for "where do I configure this?",
the answer is almost always *write a context*. One file, one
condition, in source control, observable through `harness context`.
That is the whole product.
