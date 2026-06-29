# Observability with OpenTelemetry

> A hands-on tutorial. By the end of this guide you'll have a local
> OTel collector receiving traces from a running harness, you'll know
> the exact span tree every turn emits, you'll have trace-correlated
> JSON logs going to stdout, and you'll have a cost-per-turn signal
> you can alert on.

This guide assumes you finished the
[Production Deployment](./deployment.md) guide — or at least know how
to set `HARNESS_OTEL_ENDPOINT` and run the binary. Everything below
works the same whether the harness is invoked from a CLI, a systemd
unit, or a Docker container.

AI Harness reads `HARNESS_OTEL_*` variables by design (not ambient
standard `OTEL_*` SDK vars) so tracing behavior is explicit and isolated.

## Why observability is a first-class concern

Most harnesses treat tracing as a "wire up your own SDK" exercise.
AI Harness ships OpenTelemetry as a **runtime contract**: every
turn, every tool call, every delegation, every source event already
emits a span with stable attribute names. You don't add tracing — you
**turn it on**, and you can rely on the shape of what comes out.

That matters because the unit you actually want to reason about isn't
"a process" or "a request" — it's **a turn**. A turn fans out into
tool calls, sub-agent delegations, and hook decisions, and you need
all of those nested under one trace to answer questions like:

- Why did this turn take 12 seconds? (slow tool? slow model? slow
  delegate?)
- Which tool calls were denied by policy? (`tool.policy=denied`)
- How many tokens did this user consume today? (sum
  `turn.total_tokens` by service+session)
- Did the claims verifier pass, fail, or get skipped? (`delegation.verify_outcome`)

You answer those by querying spans, not by grepping logs.

---

## 1. Stand up a local collector

You don't need a SaaS vendor to start. The fastest path is the
upstream OTel collector in Docker, configured to log traces to stdout
so you can read them.

Create `otel-collector.yaml`:

```yaml
receivers:
  otlp:
    protocols:
      http:
        endpoint: 0.0.0.0:4318

exporters:
  debug:
    verbosity: detailed

service:
  pipelines:
    traces:
      receivers: [otlp]
      exporters: [debug]
```

Run it:

```bash
docker run --rm -p 4318:4318 \
  -v "$PWD/otel-collector.yaml:/etc/otel/config.yaml" \
  otel/opentelemetry-collector:latest \
  --config /etc/otel/config.yaml
```

Point the harness at it:

```bash
export HARNESS_OTEL_ENDPOINT=http://localhost:4318
export HARNESS_OTEL_SERVICE_NAME=ai-harness-dev
export HARNESS_OTEL_SAMPLE_RATIO=1.0
harness run --config ./harness.md "summarize the README"
```

Within a second or two, the collector's stdout should print a trace
with several spans. If nothing shows up, see [Troubleshooting](#troubleshooting).

> **Production swap:** the only thing that changes for production is
> the exporter section of the collector config — point it at Honeycomb,
> Tempo, Datadog, Jaeger, or whatever you already run. The harness
> side is identical.

---

## 2. The span tree (what every turn looks like)

Every interactive turn produces this nested span tree:

```
source.pump                           ← only when running `harness serve`
└── agent.turn                        ← one per user message
    ├── tool.call                     ← one per tool invocation
    ├── tool.call
    ├── delegation.execute            ← one per sub-agent dispatch
    │   └── agent.turn                ← the delegate's own turn (recursive)
    │       └── tool.call
    └── tool.call
```

Each layer is created by a different package:

| Span name | Emitted by | When |
|---|---|---|
| `source.pump` | `cmd/harness/serve.go` | One per event read from an input source (Telegram, HTTP, file watcher). |
| `agent.turn` | `agent/agent.go`, `agent/runstream.go` | One per `Agent.Run` / `Agent.RunStream` call. |
| `tool.call` | `tools/tools.go` | One per `Registry.Execute` call — denied calls also emit a span (with `tool.policy=denied`). |
| `delegation.execute` | `delegation/delegation.go` | One per `Delegator.Execute` call — claims verification appends `delegation.verify_outcome`. |

The nesting is automatic because each layer passes its context through
to the next. You never have to thread span context manually.

### Stable attribute names

These are part of the public contract. They are safe to alert on,
group by, and build dashboards against — they will not change without
a deprecation cycle.

**`agent.turn`** (`agent/agent.go:182-197`, `agent/runstream.go:51-65`):

| Attribute | Type | Meaning |
|---|---|---|
| `turn.index` | int | 1-based turn number within the agent's lifetime. |
| `turn.user_message_len` | int | Bytes of user input. |
| `turn.streaming` | bool | `true` for `RunStream`, absent for `Run`. |
| `turn.iterations` | int | How many model→tool round-trips the turn ran. |
| `turn.tool_calls` | int | Total tool calls in the turn. |
| `turn.prompt_tokens` | int | From provider usage. Zero for streaming today. |
| `turn.completion_tokens` | int | From provider usage. Zero for streaming today. |
| `turn.total_tokens` | int | Sum of the two above. |

**`tool.call`** (`tools/tools.go:205-237`):

| Attribute | Type | Meaning |
|---|---|---|
| `tool.name` | string | Tool name as registered. |
| `tool.call_id` | string | Model-assigned call ID — joins to logs. |
| `tool.is_error` | bool | `IsError` from the tool result. |
| `tool.policy` | string | `"denied"` when a policy rejected the call (otherwise unset). |

Span status is set to `Error` when `is_error=true` or when the
handler returned a Go error (the error is also recorded with
`span.RecordError`).

**`delegation.execute`** (`delegation/delegation.go:190-210`,
`delegation/delegation.go:491-501`):

| Attribute | Type | Meaning |
|---|---|---|
| `delegation.agent` | string | Named sub-agent (e.g. `code-reviewer`). |
| `delegation.depth` | int | Current delegation depth, enforced against `max_depth`. |
| `delegation.task_len` | int | Bytes of task instruction. |
| `delegation.model` | string | Resolved model after the agent registry lookup. |
| `delegation.tools_count` | int | Number of tools the delegate had access to. |
| `delegation.tool_calls` | int | Tool calls the delegate made. |
| `delegation.verify_outcome` | string | `passed`, `failed`, or `skipped` from the claims verifier. |

**`source.pump`** (`cmd/harness/serve.go:218-223`):

| Attribute | Type | Meaning |
|---|---|---|
| `source.name` | string | Source artifact's `name`. |
| `source.event.session_key` | string | Stable key used to route to a session worker. |
| `source.event.text_len` | int | Bytes in the inbound message. |

That's the whole contract. Anything else you see on a span (resource
attributes, instrumentation scope) comes from the OTel SDK defaults
and is the same as any other Go service.

