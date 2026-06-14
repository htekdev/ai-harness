# Tools

> A tool is a single Markdown file that turns a Starlark function into a
> capability the model can call.

Tools are the most concrete primitive in AI Harness. If you only learn one
artifact type, learn this one — every other concept (hooks, delegation,
governance) is built around regulating *what tools do and when they fire*.

## What a tool is

A tool artifact has three jobs:

1. **Declare a contract** — name, description, typed parameters, timeout.
2. **Run sandboxed logic** — a Starlark `run(args)` function with access to
   curated built-ins (`exec`, `fs`, `http`, `string`, `cache`, …).
3. **Return a structured result** — a dict the harness serializes back to
   the model as a tool result.

Tools are loaded from `.harness/tools/*.md` and are addressable by name from
the model. They are *not* free-form code: the parameter schema is enforced
before `run` is called, and every built-in respects the active sandbox
(filesystem jail, network allowlist, timeout, hook gating).

## Anatomy of a tool

A complete, real tool from the `governed-agent` example:

```markdown
---
parameters:
  command: { type: string, required: true }
  timeout_ms: { type: number, required: false }
script: |
  def run(args):
      command = args.get("command", "")
      timeout = args.get("timeout_ms", 15000)
      if not command:
          return {"error": "command is required"}
      result = exec.run("bash", ["-lc", command], timeout)
      return {
          "stdout": string.truncate(result.get("stdout", ""), 4000),
          "stderr": string.truncate(result.get("stderr", ""), 2000),
          "exit_code": result.get("exit_code", 0),
      }
---

# run_command

Run a shell command through a named wrapper. The `command_guard` hook blocks
known-dangerous patterns (`rm -rf /`, `mkfs`, `dd if=`, …) before the
command ever reaches the OS.
```

Three things to notice:

- **The frontmatter is the contract.** `parameters` is the schema the model
  sees and the harness validates against. `script` is the implementation.
- **The body is documentation.** Reviewers, teammates, and you-in-six-months
  read it to understand *why* the tool exists. The model never sees it.
- **Naming matters.** This file is `run_command.md` — a *named wrapper*
  around the raw `exec.run` built-in. Hooks can distinguish "agent asked
  for `run_command`" (allowed) from "agent tried to call `exec` directly"
  (blocked). That distinction is only possible because the tool is a
  first-class artifact, not an inline closure.

## The Starlark sandbox

Tool scripts run in Starlark, not Python. The dialect is intentionally
minimal: no I/O at the language level, no imports, no recursion, no global
mutable state. Everything the script can affect goes through harness-owned
built-ins.

The built-ins available inside `run(args)` include:

| Built-in     | Purpose                                                  |
|--------------|----------------------------------------------------------|
| `exec.run`   | Execute a process under the active command sandbox       |
| `fs.read` / `fs.write` / `fs.exists` / `fs.stat` | Filesystem ops, jailed to the workspace |
| `http.get` / `http.post` | HTTP calls, gated by the network allowlist     |
| `string.truncate`        | Bounded string helpers                         |
| `cache.get` / `cache.set` | Per-run KV cache                              |
| `log.info` / `log.warn`  | Structured logging that flows into hooks       |

Every built-in is observable: a hook can fire before and after each call,
and the tool's full input/output is available to `audit_tool_pre` and
`audit_tool_post` hooks for free.

## Why tools are files, not functions

A tool could in principle be defined in Go, registered through a plugin API,
and shipped as a binary. We deliberately reject that design for the default
path. The reasons map directly back to the [Harness as Code thesis][hac]:

- **Reviewable.** A diff like `+ .harness/tools/run_command.md` tells a
  reviewer everything: the parameter schema, the implementation, and the
  human-readable contract — in one file.
- **Composable.** Adding or removing a capability is `git mv` away. There
  is no central registry to update, no init function to register against,
  no SDK to bump.
- **Portable.** The same `run_command.md` works under any harness that
  speaks the artifact format. No vendor SDK is in the loop.
- **Governed.** Because tools are Markdown, the policy layer can read them
  too. Hooks can inspect the script, lint for dangerous patterns, or
  refuse to load tools that don't carry a required tag.

The Go plugin path still exists for cases that genuinely need it (heavy
compute, native libraries, performance-critical loops). It is the escape
hatch, not the default.

## Naming a tool well

A name is part of the agent's API surface. The model picks tools by name
and description, and hooks key off names to apply policy. Two rules carry
most of the weight:

1. **Verb-first, lowercase, snake_case.** `run_command`, `read_file`,
   `search_code`, `send_telegram`. The model parses these like English.
2. **Wrap raw built-ins under a domain name when policy matters.** Don't
   expose `exec` directly — wrap it as `run_command`, `git_diff`,
   `pytest_run`. Each wrapper gives hooks a stable hook point and gives
   reviewers a stable file to audit.

The `prefer_named_tools` hook in the governed-agent example enforces this:
the agent is allowed to call any *named* tool, but a raw `exec.run` from a
free-form turn is rejected. That guarantee only exists because tools are
named artifacts.

## Tools versus plugins versus skills

People coming from other harnesses often ask where the line is. In AI
Harness:

- **Tools** are *capabilities the model invokes by name*. They have typed
  parameters and structured returns.
- **Plugins** are *bundles of tools, hooks, and prompt fragments* shipped
  together as a single conceptual unit (`copilot-runtime`, for example).
- **Skills** are *prompt-level patterns* — Markdown that teaches the model
  *how* to use the tools, without adding new capabilities itself.

Most users only ever write tools and the occasional hook. Plugins and
skills are how you compose them at scale.

## Tool execution lifecycle

When the model calls a tool, the harness runs this sequence:

1. **Resolve.** Look up the tool by name; reject if not registered.
2. **Validate.** Coerce and check arguments against the parameter schema.
3. **`pre` hooks.** Fire any hook subscribed to `tool.pre` for this tool —
   they can inspect args, modify them, or veto the call.
4. **Execute.** Run `run(args)` under the Starlark sandbox with the timeout
   from the tool's frontmatter.
5. **`post` hooks.** Fire any hook subscribed to `tool.post` — they see the
   final return value and can amend or redact it.
6. **Return.** Serialize the (possibly hook-modified) result back to the
   model.

This pipeline is the same for every tool. There is no "fast path" that
skips hooks or validation, and no way for a tool to bypass the sandbox.
That uniformity is what makes governance tractable: a single hook can
enforce a policy across *every* tool the agent will ever call.

## What to read next

- **[Hooks](./hooks.md)** — how to attach policy and observability to the
  tool lifecycle without modifying the tools themselves.
- **[Delegation](./delegation.md)** — how tools fit into sub-agent flows
  and async work.
- **[Governance & Policy](./governance.md)** — how to compose hooks,
  allowlists, and tool wrappers into an agent you'd actually deploy.
- **Reference:** the full [Tool Artifact Schema](../reference/tool-artifact.md)
  documents every supported frontmatter field.

[hac]: ./harness-as-code.md
