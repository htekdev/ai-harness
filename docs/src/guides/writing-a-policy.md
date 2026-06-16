# Writing a Policy

> A hands-on tutorial. By the end of this guide you'll have written,
> validated, and shipped a four-layer governance stack: a `tools_policy`
> allowlist in `harness.md`, a hard-block hook on dangerous shell
> patterns, a path-jail hook, a self-augment guard on
> `meta.register_tool`, and an audit pair that turns every call into a
> metric.

This guide assumes you finished the [Quickstart](../getting-started.md)
and at least one of [Writing a Tool](./writing-a-tool.md) or
[Writing a Hook](./writing-a-hook.md). The conceptual backdrop lives in
[Governance & Policy](../concepts/governance.md) — read it first if you
want the *why*; this page is strictly the *how*.

If you want a finished reference, every artifact built here is shipped
in the [`governed-agent`](../examples/governed-agent.md) example. Open
that directory side-by-side and you'll see exactly the same files we
write below.

## What "policy" actually is in AI Harness

There is no `Policy` type. Policy is a **composition** of two artifact
kinds you've already met:

| Layer | Artifact | What it answers |
|-------|----------|-----------------|
| 2     | `tools_policy:` block in `harness.md` | "Which tools may the agent call **at all**?" |
| 3     | Hooks in `.harness/hooks/` | "Under which **conditions** may the agent call them?" |

Layer 2 is the **registry gate**: a YAML block, enforced before the
model even sees a tool list, where `deny` always beats `allow`. Layer 3
is the **conditional plane**: per-call hooks that inspect arguments,
session state, model output, and return `allow / block / modify`.

Layer 1 (registration) and layer 4 (OS sandboxes) are real and matter,
but they aren't artifacts you "write" — layer 1 is the act of putting
files in `.harness/tools/`, and layer 4 is systemd / Docker / network
policies. This guide is about the two layers you author as code.

## 1. Set up

If you don't already have a workspace:

```bash
mkdir -p governed-demo && cd governed-demo
harness init .
```

`init` scaffolds a minimal `harness.md` plus a handful of stock tools
(`fs.read`, `fs.list`, `fs.glob`, `run_command`, `web_fetch`). That is
exactly the surface this guide governs.

## 2. Layer 2 — write the `tools_policy` block

Open `harness.md` and add the policy section:

```yaml
# Declarative tool governance.
# allowlist mode: ONLY tools matching a pattern below may be invoked.
# Deny entries always win over Allow.
tools_policy:
  mode: allowlist
  allow:
    - "fs.read"
    - "fs.list"
    - "fs.glob"
    - "web_fetch"
    - "run_command"
    - "delegate*"
  deny:
    - "fs.remove"
    - "fs.move"
    - "exec"
```

Three rules to internalize:

- **`mode: allowlist` flips the default.** *Nothing* is callable unless
  a pattern matches. A new tool dropped into `.harness/tools/` next
  week is invisible to the agent until you add it here. That is the
  feature — additions are explicit.
- **`deny` always beats `allow`.** A wildcard like `delegate*` cannot
  accidentally re-enable `exec`. The denylist is sticky.
- **Enforcement is at the registry, not at the model.** A denied tool
  is removed from the tool list the model sees. There is nothing to
  jailbreak.

Validate it compiles:

```bash
harness validate
```

Expected:

```
✅ harness.md valid
   5 tools, 0 hooks, 0 agents (3 ms)
```

The number of tools dropped from however many you had to the five that
match an `allow` entry. That's the registry doing its job.

Try a denied call:

```bash
harness run "Use the exec tool to print hello."
```

The model receives a tool list that does not contain `exec` and
explains to the user that it has no such tool. No hook fired, no
sandbox check ran — the policy never let the call leave the registry.

> **Why allowlist over denylist?** A denylist optimizes for
> *convenience now*; an allowlist optimizes for *surprise resistance
> later*. If you can name your tools at all, you can name the four to
> ten you actually use. Allowlist is the production default.