---

## 3. Trace-correlated logs

The harness logger automatically injects `trace_id` and `span_id`
into every log record that runs inside a span. That's done by a
`slog.Handler` middleware (`harness/trace.go:175-198`) that wraps the
log handler `NewLogger`/`NewLoggerWithTrace` returns.

Turn on JSON logs so you can pipe them to a log shipper:

```bash
export HARNESS_LOG_FORMAT=json
export HARNESS_LOG_LEVEL=info
harness run --config ./harness.md "what changed in main yesterday?"
```

A typical record looks like:

```json
{
  "time": "2026-06-15T03:21:14.882Z",
  "level": "INFO",
  "msg": "tool call complete",
  "tool": "git_log",
  "iteration": 2,
  "trace_id": "9a7d0d8e7d6f4b2a1c5e6f8a9b0c1d2e",
  "span_id": "0123abcd4567ef89"
}
```

The `trace_id` is the same one the OTel collector saw. That's the
join key — in Tempo/Honeycomb/Datadog, click a slow `agent.turn` span
and pivot directly to the matching log lines, no separate query
required.

### Log levels in practice

| Level | Use for |
|---|---|
| `error` | Production default for noisy multi-tenant deploys. You'll still get tool/turn failures via OTel span status. |
| `warn` | Sensible production default for most agents — surfaces blocked hooks and verification failures without per-iteration chatter. |
| `info` | Default for development. One line per turn-start, tool-call-complete, delegation-complete. |
| `debug` | Triaging. Adds per-iteration model request/response shape, hook dispatch fan-out, and artifact condition evaluation. Expect high volume. |

`HARNESS_LOG_LEVEL=debug` plus a fully sampled tracer
(`HARNESS_OTEL_SAMPLE_RATIO=1.0`) is the canonical "I'm debugging a
weird turn" setup. Turn both down before going to production.

---

## 4. Cost telemetry

Token counts are already on every `agent.turn` span — that's enough
for a cost dashboard:

```promql
# Tokens per turn over the last hour, by service.
sum by (service_name) (
  rate(span_attribute_turn_total_tokens_total{span_name="agent.turn"}[1h])
)
```

(The exact metric name depends on your collector's
`spanmetrics`/`attributes` processor configuration; the point is the
attributes are already there, you don't have to instrument anything.)

To turn tokens into dollars, the harness ships a small `CostTracker`
helper in the evals package (`evals/cost.go`):

```go
import "github.com/htekdev/ai-harness/evals"

ct := &evals.CostTracker{}
ct.Add(result.Usage.TotalTokens)
log.Info("turn cost",
    "tokens", ct.TotalTokens(),
    "usd",    ct.EstimatedUSD(),
)
```

`CostTracker` uses a single **blended** price-per-million-tokens
constant (`evals.BlendedPricePerMillion`, currently `0.40`, tuned for
`gpt-4o-mini`). It is **intentionally a rough estimate**:

- It doesn't separate input vs output tokens (`InputPricePerMillion`
  and `OutputPricePerMillion` are exported if you need precision).
- It doesn't know which provider/model actually served the turn.
- It rounds aggressively.

