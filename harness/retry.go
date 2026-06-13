package harness

import (
	"time"

	"github.com/htekdev/ai-harness/completion"
	"github.com/htekdev/ai-harness/config"
)

// retryPolicyFromConfig converts a declarative config.RetryConfig into a
// completion.RetryPolicy. Returns nil when the input is nil so the
// completion client falls back to its default policy.
func retryPolicyFromConfig(r *config.RetryConfig) *completion.RetryPolicy {
	if r == nil {
		return nil
	}
	p := completion.DefaultRetryPolicy()
	if r.MaxRetries != nil {
		p.MaxRetries = *r.MaxRetries
	}
	if r.InitialBackoffMS > 0 {
		p.InitialBackoff = time.Duration(r.InitialBackoffMS) * time.Millisecond
	}
	if r.MaxBackoffMS > 0 {
		p.MaxBackoff = time.Duration(r.MaxBackoffMS) * time.Millisecond
	}
	if r.Multiplier > 0 {
		p.Multiplier = r.Multiplier
	}
	return &p
}
