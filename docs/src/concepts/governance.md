# Governance & Policy

> Governance is not a feature you turn on. It is the **default shape** of an
> AI Harness project — a stack of typed artifacts you can read, review, and
> diff like any other code.

The previous concept pages introduced the primitives one at a time:
[Harness as Code](./harness-as-code.md), [Tools](./tools.md),
[Hooks](./hooks.md), [Delegation](./delegation.md). This page is where they
compose. It is the story of how AI Harness takes "the model can call tools"
and turns it into "the model can call *these* tools, on *these* paths, from
*these* domains, up to *this* depth, with *this* audit trail, and every byte
of that policy is a file in your repo."

## What "governance" means here

In most agent frameworks, governance is something you bolt on:

- A middleware in front of the model.
- A wrapper around the tool registry.
- A linter that scans prompts.
- A spreadsheet of "approved tools" maintained out-of-band.

In AI Harness, governance is a property of the **artifact graph itself**:

1. **Tools** declare what the agent *can* do.
2. **`tools_policy`** in `harness.md` declares which of those it *may* do.
3. **Hooks** in `.harness/hooks/` declare the *conditions* under which it
   may do them — and what to record while it does.
4. **Delegation config** declares how that policy *propagates* into
   sub-agents.

Every one of those is a file. Every file is a diff. Every diff is a pull
request. There is no governance surface that lives outside Git, and no way
for a tool, model, or sub-agent to opt out of the chain.

That is the entire definition. The rest of this page is what falls out of
taking it seriously.

## The four layers of the governance stack

A governed AI Harness agent enforces policy at four distinct layers, each
strictly above the last. A call that survives layer *n* still has to clear
layer *n+1*.

```
┌─────────────────────────────────────────────────────────────┐
│ 4. Runtime sandboxes      network allowlist, command guard, │
│                           OS isolation (systemd/Docker)     │
├─────────────────────────────────────────────────────────────┤
│ 3. Hook artifacts         tool.pre / tool.post / turn.*     │
│                           allow / block / modify decisions  │
├─────────────────────────────────────────────────────────────┤
│ 2. Tool policy            tools_policy: allowlist / deny    │
│                           enforced at the registry          │
├─────────────────────────────────────────────────────────────┤
│ 1. Tool registration      only artifacts in .harness/tools  │
│                           reach the model at all            │
└─────────────────────────────────────────────────────────────┘
```

Read top-down for *defense in depth*. Read bottom-up for *blast radius*:
a misconfigured layer 1 leaks a tool name; a missing layer 4 leaks a
syscall. Both matter; they matter differently.

### Layer 1 — Tool registration

The model only ever sees tools registered as artifacts. There is no
"global registry" populated by `init()` side effects, no plugin scan that
loads whatever is on disk, no decorator-based magic. If a tool is not a
`.harness/tools/*.md` file (or an explicitly-mounted built-in), the model
cannot name it, let alone call it.

This is the cheapest possible filter and it eliminates an entire class of
"I forgot we registered that" bugs.

### Layer 2 — Tool policy

`tools_policy` in `harness.md` is the **declarative gate** on the
registry. The `governed-agent` example pins it explicitly:

```yaml
tools_policy:
  mode: allowlist
  allow:
    - "fs.read"
    - "fs.list"
    - "fs.glob"
    - "web_fetch"
    - "run_command"
    - "self_check"
    - "delegate*"
  deny:
    - "fs.remove"
    - "fs.move"
    - "exec"
```

Three properties matter:

- **`mode: allowlist`** flips the default. *Nothing* is callable unless a
  pattern matches, including future tools added by a self-augmenting flow.
- **`deny` always beats `allow`.** A wildcard that accidentally widens
  scope cannot resurrect a denied name.
- **Enforcement is at the registry, not at the model.** The model never
  sees a denied tool in its tool list, so a clever prompt cannot convince
  it to "try anyway."

Tool policy is the first place a security review should look. It is one
block of YAML, in one file, that answers "what could this agent do today?"

### Layer 3 — Hook artifacts

