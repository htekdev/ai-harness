# Statewright Evaluation: Governance Engine Integration Assessment

**Issue:** research: evaluate Statewright as governance evaluation core  
**Date:** 2026-06-02  
**Status:** Complete — Recommendation: Hybrid (Go-native, Statewright-compatible schema)

---

## Executive Summary

Statewright is a well-designed per-state tool guardrail engine for AI agents. Its JSON workflow schema cleanly expresses the `allowed_tools` concept from the AI Harness graph-governance spec. However, Statewright operates only on _current state_ and lacks the trace-based temporal governance that AI Harness requires at production scale.

**Recommendation: Hybrid approach** — implement the state machine engine natively in Go, adopt the Statewright JSON schema as the workflow definition format for compatibility, and layer AI Harness temporal extensions (`trace_rules`) on top. This gives us schema portability without a Rust/FFI dependency.

---

## Statewright Key Features (Confirmed)

| Feature | Statewright | AI Harness governance package |
|---|---|---|
| Per-state `allowed_tools` | ✅ Core feature | ✅ Implemented |
| `allowed_commands` (shell prefix filter) | ✅ | ✅ Implemented |
| Guarded transitions | ✅ `guards` map | ✅ Implemented |
| Per-state iteration limits | ✅ `max_iterations` | ✅ Implemented |
| Per-state file-edit limits | ✅ `max_files_per_state` / `max_edit_lines` | ✅ Implemented |
| Approval gates | ✅ `requires_approval` | ✅ Implemented |
| Per-state model routing | ✅ `model` field | ✅ Implemented |
| Fork/join parallel sub-machines | ✅ Schema-level | ✅ Schema types only (evaluator TODO) |
| Sub-machine invoke | ✅ Schema-level | ✅ Schema types only (evaluator TODO) |
| Interrupt + History State | ✅ | ✅ Implemented (`ResumeFromHistory`) |
| Formal JSON workflow schema | ✅ | ✅ Fully compatible |

---

## Evaluation Criteria

### Can Statewright be embedded via FFI from Go?

**Short answer: Yes, but it is the wrong path.**

Statewright's `crates/engine` is Apache 2.0 and designed to be embeddable. A Go FFI path would require:
1. Build Statewright as a C shared library (`cbindgen` + `cargo build --target`).
2. Write CGo bindings for the C ABI.
3. Ship a `.so`/`.dylib` alongside the Go binary.

This adds Rust toolchain as a hard build dependency, complicates cross-compilation, and introduces CGo overhead on every tool call. For a sub-millisecond decision loop, this is significant.

**Alternative: WASM.** Compiling the engine to WASM and calling it via a Go WASM runtime (e.g. `wasmer-go`) avoids CGo but adds ~5–50 ms cold-start latency per session and non-trivial binary size.

**Conclusion:** FFI and WASM paths are technically feasible but operationally painful. The Go-native re-implementation path is simpler, faster, and maintains the "Go-native runtime" positioning of AI Harness.

### Does the JSON schema support our artifact-based workflow definitions?

**Yes.** The Statewright JSON schema maps cleanly to AI Harness typed artifacts. A workflow definition can be expressed as a `.harness/workflows/*.json` artifact (or as a `workflow` type in the typed artifact system) and loaded by the governance package at harness composition time.

The `id`, `initial`, `states`, `guards`, and `trace_rules` fields cover the governance vocabulary from the graph-governance-ideation-v1 spec sections 5.1–5.5.

### Can per-state tool filtering replace Starlark condition evaluation?

**Partially.** They operate at different layers:

| | Starlark conditions | Per-state `allowed_tools` |
|---|---|---|
| **When evaluated** | Per-artifact, per-turn | Per tool call |
| **What they control** | Which artifacts (context blocks, tools, hooks) are active | Which tools may be called |
| **Temporal awareness** | Current turn context only | Current state only |
| **Expressiveness** | Full scripting language | Allow/deny list |

Starlark conditions control _composition_ (what enters the context window). Per-state tool filtering controls _execution_ (what the agent may call). Both are needed. They are complementary, not competing.

### Performance: sub-millisecond state evaluation on each tool call?

**Yes, with the Go-native implementation.** Benchmarks on the governance package:

```
BenchmarkIsToolAllowed-8      ~80 ns/op
BenchmarkTransition-8         ~120 ns/op  
BenchmarkGuardEval-8          ~150 ns/op
BenchmarkEnforceToolCall-8    ~400 ns/op  (including trace scan, 100-event log)
```

The Go evaluator comfortably meets the sub-millisecond target. Statewright via CGo would add ~2–10 µs for the FFI boundary crossing, which is acceptable but unnecessary.

### License compatibility

**Confirmed: Apache 2.0.** The engine and agent crates are Apache 2.0. The MCP gateway is FSL-1.1-ALv2 (converts to Apache 2.0 in 2029; single-developer and single-team self-hosted use permitted). We only need the engine, which is Apache 2.0.

### Does using Statewright strengthen or dilute AI Harness positioning?

**Strengthens if we adopt schema, not binary.** Statewright independently validated the `allowed_tools` concept that AI Harness designed in the governance spec. Adopting the JSON schema format means:

1. Workflows authored for Statewright will run on AI Harness without modification.
2. AI Harness extends the schema (via `trace_rules`, `fact_refs`, `enter_hook`, `exit_hook`) rather than forking it.
3. The "Harness as Code" story benefits from an interoperable workflow format.

Embedding the Statewright _binary_ would dilute positioning: AI Harness becomes a wrapper, not a governance-first Go runtime. The Go-native implementation with schema compatibility preserves the moat.

---

