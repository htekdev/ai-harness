# Long-Running Primitives

## Goal

Define a minimal, extensible set of primitives for long-running behavior in AI Harness without turning the core into a workflow engine. The primitives must keep **Harness as Code** intact:

- everything is defined declaratively in YAML
- built-ins are the execution layer
- named tools wrap built-ins instead of embedding shell snippets everywhere
- harnesses are **self-defining**, not baseline extensions
- every change is fast to validate

## Design Constraints

1. **YAML is the DSL.** No custom language beyond the existing expression architecture.
2. **Definitions are data.** Events, triggers, watchers, schedules, runtimes, tools, hooks, and sub-agents are all harness artifacts.
3. **Built-ins execute.** The runtime exposes durable capabilities like `events.append`, `runtime.start`, `shell.exec`, and `schedule.scan`; artifacts compose them.
4. **State must be replayable.** Current views should be rebuildable from persisted facts.
5. **Cron is enabled, not embedded.** AI Harness should persist schedule intent and due events, while allowing an external scanner/daemon to drive wake-ups.

## Core Primitive Model

### 1. Events

Events are the persistence backbone. They are append-only facts with stable envelopes.

```yaml
events:
  store:
    kind: jsonl
    path: .harness/state/events.jsonl
  streams:
    - name: runtime
    - name: schedule
    - name: artifact
```

Recommended envelope:

```yaml
id: evt_01JV...
stream: runtime/indexer
type: runtime.started
source: runtime-controller
time: 2026-05-21T17:00:00Z
sequence: 42
data:
  lease_owner: scanner-1
meta:
  harness: docs-indexer
```

Rules:

- event names should describe facts (`runtime.started`, `file.changed`, `schedule.due`)
- events are written through built-ins, not ad hoc file mutation
- projections and triggers consume events; they do not become alternate sources of truth

### 2. Triggers

Triggers match events and request follow-up actions.

```yaml
triggers:
  - name: reindex-on-doc-change
    when:
      type: file.changed
      where: data.path.endsWith(".md")
    then:
      - uses: events.append
        with:
          stream: artifact/docs
          type: artifact.reindex.requested
          data:
            path: "{{ data.path }}"
      - uses: runtime.start
        with:
          runtime: docs-indexer
```

Rules:

- triggers are declarative rules, not imperative workers
- trigger outputs should stay primitive: append an event, request a runtime, enqueue a due record, invoke a named tool
- recursion protection and idempotency belong in the trigger executor, not in each trigger definition

### 3. Watchers

Watchers observe an external or internal source and emit events when something changes. They are specialized runtimes, not a separate persistence model.

```yaml
watchers:
  - name: docs-tree
    kind: fs
    path: docs
    include:
      - "**/*.md"
    emits:
      stream: artifact/docs
      type: file.changed
      data:
        path: "{{ watch.path }}"
        op: "{{ watch.op }}"
    runtime:
      restart: always
      checkpoint: cursor
```

Rules:

- watchers do not mutate harness state directly
- watchers produce facts; triggers and projections react to those facts
- watcher progress is checkpointed through the runtime layer

### 4. Schedules

Schedules are durable definitions plus next-due state. They make cron possible without baking cron into the core.

```yaml
schedules:
  - name: nightly-evals
    kind: cron
    expression: "0 2 * * *"
    timezone: America/Chicago
    emits:
      stream: schedule/nightly-evals
      type: schedule.due
      data:
        workflow: nightly-evals
```

Rules:

- schedules are data, not daemon configuration
- the harness persists schedule definitions and next due times
- any scanner (CLI command, sidecar, systemd timer, GitHub Action) may call `schedule.scan` and emit due events

### 5. Runtimes

Runtimes represent durable work that can outlive a single agent turn: watchers, background delegates, scanners, or persistent processes.

```yaml
runtimes:
  - name: docs-indexer
    kind: process
    command:
      uses: shell.exec
      with:
        command: ["npm", "run", "index-docs"]
    lifecycle:
      desired: running
      lease_ttl: 30s
      checkpoint_every: 15s
```

Runtime facts should include:

- `runtime.requested`
- `runtime.started`
- `runtime.heartbeat`
- `runtime.checkpointed`
- `runtime.failed`
- `runtime.stopped`

Rules:

- a runtime is more than a PID; it has desired state, lease ownership, heartbeat, and checkpoint data
- another worker should be able to reconcile or resume a runtime from persisted state
- watchers and scanners are just runtime kinds with different adapters

### 6. Named Tools Over Built-ins

Built-ins stay low-level; harness authors expose friendly, domain-specific tools.

```yaml
tools:
  - name: run-build
    description: Run the docs build and stream structured events
    uses: shell.exec
    with:
      command: ["npm", "run", "build"]
    emits:
      start: tool.run-build.started
      success: tool.run-build.succeeded
      failure: tool.run-build.failed
```

This keeps the execution layer stable while letting agents learn ergonomic tool names.

## Recommended Harness Shape

A long-running harness should be expressible as one artifact graph:

```yaml
harness:
  context: {}
  hooks: []
  tools: []
  subagents: []
  events: {}
  triggers: []
  watchers: []
  schedules: []
  runtimes: []
```

That model reinforces the core abstraction Hector called out:

> A harness = context + hooks + sub-agent orchestration + execution artifacts.

## Validation Loop

Long-running primitives only work if authors can validate changes immediately.

Recommended validation surface:

- `harness validate` — schema + reference validation
- `harness events replay` — rebuild projections deterministically
- `harness schedule scan --at <time>` — dry-run due events
- `harness runtime inspect <name>` — lease/checkpoint visibility
- eval fixtures proving watcher/trigger/runtime interactions

## Execution Boundary

The harness definition layer should answer **what exists** and **what should happen**.
The built-in/runtime layer should answer **how it runs**.

That means:

- YAML defines schedules, watchers, triggers, runtimes, and named tools
- Go built-ins implement append/replay/subscribe, process supervision, scheduling scans, and checkpoint persistence
- the agent loop consumes the same artifacts rather than special-casing background behavior

## Proposed Rollout

1. **Events first** — append, replay, subscribe, file-backed persistence
2. **Triggers second** — declarative event matching and action adapters
3. **Runtimes third** — lease, heartbeat, checkpoint, recovery
4. **Watchers + schedule scanners next** — adapters on top of runtimes and events
5. **Reference implementation** — encode Copilot CLI concepts as harness artifacts to prove the surface area is expressive enough

## Bottom Line

The minimal long-running primitive set is:

- **events** for persistence
- **triggers** for reaction
- **watchers** for observation
- **schedules** for durable wake-up intent
- **runtimes** for leased long-lived work
- **built-ins + named tools** for execution

That keeps AI Harness declarative, replayable, and extensible without turning YAML into a new programming language or forcing a heavyweight orchestrator into the core.