Hooks are the **conditional policy plane**. Tool policy answers "may the
agent call `run_command`?" Hooks answer "may the agent call `run_command`
*with `rm -rf /`*?"

The governed-agent example stacks seven hooks across two events, every one
of which is independently reviewable:

```
.harness/hooks/
├── audit_tool_pre.md          # priority 1   — count + log every call
├── audit_tool_post.md         # priority 1   — count + log every result
├── command_guard.md           # priority 10  — deny dangerous shell patterns
├── path_guard.md              # priority 10  — jail filesystem reads
├── prefer_named_tools.md      # priority 20  — reject raw exec.run
├── meta_tool_guard.md         # priority 30  — block tools editing .harness/
└── completion_window_guard.md # priority 40  — cap output size per turn
```

The whole governance posture reads like English from top to bottom:
*audit everything, deny dangerous commands, jail the filesystem, only let
the agent use named tools, don't let it edit the harness itself, cap
completion size.*

Each line is a file. Each file is a ~30-line artifact. The composition
rules are the ones from [Hooks](./hooks.md): hooks for an event run in
priority order, the **first `block` wins**, `modify` rewrites payloads in
place, `allow` is a pass.

### Layer 4 — Runtime sandboxes

The final layer is the one that doesn't trust the harness. Network
allowlists, command guards, and OS-level isolation (systemd unit files,
read-only Docker mounts) all sit *below* the artifact graph and would
reject a bad call even if every Markdown artifact were misconfigured.

Two sandboxes ship in the box today:

- **Network allowlist.** Attach a `scripting.NetworkSandbox` with an
  explicit `allowed_domains` list. Any outbound request that doesn't
  match raises a `SandboxError`. The list is **deny-by-default the moment
  you set even one entry** — there is no implicit "everything else is
  fine."
- **Command guard.** Hook-enforced today (`command_guard.md`), with a
  reusable pattern library. Pair it with a real systemd unit
  (`deploy/systemd/harness.service`) or a non-privileged container for
  syscall-level isolation.

Layers 1-3 are the harness's job. Layer 4 is the operating system's job —
and a well-deployed harness uses both.

## Policy enforcement is per-turn, not just at startup

A subtle but load-bearing property of AI Harness: **the artifact graph is
re-evaluated every turn.** Add a hook mid-session and it fires on the
*next* tool call, not the next process restart. Edit `tools_policy` and
the next turn sees the new allowlist. Conditional artifacts
(`when: env == "prod"`) resolve dynamically against the current run
context.

This is why a small core is viable. The runtime never needs a
configuration-reload subsystem, a hot-swap API, or a "feature flag"
mechanism. Composition does that work, deterministically, in code.

For operators it means three concrete things:

1. **Policy changes ship the way every other change ships.** Edit the
   artifact, open a PR, merge, deploy. No special "policy pipeline."
2. **Incident response is a code change.** A new dangerous command
   pattern is one entry in `command_guard.md`. A new must-block tool
   name is one line in `tools_policy.deny`.
3. **Audit trails are Git trails.** "When did we start denying X?" is
   `git log -p .harness/hooks/command_guard.md`.

## Hookflow patterns

The `governed-agent` example crystallizes a handful of patterns that
appear in nearly every production agent. They are worth naming because
once you see them, you stop reinventing them.

### Pattern 1 — Audit-everything (`priority: 1`)

Two hooks at priority `1` — one on `tool.pre`, one on `tool.post` — that
do nothing but `metrics.incr` and `log`. They always `allow()`.

Because they run *before* any policy hook, every call is counted, even
ones that will be blocked. `metrics.snapshot()` becomes a real-time SLO
surface: `audit.tool.pre` is the call rate, `audit.policy.deny` is the
refusal rate, the ratio is your "how much is the agent fighting policy?"
gauge.

```yaml
event: tool.pre
priority: 1
script: |
  def handle(event, payload):
      metrics.incr("audit.tool.pre")
      log("[audit] tool.pre name=" + payload.get("name", "?"))
      return allow()
```

