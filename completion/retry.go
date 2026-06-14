package completion

import (
	"math"
	"time"
)

// RetryPolicy controls how the completion client retries transient errors.
//
// Backoff for attempt N (N >= 1) is computed as:
//
//	delay = InitialBackoff * (Multiplier ^ (N-1))
//
// clamped to MaxBackoff. A Multiplier of 1.0 is constant backoff. A
// Multiplier of 2.0 with InitialBackoff=1s reproduces the original
// 1s/2s/4s/... exponential schedule.
//
// All zero-value fields are filled by DefaultRetryPolicy.
type RetryPolicy struct {
	// MaxRetries is the maximum number of retry attempts after the
	// initial request. A value of 0 disables retries.
	MaxRetries int

	// InitialBackoff is the delay before the first retry (attempt 1).
	InitialBackoff time.Duration

	// MaxBackoff caps the per-attempt delay. Zero means no cap.
	MaxBackoff time.Duration

	// Multiplier is the exponential growth factor between attempts.
	// Must be >= 1.0; values < 1.0 are coerced to 1.0.
	Multiplier float64
}

// DefaultRetryPolicy returns the policy that matches the historical
// hardcoded behavior: 3 retries, 1s initial backoff, exponential base 2,
// 30s cap.
func DefaultRetryPolicy() RetryPolicy {
	return RetryPolicy{
		MaxRetries:     3,
		InitialBackoff: 1 * time.Second,
		MaxBackoff:     30 * time.Second,
		Multiplier:     2.0,
	}
}

// withDefaults returns a copy of p with zero-value fields filled in from
// DefaultRetryPolicy. MaxRetries is preserved as-is (0 means "no retries").
func (p RetryPolicy) withDefaults() RetryPolicy {
	d := DefaultRetryPolicy()
	out := p
	if out.InitialBackoff <= 0 {
		out.InitialBackoff = d.InitialBackoff
	}
	if out.MaxBackoff <= 0 {
		out.MaxBackoff = d.MaxBackoff
	}
	if out.Multiplier < 1.0 {
		out.Multiplier = d.Multiplier
	}
	return out
}

// Backoff returns the delay before the given retry attempt.
// attempt is 1-indexed; Backoff(0) returns 0.
func (p RetryPolicy) Backoff(attempt int) time.Duration {
	if attempt <= 0 {
		return 0
	}
	pp := p.withDefaults()
	// delay = initial * multiplier^(attempt-1), in float seconds
	factor := math.Pow(pp.Multiplier, float64(attempt-1))
	delay := time.Duration(float64(pp.InitialBackoff) * factor)
	if pp.MaxBackoff > 0 && delay > pp.MaxBackoff {
		delay = pp.MaxBackoff
	}
	return delay
}