## 3. Layer 3 — write your first guard hook

Tool policy answers "may the agent call `run_command`?" Hooks answer
"may the agent call `run_command` *with `rm -rf /`*?" Create
`.harness/hooks/command_guard.md`:

```markdown
---
event: tool.pre
priority: 10
when: payload["name"] == "run_command"
script: |
  def handle(event, payload):
      cmd = payload.get("args", {}).get("command", "")
      dangerous = [
          "rm -rf /",
          "rm -rf /*",
          ":(){ :|:& };:",
          "mkfs",
          "dd if=",
          "shutdown",
          "reboot",
          "> /dev/sda",
          "chmod -R 000 /",
      ]
      for d in dangerous:
          if d in cmd:
              metrics.incr("audit.policy.deny")
              return block("dangerous command pattern blocked: '" + d + "'")
      return allow()
---

# command_guard

Hard-blocks well-known destructive shell patterns. This is intentionally
a list of literal substrings — the goal is "make obvious damage hard",
not "sandbox an adversary". For real isolation pair this with the
systemd unit (`deploy/systemd/harness.service`) or a Docker container
with read-only mounts.
```

A few things to notice:

- **`when:` is a fast prefilter.** It runs before `handle` is even
  invoked, so the entire hook is a no-op for any tool that isn't
  `run_command`. Use `when:` aggressively — hooks without it pay
  Starlark startup cost on every event.
- **`metrics.incr` is the audit signal.** Even a hard block leaves a
  metric behind, so refusal rates show up in `metrics.snapshot()`.
- **Substring matching is a *speed bump*, not a sandbox.** A motivated
  adversary will route around a string list. The point of this hook is
  to make obviously-broken `run_command` invocations from a
  hallucinating model fail loudly. OS-level isolation (layer 4) is
  what actually defends you.

Validate:

```bash
harness validate
```

Expected:

```
✅ harness.md valid
   5 tools, 1 hook, 0 agents (3 ms)
```

Trigger it:

```bash
harness run "Run 'rm -rf /tmp/foo' to clean up."
```

The model sees a structured error (`blocked by hook "command_guard":
dangerous command pattern blocked: 'rm -rf /'`) and reports the
refusal. The Starlark `run` for `run_command` never executed.

## 4. Jail the filesystem with `path_guard`

`run_command` is the obvious blast radius, but `fs.read` / `fs.list` /
`fs.glob` are equally dangerous if absolute paths and `..` are allowed.
Create `.harness/hooks/path_guard.md`:

```markdown
---
event: tool.pre
priority: 10
when: payload["name"] in ["fs.read", "fs.list", "fs.glob"]
script: |
  def handle(event, payload):
      args = payload.get("args", {})
      path = args.get("path", "")
      if not path:
          path = args.get("pattern", "")
      if ".." in path:
          metrics.incr("audit.policy.deny")
          return block("path traversal not allowed: contains '..'")
      if path.startswith("/") or (len(path) > 1 and path[1] == ":"):
          metrics.incr("audit.policy.deny")
          return block("absolute paths not allowed in this profile")
      return allow()
---

# path_guard

Blocks any filesystem read whose path contains `..` or is absolute
(both POSIX `/etc` and Windows `C:` forms). Combined with a systemd
unit's `ReadWritePaths` or Docker's read-only mount, this gives layered
defense: the harness rejects bad paths *and* the OS would reject them
again at syscall time.
```

Two patterns worth naming:

- **One hook, multiple tools.** The `when:` clause uses `in [...]` so a
  single artifact governs three related tools. Beats writing three
  copies, beats hiding the policy inside each tool's body.
- **Cross-platform path detection.** `path[1] == ":"` catches Windows
  drive letters without a regex. Starlark string indexing is bounded
  and safe — the `len(path) > 1` guard prevents a panic on `"C"`.

## 5. Lock down self-augmentation with `meta_tool_guard`

The deepest hole in any agent platform is self-augmentation: the agent
calls `meta.register_tool` mid-session and adds a tool the policy
doesn't know about. AI Harness governs this with its own event:

```markdown
---
event: meta.register_tool
priority: 5
script: |
  def handle(event, payload):
      name = payload.get("name", "")
      banned_prefixes = ["exec", "fs.remove", "fs.move", "system."]
      for p in banned_prefixes:
          if name == p or name.startswith(p + "_") or name.startswith(p + "."):
              metrics.incr("audit.meta.deny")
              return block("self-augment blocked: tool name '" + name + "' matches banned prefix '" + p + "'")
      log("[audit] meta.register_tool approved name=" + name)
      return allow()
---

# meta_tool_guard

Governs the **self-augmenting** path. When the agent uses
`meta.register_tool` to define a new capability mid-session, this hook
enforces the same naming policy as `tools_policy.deny` — so the agent
cannot "rename its way" around governance.
```

This is the artifact that makes "the harness governs itself" actually
true. Without it, `tools_policy.deny: ["exec"]` is a startup-time
constraint; with it, the constraint travels into runtime as well.

> **Pair the prefix list with `tools_policy.deny`.** Drift between the
> two is the most common bug in this whole stack. A future improvement
> is sharing one source of truth — for now, keep them in lockstep and
> review them in the same PR.

## 6. The audit pair — every call becomes a metric

Two hooks at priority `1` — one on `tool.pre`, one on `tool.post` —
that do nothing but `metrics.incr` and `log`. They run **before** any
guard, so even blocked calls show up in metrics.

`.harness/hooks/audit_tool_pre.md`:

```markdown
---
event: tool.pre
priority: 1
script: |
  def handle(event, payload):
      metrics.incr("audit.tool.pre")
      log("[audit] tool.pre name=" + payload.get("name", "?"))
      return allow()
---

# audit_tool_pre

Counts every tool call attempted (including ones a higher-priority
guard will block). `audit.tool.pre - audit.tool.post` is the refusal
rate. `audit.tool.pre / turn` is the call-rate-per-turn SLO.
```

`.harness/hooks/audit_tool_post.md`:

```markdown
---
event: tool.post
priority: 1
script: |
  def handle(event, payload):
      metrics.incr("audit.tool.post")
      log("[audit] tool.post name=" + payload.get("name", "?"))
      return allow()
---

# audit_tool_post

Counts every tool call that actually returned a result. Pair with
`audit_tool_pre` to derive refusal rate.
```

This is one of the most undervalued patterns in the whole governance
stack. Once it's in, you have an instant SLO surface:

```
audit.tool.pre        # calls attempted
audit.tool.post       # calls succeeded
audit.policy.deny     # calls hard-blocked
audit.meta.deny       # self-augment attempts blocked
```

Three numbers tell you the health of the agent. None of them require a
metrics library — `metrics.incr` is a built-in.

## 7. Cap the completion window

The last hook in the stack runs on `completion.pre` — the hand-off
from harness to provider. It exists to reject pathological inputs the
earlier hooks couldn't see (e.g. a tool returning 5,000 messages in one
shot):

```markdown
---
event: completion.pre
priority: 50
script: |
  def handle(event, payload):
      messages = payload.get("messages", [])
      if len(messages) > 200:
          metrics.incr("audit.policy.deny")
          return block("conversation history too long (max 200 messages)")
      return allow()
---

# completion_window_guard

Caps the conversation window before it goes to the provider. The
`context.max_history` setting in `harness.md` already trims older
turns; this hook is the last-line defense against runaway tool output.
```

Why a hook instead of just a setting? Because the setting trims
silently and the hook *records the deny*. When you see
`audit.policy.deny` spike, you want to know which guard fired — not
that "history was quietly truncated again."

## 8. Putting it all together

Your `.harness/hooks/` directory should now look like this:

```
.harness/hooks/
├── audit_tool_pre.md            # priority  1 — count every call
├── audit_tool_post.md           # priority  1 — count every result
├── command_guard.md             # priority 10 — deny dangerous shell
├── path_guard.md                # priority 10 — jail filesystem reads
├── meta_tool_guard.md           # priority  5 — guard self-augmentation
└── completion_window_guard.md   # priority 50 — cap conversation window
```

