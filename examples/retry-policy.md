# Classification-Driven Retry Policy

**Phase 5.3 example.** Shows how the typed error taxonomy in
`harness/errs` lets callers implement a retry policy that reacts to *what
kind* of failure occurred — without parsing message text.

## Why typed errors

Pi and Copilot CLI extensions surface errors as raw strings: callers
cannot programmatically distinguish a tool-handler failure from a
config-load failure from a transient provider outage. AI Harness lifts
classification into a first-class runtime contract via
[`harness/errs`](../harness/errs/errors.go):

| Kind           | Meaning                                              | Retriable? |
|----------------|------------------------------------------------------|------------|
| `KindConfig`      | Config / artifact loading / validation failed     | ❌         |
| `KindTool`        | Tool registry lookup or handler execution failed  | ❌         |
| `KindCompletion`  | Provider / completion call failed                 | ✅ (often) |
| `KindDelegation`  | Sub-agent delegation failed                       | depends    |
| `KindSource`      | Input source (stdin/telegram/meshwire) failed     | ✅ (often) |
| `KindPersistence` | Offset store / event log / session I/O failed     | depends    |

Retriability is independently tracked: hot paths that wrap transient
failures use `errs.Retriable(...)` so callers can check
`errs.IsRetriable(err)` without re-classifying.

## Canonical retry pattern

```go
import (
    "context"
    "time"

    "github.com/htekdev/ai-harness/harness/errs"
)

// callWithRetry retries only when the underlying error is BOTH a completion
// failure AND marked retriable. Config/tool/delegation failures fail fast.
func callWithRetry(ctx context.Context, do func(context.Context) error) error {
    const maxAttempts = 3
    backoff := 250 * time.Millisecond

    var lastErr error
    for attempt := 1; attempt <= maxAttempts; attempt++ {
        err := do(ctx)
        if err == nil {
            return nil
        }
        lastErr = err

        // Only completion-kind, retriable errors warrant another attempt.
        if errs.KindOf(err) != errs.KindCompletion || !errs.IsRetriable(err) {
            return err
        }

        // Don't sleep after the final attempt.
        if attempt == maxAttempts {
            break
        }
        select {
        case <-ctx.Done():
            return ctx.Err()
        case <-time.After(backoff):
            backoff *= 2
        }
    }
    return lastErr
}
```

## What each branch does

- **`KindConfig` / `KindTool`** — fail fast. Re-running won't fix a
  malformed harness.md or a missing tool registration.
- **`KindCompletion` (retriable)** — exponential backoff. Provider
  flakes (5xx, rate limits, transient network) recover quickly.
- **`KindDelegation`** — depends on the wrapped cause. Walk the chain
  with `errors.As` if you want sub-policies.
- **`KindPersistence`** — usually fail fast unless you've explicitly
  marked the error retriable (e.g. file lock contention).

## Why this beats string matching

Without typed errors, the same retry policy would have to grep error
messages — fragile, locale-sensitive, and silently broken whenever a
provider rewords its 500 page. With `errs`, every harness subsystem
publishes a stable `Kind` that hooks, dashboards, and operator tooling
can rely on.

## See also

- `harness/errs/errors.go` — the taxonomy itself
- `harness/errs/errors_test.go` — `KindOf` / `IsRetriable` semantics
- `harness/errs/retry_policy_test.go` — executable version of this
  example, run with `go test ./harness/errs/`