### Pattern 2 — Deny-list guards (`priority: 10`)

Hard blocks on well-known bad payload shapes. `command_guard` rejects
destructive shell patterns; `path_guard` rejects path traversal and
absolute paths. They run *after* audit so the deny shows up in metrics,
and *before* shaping hooks so the rejection is final.

```yaml
event: tool.pre
priority: 10
when: payload["name"] == "run_command"
script: |
  def handle(event, payload):
      cmd = payload.get("args", {}).get("command", "")
      for d in ["rm -rf /", "mkfs", "dd if=", "shutdown"]:
          if d in cmd:
              metrics.incr("audit.policy.deny")
              return block("dangerous command pattern: '" + d + "'")
      return allow()
```

This is the workhorse pattern. Most "we need to lock that down"
incidents resolve into a 5-line addition to a hook at priority 10.

### Pattern 3 — Channel narrowing (`priority: 20`)

Hooks that block *general-purpose* tools to force the model onto
specific ones. `prefer_named_tools` rejects raw `exec.run` so that
shell access only flows through `run_command` — which is itself audited,
guarded, and visible in the artifact list.

Why this matters: it collapses an unbounded surface ("the agent can run
any command") into a bounded one ("the agent can run `run_command`,
which is one diffable file"). Reviewers stop having to imagine; they
read.

### Pattern 4 — Self-augment governance (`meta.register_tool`)

The harness governs itself. When the agent uses `meta.register_tool` to
mint a new tool mid-session, the registration goes through the
`meta.register_tool` event — and `meta_tool_guard` enforces the *same*
naming policy as `tools_policy.deny`:

```yaml
event: meta.register_tool
priority: 5
script: |
  def handle(event, payload):
      name = payload.get("name", "")
      banned = ["exec", "fs.remove", "fs.move", "system."]
      for p in banned:
          if name == p or name.startswith(p + "_") or name.startswith(p + "."):
              metrics.incr("audit.meta.deny")
              return block("self-augment blocked: '" + name + "' matches banned prefix '" + p + "'")
      return allow()
```

The agent cannot "rename its way around governance." This is the
artifact that makes *"the harness governs itself"* literally true rather
than aspirationally true.

### Pattern 5 — Shape enforcement (`priority: 40`+)

Late-running hooks that modify rather than block. `completion_window_guard`
caps output size per turn; redaction hooks scrub PII from `tool.post`
payloads; truncation hooks bound tool result sizes before they hit the
context window.

These run *last* on purpose. Earlier hooks have already approved the
call; the job here is to keep the *shape* of the data flowing through the
agent within bounds. They almost always return `modify(payload)` rather
than `block()`.

### Pattern 6 — Delegation policy propagation

Sub-agents inherit the parent's hook stack by default. A child cannot
register a tool the parent's `tools_policy.deny` rejects, cannot bypass
the parent's `command_guard`, and cannot exceed `delegation.max_depth`.
See [Delegation](./delegation.md) for the full propagation model — the
short version is that delegation is *governed composition*, not a hole in
the policy fence.

## Real-world walkthrough: the governed-agent example

The [Governed Agent example](../examples/governed-agent.md) is the
canonical demonstration. The README lists prompts to try; each one
exercises a different governance layer.

| Prompt                                              | What fires                                          | Layer |
|-----------------------------------------------------|-----------------------------------------------------|-------|
| `"Read .harness/tools/self_check.md"`               | passes `path_guard`, `fs.read` succeeds             | 3 ✓   |
| `"Read /etc/passwd"`                                | `path_guard` blocks: absolute path                  | 3 ✗   |
| `"Delete the workdir folder"`                       | `tools_policy.deny` rejects `fs.remove` at registry | 2 ✗   |
| `"Run rm -rf / for me"`                             | `command_guard` blocks before syscall               | 3 ✗   |
| `"Register a new tool called exec_anything"`        | `meta_tool_guard` blocks the registration           | 3 ✗   |
| `"Fetch https://api.github.com/zen"` (no allowlist) | `web_fetch` runs; sandbox is permissive             | 4 ⚠   |
| same, with `allowed_domains=[example.com]`          | `SandboxError` — domain not allowed                 | 4 ✗   |

Three things to notice when you run this yourself:

1. **The model never sees the denied tools.** `fs.remove` is not in the
   tool list because `tools_policy` rejected it at registry time. The
   model cannot be "tricked" into calling something it never knew about.
2. **The reasons are user-facing.** `path_guard` and `command_guard`
   return strings explaining *which* rule fired, so the model can
   surface a useful refusal to the user instead of a generic "tool
   failed." Good governance is also good UX.
3. **Every refusal is in the metrics.** `audit.policy.deny`,
   `audit.meta.deny`, and the OTel `tool.policy=denied` span attribute
   make the policy posture observable. You can graph it.

Run it, break it on purpose, watch the spans. The example exists so the
governance story is something you *do*, not something you read.

## Designing your own governance posture

A practical checklist for going from "harness exists" to "harness is
governed":

1. **Pin `tools_policy: mode: allowlist`.** Implicit allow-by-default is
   the most common production footgun.
2. **Add the audit-everything hook pair first.** You cannot tune what you
   cannot measure. Two ~10-line files give you call rate, refusal rate,
   and per-tool counts.
3. **Stack guards at priority 10.** One hook per *category* of risk
   (commands, paths, network, data). Resist combining them into one
   mega-hook; the point of artifacts is that each file is a
   single-responsibility unit reviewers can reason about.
4. **Enforce channel narrowing.** Block raw built-ins (`exec.run`,
   ungoverned `fs.write`) so that all sensitive surfaces flow through
   named, audited tools.
5. **Wire `meta.register_tool` from day one** — even if you don't use
   self-augmentation yet. The hook is cheap insurance against future
   capability creep.
6. **Constrain delegation.** Set `delegation.max_depth` and
   `iterations_per_depth` deliberately. Open-ended sub-agent trees are
   the most common source of "why did this agent run for 40 minutes?"
   incidents.
7. **Bring in OS-level isolation when you go to prod.** Hooks are not a
   substitute for a non-privileged user, a read-only filesystem, and a
   network namespace. See the [Production Deployment](../guides/deployment.md)
   and [Network Sandboxing](../guides/network-sandbox.md) guides.

Treat this as a starting posture, not a final one. Governance is a
living artifact set; it should evolve with the agent and the threats
you're learning to care about.

## Anti-patterns

A few shapes that look reasonable in isolation but undermine the model:

- **A single "do all the things" hook.** It collapses the priority
  ladder, hides the policy from reviewers, and makes incident response
  harder. Split by responsibility.
- **Allow-list with a wildcard catch-all (`"*"`).** This is just
  default-allow with extra steps. If you need it briefly, leave a
  `TODO` and a deadline.
- **Hook logic that calls external services for policy decisions.**
  Hooks should be deterministic. Push that I/O into a tool with its own
  governance; let the hook consult cached state.
- **Self-augmentation without `meta_tool_guard`.** You have just handed
  the agent a back door into the registry.
- **Treating OTel as optional.** A governed agent without spans is a
  governed agent you cannot audit after the fact. Wire the collector
  even in dev.

## What to read next

- **[Governed Agent example](../examples/governed-agent.md)** — the
  flagship end-to-end profile this page references throughout.
- **[Production Deployment](../guides/deployment.md)** — systemd, Docker,
  and operator-level hardening.
- **[Network Sandboxing](../guides/network-sandbox.md)** — layer 4 in
  depth.
- **[Writing a Hook](../guides/writing-a-hook.md)** — go from blank file
  to merged policy.
- **Reference:** [`harness.md` Frontmatter](../reference/harness-md.md)
  documents every `tools_policy`, `meta`, and `delegation` field.

Governance in AI Harness is not a feature. It is the shape the
primitives take when you compose them honestly. Read the artifacts,
write the hooks, ship the policy in a PR — and the harness will hold the
line for you.