Read it top-to-bottom and the governance posture is plain English:
*audit everything, deny dangerous commands, jail the filesystem, guard
self-augmentation, cap the conversation window.* Each line is one
file. Each file is roughly thirty lines of YAML and Starlark. Each
file is a diff in Git.

Run `harness validate` one more time:

```
✅ harness.md valid
   5 tools, 6 hooks, 0 agents (4 ms)
```

Run a sanity check:

```bash
harness run "Read /etc/passwd and tell me who owns it."
```

Expected outcome: the model attempts `fs.read` with `path=/etc/passwd`,
`path_guard` blocks at `tool.pre` with "absolute paths not allowed",
the model reports the refusal to the user. `metrics.snapshot()` shows
`audit.tool.pre=1`, `audit.policy.deny=1`, `audit.tool.post=0` — a
clean refusal trace.

## 9. Pattern catalog (memorize these)

Five patterns appear in nearly every production policy. They show up in
this exact stack and are worth naming:

| Pattern | Priority | Event | Verdict | Purpose |
|---------|----------|-------|---------|---------|
| Audit-everything | `1` | `tool.pre` / `tool.post` | `allow()` | Metric every call (incl. blocked) |
| Deny-list guard | `10` | `tool.pre` | `block()` | Hard-block known-bad payloads |
| Path/argument jail | `10` | `tool.pre` | `block()` | Reject inputs outside policy |
| Self-augment guard | `5` | `meta.register_tool` | `block()` | Govern runtime tool registration |
| Window cap | `50` | `completion.pre` | `block()` | Last-line defense before provider |

Priority numbering is conventional: `1` for audit, `5–10` for hard
guards, `20–30` for shaping/normalizing hooks, `40–50` for end-of-turn
caps. Pick a convention, document it in `harness.md`, and stick to it.

## 10. What changes when policy ships

The whole reason policy is artifacts-not-config is that policy changes
ship the way every other change ships. To raise a denylist:

```diff
 deny:
   - "fs.remove"
   - "fs.move"
   - "exec"
+  - "system.shutdown"
```

That's a one-line PR. CI re-validates the harness, the deploy pipeline
restarts the runtime, and the next turn enforces the new policy. There
is no "policy reload endpoint", no "feature flag to toggle", no
"runtime config service to redeploy". The artifact graph is
re-evaluated every turn — see the [Per-turn evaluation
section](../concepts/governance.md#policy-enforcement-is-per-turn-not-just-at-startup)
in the concepts page.

Three operator consequences worth burning in:

1. **Incident response is a code change.** New dangerous command
   pattern? One entry in `command_guard.md`. New must-block tool?
   One line in `tools_policy.deny`.
2. **Audit trails are Git trails.** "When did we start denying X?"
   is `git log -p .harness/hooks/command_guard.md`.
3. **Reviewable surface is bounded.** A security review on this
   profile is reading six small files plus a YAML block. There is no
   third party, no plugin scan, no "registered handlers" list.

## 11. Where to go next

- The complete reference implementation lives in
  [`examples/governed-agent`](https://github.com/htekdev/ai-harness/tree/main/examples/governed-agent)
  with all six hooks plus a CI job that exercises a denied call,
  asserts the metrics, and dumps the trace.
- Pair this guide with [Network Sandboxing](./network-sandbox.md) to
  add the layer-4 outbound-traffic gate.
- Pair it with [Observability with
  OpenTelemetry](./observability.md) so the same `audit.*` metrics
  flow into your existing dashboards.
- For governing **sub-agents**, the same hook events fire under
  delegation — see [Writing a Sub-Agent](./writing-a-sub-agent.md) for
  `delegation.pre` / `delegation.post` and how policy propagates into
  child loops.

You now have the full Layer 2 + Layer 3 stack. Layer 4 is your
operating system, and that's the next guide on the path to a
production deployment.