That's a deliberate choice — the tracker is the eval budget cap
(`BudgetCapUSD` in `evals/runner.go`), not your billing system.
**For real cost attribution, do the math on the raw token attributes
in your OTel backend** (or your provider's usage API), where you can
multiply per-model with the actual current pricing.

If you want a turn-level cost signal in OTel itself, the simplest
hook is a `turn.end` hook that reads `turn.total_tokens`, multiplies
by your blended rate, and writes a custom attribute on the active
span:

```python
# .harness/hooks/cost-attribution.md  (Starlark hook)
when: event == "turn.end"
script: |
    def handle(event, payload):
        tokens = payload.get("total_tokens", 0)
        # Per-million-token blended rate; tune per-model.
        usd = tokens * 0.40 / 1_000_000
        return {"action": "annotate", "attributes": {
            "turn.cost_usd_estimate": usd,
        }}
```

Now your `agent.turn` spans carry a `turn.cost_usd_estimate` you can
sum, alert on, and slice by session_key.

---

## 5. Sampling and verbosity

Default sampling is `1.0` — every turn is exported. That's the right
default for development and low-traffic production. Two situations
warrant turning it down:

**High-volume sources.** A `serve` deployment polling a chat with
thousands of messages an hour will dwarf your collector. Drop the
sample ratio:

```bash
HARNESS_OTEL_SAMPLE_RATIO=0.1   # keep 10% of traces
```

Sampling is **TraceIDRatioBased** (`harness/trace.go:139`), so once a
trace is in, every span in it is in — you never get half a turn.

**Sub-agent fan-out.** If a parent agent delegates aggressively, you
can keep parent-only sampling by setting `HARNESS_OTEL_SAMPLE_RATIO`
to `1.0` on the parent and `0.0` (off) on delegates. In practice
most users keep both on at the same ratio and rely on the trace tree
for correlation.

Always pair sampling with a sane log level — `HARNESS_LOG_LEVEL=info`
on a sampled deploy stays manageable; `debug` doesn't.

---

## 6. End-to-end smoke test

Use this checklist after wiring observability in any new environment.
All five must pass.

1. **Collector sees an `agent.turn` span** after a single
   `harness run` invocation. (If not: check `HARNESS_OTEL_ENDPOINT`
   is reachable from inside the container/host where the harness
   runs, not from your laptop.)
2. **The span has `turn.total_tokens > 0`** (non-streaming) or
   `turn.streaming=true` (streaming).
3. **Tool calls appear as `tool.call` children** with `tool.name`
   matching what your harness actually called.
4. **A log line with `trace_id` set** appears at the same time, and
   that trace ID matches the span. (`HARNESS_LOG_FORMAT=json` makes
   this trivial to verify with `jq`.)
5. **Shutdown flushes cleanly**: send `SIGINT` and confirm no
   `dropped spans` warnings in the collector. The harness defers
   `ShutdownTracer` on exit (`harness/trace.go:84-92`) — if you've
   embedded it in your own binary, do the same.

---

## Troubleshooting

| Symptom | Likely cause | Fix |
|---|---|---|
| No spans at all. | `HARNESS_OTEL_ENDPOINT` is unset or unreachable. Tracing is **disabled by default**. | Set the env var; verify the URL resolves *from the harness process*, not from your shell. |
| `invalid HARNESS_OTEL_PROTOCOL` error at startup. | Only `http` is supported in v1. | Unset the variable or set it to `http`. gRPC support is reserved for v2. |
| `invalid HARNESS_OTEL_SAMPLE_RATIO` error at startup. | Value isn't a float in `[0,1]`. | Use `0`, `1`, or a decimal like `0.1`. |
| Logs have no `trace_id`. | A custom logger replaced `NewLogger`/`NewLoggerWithTrace` without re-wrapping with `TraceContextHandler`. | Wrap your `slog.Handler` with `harness.NewTraceContextHandler(...)` before installing it. |
| Spans land but no `agent.turn` — only `source.pump`. | A hook is blocking the turn before `Agent.Run` opens its span. | Check `turn.start` hooks. A `{"action": "block"}` aborts before the turn span is created — by design. |
| Trace cuts off after a `delegation.execute` error. | The error path records the error and ends the span; child spans only appear if the delegate actually started. | Check `delegation.depth` against `max_depth`, and your agent resolver. |
| Tokens always zero on `agent.turn`. | You're using `RunStream`. Streaming providers don't return usage. | Switch to `Run` for cost-critical workloads, or compute tokens from the streamed deltas. |

---

## Going further

- **`harness.md` frontmatter:** `--otel-*` flags can be passed
  directly to `harness run`/`harness serve` — they override env, and
  env overrides the built-in defaults (`harness/trace.go:98-103`).
- **Custom spans from your tools/hooks:** call
  `harness.Tracer().Start(ctx, "my-tool.work")` — the tracer respects
  the same noop-by-default contract, so adding spans to your own
  code is zero-cost when tracing is off (`harness.md:283`).
- **Production deployment recipes:** the
  [Production Deployment](./deployment.md) guide wires all of the
  above into systemd and Docker Compose units that load
  `harness.env` and survive restarts.

You now have the full observability story: span tree, attributes, log
correlation, cost signal, sampling. Everything else is dashboard work
in your OTel backend.
