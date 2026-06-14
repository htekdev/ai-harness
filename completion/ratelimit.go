package completion

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// RateLimitPolicy declares per-model and global request rate limits applied
// before each outbound completion request (including retries). Rates are
// expressed in requests per second; bursts allow short-lived spikes above
// the steady-state rate.
//
// A zero-value policy disables rate limiting. Per-model entries override
// the global limit for matching models. When both apply, BOTH must admit
// the request (the request waits for the slower of the two).
//
// Burst defaults to max(1, ceil(RPS)) when zero.
type RateLimitPolicy struct {
	// GlobalRPS is the steady-state requests/sec ceiling for ALL models
	// combined. Zero disables the global limit.
	GlobalRPS float64

	// GlobalBurst is the maximum burst size for the global bucket.
	// Zero defaults to max(1, ceil(GlobalRPS)).
	GlobalBurst int

	// PerModel applies a model-scoped bucket in addition to the global one.
	// Keyed by exact model id (matched against ClientConfig.Model or the
	// per-request Model override). Models with no entry are governed only
	// by the global limit.
	PerModel map[string]ModelRateLimit
}

// ModelRateLimit is a per-model token-bucket configuration.
type ModelRateLimit struct {
	// RPS is the steady-state requests/sec ceiling for this model.
	// Must be > 0 for the entry to be active.
	RPS float64
	// Burst is the maximum burst size. Zero defaults to max(1, ceil(RPS)).
	Burst int
}

// RateLimiter is the interface the completion client uses to throttle
// outbound requests. Implementations MUST be safe for concurrent use and
// MUST honor ctx cancellation while waiting.
type RateLimiter interface {
	// Wait blocks until the request for the given model is admitted or
	// ctx is cancelled. Returns ctx.Err() on cancellation.
	Wait(ctx context.Context, model string) error
}

// NewRateLimiter materializes a RateLimiter from the policy. Returns nil
// when the policy carries no active limits (global zero AND no per-model
// entries with RPS > 0).
func NewRateLimiter(p RateLimitPolicy) RateLimiter {
	hasGlobal := p.GlobalRPS > 0
	active := make(map[string]*tokenBucket)
	for model, ml := range p.PerModel {
		if ml.RPS <= 0 {
			continue
		}
		burst := ml.Burst
		if burst <= 0 {
			burst = defaultBurst(ml.RPS)
		}
		active[model] = newTokenBucket(ml.RPS, burst)
	}
	if !hasGlobal && len(active) == 0 {
		return nil
	}

	rl := &bucketLimiter{perModel: active}
	if hasGlobal {
		burst := p.GlobalBurst
		if burst <= 0 {
			burst = defaultBurst(p.GlobalRPS)
		}
		rl.global = newTokenBucket(p.GlobalRPS, burst)
	}
	return rl
}

func defaultBurst(rps float64) int {
	if rps <= 0 {
		return 1
	}
	b := int(rps)
	if float64(b) < rps {
		b++
	}
	if b < 1 {
		b = 1
	}
	return b
}

// bucketLimiter is the default RateLimiter implementation backed by
// in-memory token buckets.
type bucketLimiter struct {
	global   *tokenBucket
	perModel map[string]*tokenBucket
}

func (l *bucketLimiter) Wait(ctx context.Context, model string) error {
	// Per-model first so that a saturated model bucket cannot consume a
	// global slot it cannot actually use.
	if l.perModel != nil {
		if mb, ok := l.perModel[model]; ok {
			if err := mb.wait(ctx); err != nil {
				return err
			}
		}
	}
	if l.global != nil {
		if err := l.global.wait(ctx); err != nil {
			return err
		}
	}
	return nil
}

// tokenBucket is a simple lazily-refilled token bucket. tokens are stored
// as a float to support sub-token refill increments at fractional RPS.
type tokenBucket struct {
	mu         sync.Mutex
	rps        float64
	burst      float64
	tokens     float64
	lastRefill time.Time
	now        func() time.Time
	sleep      func(ctx context.Context, d time.Duration) error
}

func newTokenBucket(rps float64, burst int) *tokenBucket {
	if rps <= 0 {
		rps = 0
	}
	if burst < 1 {
		burst = 1
	}
	return &tokenBucket{
		rps:        rps,
		burst:      float64(burst),
		tokens:     float64(burst),
		lastRefill: time.Now(),
		now:        time.Now,
		sleep:      ctxSleep,
	}
}

func ctxSleep(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return nil
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// wait blocks until one token is available or ctx is cancelled.
func (b *tokenBucket) wait(ctx context.Context) error {
	for {
		if err := ctx.Err(); err != nil {
			return err
		}

		b.mu.Lock()
		if b.rps <= 0 {
			// Defensive: a non-positive RPS bucket would never refill.
			// Treat as permanently saturated and return immediately so we
			// don't deadlock; callers should not construct such buckets.
			b.mu.Unlock()
			return fmt.Errorf("rate limiter: bucket has non-positive rps")
		}

		now := b.now()
		elapsed := now.Sub(b.lastRefill).Seconds()
		if elapsed > 0 {
			b.tokens += elapsed * b.rps
			if b.tokens > b.burst {
				b.tokens = b.burst
			}
			b.lastRefill = now
		}

		if b.tokens >= 1 {
			b.tokens -= 1
			b.mu.Unlock()
			return nil
		}

		// Need to wait for (1 - tokens) more tokens to accumulate.
		needed := 1 - b.tokens
		waitSec := needed / b.rps
		b.mu.Unlock()

		// Convert to duration (round up to at least 1ns to make progress).
		d := time.Duration(waitSec * float64(time.Second))
		if d < time.Nanosecond {
			d = time.Nanosecond
		}
		if err := b.sleep(ctx, d); err != nil {
			return err
		}
		// Loop and re-check; another goroutine may have drained the refill.
	}
}
