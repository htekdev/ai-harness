# Long-Running Primitives v1

## Goal

Give AI Harness a minimal, composable foundation for persistence and long-running work **without** baking in a cron engine, a queue system, or a heavyweight workflow runtime.

## Research Signals

- **OpenAI Agents SDK** treats the agent loop, sessions, and streamed run events as first-class, but expects durable orchestration and scheduling to live outside the core runtime. Sessions provide built-in conversation memory across runs, and `Runner.run_streamed()` exposes event streams while the run is happening. Sources: [Sessions](https://openai.github.io/openai-agents-python/sessions/), [Running agents](https://openai.github.io/openai-agents-python/running_agents/).
- **Model Context Protocol** separates callable tools from long-lived notification streams. `subscriptions/listen` is a durable-ish watcher primitive at the protocol layer: open a stream, receive notifications until canceled. Source: [MCP Subscriptions](https://modelcontextprotocol.io/specification/draft/basic/utilities/subscriptions).
- **Pi agent core** leans on a stateful agent plus event streaming rather than a grab-bag of special-case async APIs. Source: [pi-agent-core README](https://github.com/badlogic/pi-mono/blob/main/packages/agent/README.md).

## Recommendation

The right abstraction is **not** a raw "start background process" primitive.

The right abstraction is:

1. **A durable event stream** as the single persistence primitive
2. **Materialized state views** rebuilt from that stream
3. **Triggers** that match events and emit actions
4. **Runtimes** that represent leased, long-running workers/processes/watchers
5. **Schedules** as durable wake-up records that an external scanner can honor

This keeps the core minimal while making cron, watchers, hooks, and background execution possible.

## Why Events Should Be First-Class

A persistent event log gives AI Harness one primitive that solves multiple problems:

- **Persistence**: facts survive process restarts
- **Replay**: rebuild current state by replaying prior facts
- **Observability**: inspect exactly what happened
- **Triggers**: match on facts instead of polling ad hoc mutable state
- **Watchers**: emit facts when observed state changes
- **Scheduling**: persist future wake-ups as facts, then emit due events later

### Event shape

```go
type Event struct {
    ID       string
    Stream   string // session/<id>, runtime/<id>, artifact/<id>, schedule/<id>
    Type     string // runtime.started, file.changed, trigger.fired, timer.due
    Source   string // hook/tool/watcher/runtime/user
    Time     time.Time
    Sequence uint64
    Data     json.RawMessage
    Meta     map[string]string
}
```

### Required operations

- `Emit` / `Append`
- `Subscribe`
- `Replay`
- `ReadStream`

## Proposed Primitive Stack

### 1. Event Store

**Responsibility:** append-only source of truth.

**Owns:**
- ordered event persistence
- stream partitioning
- replay
- in-process subscriptions

**Does not own:**
- cron parsing
- queue retries
- external watcher logic
- process supervision policy

### 2. Projections

**Responsibility:** derive current state from events.

Examples:
- runtime registry (`runtime/<id>` streams)
- artifact state
- schedule table
- trigger stats / observability views

A projection is disposable. If corrupted, rebuild it from replay.

### 3. Triggers

**Responsibility:** declaratively say **when event X happens, do Y**.

A trigger is a rule:

```yaml
on:
  type: file.changed
  stream_prefix: artifact/
  where: payload.path endsWith ".md"
do:
  - emit: artifact.reindex.requested
  - start_runtime: indexer
```

Trigger outputs should stay primitive:
- emit a new event
- invoke a tool/hook adapter
- start or wake a runtime
- create/update a durable wake-up record

### 4. Runtimes

**Responsibility:** represent long-running workers that outlive one agent turn.

A runtime is **not just a PID**. It is a durable record with lease semantics:

```go
type RuntimeHandle struct {
    ID            string
    Kind          string // process, delegate, watcher, scanner
    DesiredState  string // running, paused, stopped
    LeaseOwner    string
    LeaseUntil    time.Time
    LastHeartbeat time.Time
    Checkpoint    json.RawMessage
}
```

Lifecycle is evented:
- `runtime.requested`
- `runtime.started`
- `runtime.heartbeat`
- `runtime.checkpointed`
- `runtime.failed`
- `runtime.stopped`

This is the primitive that enables background delegates, watcher loops, and external daemons.

### 5. Watchers

**Responsibility:** observe some source of truth and emit events.

Watchers should **not** be a separate persistence model. They are just specialized runtimes that produce events.

Examples:
- file watcher → emits `file.changed`
- process watcher → emits `process.exited`
- projection watcher → emits `schedule.due`
- external webhook bridge → emits `webhook.received`

### 6. Scheduling Enablement

AI Harness should **not** ship a cron engine in the core.

Instead, it should ship the primitive that makes cron possible:

- durable schedule definitions (`schedule.created`, `schedule.updated`, `schedule.deleted`)
- a projection that computes next due times
- due events (`timer.due`) emitted by any external scanner/runtime

This lets users plug in:
- cron
- a hosted scheduler
- a systemd timer
- a GitHub Action
- a sidecar daemon

without changing the harness core.

## Core Design Rules

1. **Events are facts, not commands.** Use past-tense or state-change naming where possible.
2. **Triggers do not mutate state directly.** They emit events or request runtime actions.
3. **Watchers are producers.** They observe, then emit.
4. **Runtimes heartbeat and checkpoint.** If a process dies, another worker can resume or reconcile.
5. **Schedules are data.** A scheduler is just one possible consumer of schedule state.
6. **Current state is always rebuildable from replay.**

## Why This Beats the Alternatives

### Raw background-process primitive only
Too low-level. It gives no replay, no observability, no scheduler story, and no durable coordination surface.

### Watchers as the main primitive
Too narrow. Watchers explain change detection but not persistence, replay, state rebuild, or background delegates.

### Triggers as the main primitive
Too reactive. Triggers need a durable source of truth underneath them.

### Persistent coordination mechanism without events
Usually turns into hidden mutable state that is hard to inspect, replay, or reason about.

## Incremental Rollout

### Phase 1 — Event Store (foundational)
- append-only store
- stream filtering
- replay
- in-process subscribe
- file-backed persistence

### Phase 2 — Projections
- runtime projection
- schedule projection
- artifact projection
- snapshot/rebuild helpers

### Phase 3 — Triggers
- event matcher
- action adapters
- loop protection / recursion guards

### Phase 4 — Runtimes
- runtime registry
- lease + heartbeat model
- checkpoint API
- recovery semantics

### Phase 5 — Scheduler Adapters
- due-event scanner
- cron adapter package outside core
- file/process watcher adapters

## Initial API Sketch

```go
type Store interface {
    Append(ctx context.Context, event Event) (Event, error)
    Replay(ctx context.Context, stream string, fn func(Event) error) error
    Subscribe(ctx context.Context, stream string) (<-chan Event, func())
}

type Projector interface {
    Apply(Event) error
    Reset()
}
```

## Immediate Implementation Recommendation

Build **Phase 1** now:

- Add an `events` package
- Make it file-backed and append-only (JSONL is enough to start)
- Support `Append`, `Replay`, and `Subscribe`
- Treat it as the canonical primitive that later powers triggers, runtimes, watcher adapters, and schedule projections

That gives AI Harness a real persistence backbone without prematurely committing to a heavyweight workflow engine.