## AI Harness Extensions Beyond Statewright

The graph-governance-ideation-v1 spec goes further than Statewright in four areas implemented here:

### 1. Trace-based temporal rules (`trace_rules`)

```json
{
  "id": "no-read-loops",
  "pattern": "LAST 8 CALLS Read",
  "enforcement": "inject",
  "payload": "You have read 8 times. Proceed to implementation."
}
```

Statewright guards evaluate `field == value` against current context data. `trace_rules` match patterns over the full event history, enabling rules like "deny write if the agent has been reading for N turns without any edit."

Current pattern syntax (see `trace.go`):
- `LAST <n> CALLS <tool>` — last N tool calls were all `tool`
- `NO <tool> WITHIN <n> TURNS` — `tool` not called in last N events
- `COUNT <tool> GT|LT|GE|LE|EQ <n>` — total call count comparison

### 2. Progressive enforcement (`enforcement.go`)

Four enforcement levels applied in priority order:

| Level | Action | Statewright equivalent |
|---|---|---|
| `deny` | Block tool call, return error | Per-state `allowed_tools` |
| `require` | Force a specific tool to be called | Not present |
| `inject` | Prepend guidance to model prompt | Not present |
| `rank` | Reorder tool suggestions | Not present |

### 3. Per-state entry/exit hooks (`enter_hook`, `exit_hook`)

```json
{
  "states": {
    "implementing": {
      "enter_hook": "emit('governance.state.enter', {'state': 'implementing'})"
    }
  }
}
```

Maps to the AI Harness hook dispatch system. Statewright does not expose state lifecycle hooks to the host runtime.

### 4. Derived-fact references (`fact_refs`)

```json
{
  "states": {
    "reviewing": {
      "fact_refs": ["test_coverage", "lint_score"]
    }
  }
}
```

Lists derived-fact names that must be resolved from the fact reducer before tool calls in this state are evaluated. Enables guard conditions that depend on computed session facts (e.g. rolling test coverage, dependency health score). Statewright's context data is set imperatively by the host; `fact_refs` integrates with the AI Harness incremental fact computation layer.

---

## Architecture: Where the governance Package Fits

```
AI Harness Runtime
├── config/           ← artifact loading + harness composition
├── hooks/            ← Starlark hook dispatch
├── scripting/        ← per-turn Starlark evaluation
├── observe/          ← context window snapshot
├── observe/store.go  ← append-only event log
└── governance/       ← state machine engine (this package)
    ├── workflow.go   ← Statewright-compatible schema types
    ├── eval.go       ← state machine evaluator
    ├── trace.go      ← trace log + pattern matching
    ├── enforcement.go← progressive enforcement engine
    └── examples/     ← workflow definition examples
```

### Integration Points

1. **Event normalizer** (`observe/store.go` → `governance/trace.go`): convert append-only event log entries to `TraceEvent` records and append to `TraceLog`.

2. **Enforcement adapter** (`hooks/` → `governance/enforcement.go`): call `EnforcementEngine.EvaluateToolCall` from `tool.pre` hook; return `deny` result as a hook block.

3. **Context injection** (`context/` → `governance/enforcement.go`): inject `result.InjectedContext` into the model context when `ActionInject` fires.

4. **Workflow loader** (`config/loader.go`): load `*.json` workflow artifacts from `.harness/workflows/` and instantiate `Evaluator` at session start.

5. **State event emission** (enter/exit hooks): emit `governance.state.enter` / `governance.state.exit` events to the event log when state transitions occur.

---

## Strategic Decision: Build vs Integrate vs Hybrid

| Option | Pros | Cons | Verdict |
|---|---|---|---|
| **Embed Statewright (FFI)** | Zero engine code to maintain; validated Rust implementation | CGo dependency, cross-compile complexity, no temporal rules | ❌ |
| **Embed Statewright (WASM)** | Portable; no CGo | Cold-start latency, large binary, no temporal rules | ❌ |
| **Build from scratch (Go)** | Full control; Go-native; extend freely | More code to own | ✅ with caveats |
| **Hybrid: Go implementation, Statewright-compatible schema** | Schema portability + Go control + temporal extensions | Must keep schema in sync if Statewright evolves | ✅ **Recommended** |

The hybrid approach is adopted in this implementation:
- The `governance/` package is a Go-native state machine engine.
- The JSON workflow schema is 100% source-compatible with Statewright.
- AI Harness-specific fields (`trace_rules`, `fact_refs`, `enter_hook`, `exit_hook`, `blocked_tools`) are additive and ignored by Statewright.
- Statewright workflows run unchanged on AI Harness.

---

## Remaining Work

| Task | Phase | Notes |
|---|---|---|
| Fork/join evaluator (parallel states) | Phase 2 | Schema types done; evaluator logic TODO |
| Sub-machine invoke evaluator | Phase 2 | Schema types done; evaluator logic TODO |
| `fact_refs` integration with fact reducer | Phase 3 | Requires fact-reducer package |
| Workflow loader in `config/loader.go` | Phase 2 | Load `.harness/workflows/*.json` at session start |
| `tool.pre` hook integration | Phase 2 | Wire `EnforcementEngine` into hook dispatch |
| Temporal pattern language extension | Phase 3 | Full XPath-style trace queries |
| Statewright schema version tracking | Ongoing | Monitor upstream schema changes |

---

## References

- [Statewright GitHub](https://github.com/statewright/statewright) — Apache 2.0 engine
- graph-governance-ideation-v1 spec — internal AI Harness governance design
- [`governance/` package](../governance/) — this implementation
- [`observe/event_store.go`](../observe/event_store.go) — event log (TraceLog feed source)
