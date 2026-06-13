package errs_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/htekdev/ai-harness/harness/errs"
)

// callWithRetry is the canonical classification-driven retry policy.
// It mirrors examples/retry-policy.md so the example stays executable.
//
// Only completion-kind, retriable errors warrant another attempt; config /
// tool / delegation failures fail fast.
func callWithRetry(ctx context.Context, do func(context.Context) error, maxAttempts int) error {
	backoff := 1 * time.Millisecond // tests use a tiny backoff

	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		err := do(ctx)
		if err == nil {
			return nil
		}
		lastErr = err

		if errs.KindOf(err) != errs.KindCompletion || !errs.IsRetriable(err) {
			return err
		}
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

// Retriable completion failures should be retried up to maxAttempts and
// succeed once the underlying op stops failing.
func TestRetryPolicy_RetriesCompletionAndSucceeds(t *testing.T) {
	t.Parallel()
	calls := 0
	err := callWithRetry(context.Background(), func(context.Context) error {
		calls++
		if calls < 3 {
			return errs.Retriable(errs.KindCompletion, "provider.call",
				errors.New("upstream 503"), "upstream call %d", calls)
		}
		return nil
	}, 5)
	if err != nil {
		t.Fatalf("expected nil after retries, got %v", err)
	}
	if calls != 3 {
		t.Fatalf("expected 3 attempts, got %d", calls)
	}
}

// Non-retriable config errors must fail fast — no retries.
func TestRetryPolicy_DoesNotRetryConfigErrors(t *testing.T) {
	t.Parallel()
	calls := 0
	err := callWithRetry(context.Background(), func(context.Context) error {
		calls++
		return errs.Newf(errs.KindConfig, "config.Load", "missing harness.md")
	}, 5)
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	if calls != 1 {
		t.Fatalf("expected 1 attempt (no retry on config), got %d", calls)
	}
	if errs.KindOf(err) != errs.KindConfig {
		t.Fatalf("expected KindConfig, got %s", errs.KindOf(err))
	}
}

// Tool errors are not retriable even if marked retriable — only completion
// kind triggers the retry branch in the canonical policy.
func TestRetryPolicy_DoesNotRetryToolErrors(t *testing.T) {
	t.Parallel()
	calls := 0
	err := callWithRetry(context.Background(), func(context.Context) error {
		calls++
		return errs.Retriable(errs.KindTool, "tools.execute",
			errors.New("handler panic"), "handler crashed")
	}, 5)
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	if calls != 1 {
		t.Fatalf("expected 1 attempt (tool not retried), got %d", calls)
	}
	if errs.KindOf(err) != errs.KindTool {
		t.Fatalf("expected KindTool, got %s", errs.KindOf(err))
	}
}

// Non-retriable completion errors (e.g. 401 unauthorized) should not be
// retried — the Retriable flag is the second gate.
func TestRetryPolicy_DoesNotRetryNonRetriableCompletion(t *testing.T) {
	t.Parallel()
	calls := 0
	err := callWithRetry(context.Background(), func(context.Context) error {
		calls++
		return errs.Newf(errs.KindCompletion, "provider.call", "401 unauthorized")
	}, 5)
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	if calls != 1 {
		t.Fatalf("expected 1 attempt (non-retriable completion), got %d", calls)
	}
}

// After exhausting maxAttempts on a persistently failing retriable
// completion, the last error is returned.
func TestRetryPolicy_GivesUpAfterMaxAttempts(t *testing.T) {
	t.Parallel()
	calls := 0
	err := callWithRetry(context.Background(), func(context.Context) error {
		calls++
		return errs.Retriable(errs.KindCompletion, "provider.call",
			errors.New("upstream 503"), "upstream still flaky")
	}, 3)
	if err == nil {
		t.Fatalf("expected error after exhausting attempts, got nil")
	}
	if calls != 3 {
		t.Fatalf("expected 3 attempts, got %d", calls)
	}
	if errs.KindOf(err) != errs.KindCompletion {
		t.Fatalf("expected KindCompletion, got %s", errs.KindOf(err))
	}
	if !errs.IsRetriable(err) {
		t.Fatalf("expected retriable=true on final error")
	}
}

// Honor ctx cancellation during the backoff sleep.
func TestRetryPolicy_RespectsContextCancellation(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	calls := 0
	go func() {
		time.Sleep(2 * time.Millisecond)
		cancel()
	}()
	err := callWithRetry(ctx, func(context.Context) error {
		calls++
		return errs.Retriable(errs.KindCompletion, "provider.call",
			errors.New("upstream 503"), "flaky")
	}, 100)
	if err == nil {
		t.Fatalf("expected ctx cancellation error, got nil")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
}
