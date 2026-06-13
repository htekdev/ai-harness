package harness

import (
	"testing"
	"time"

	"github.com/htekdev/ai-harness/config"
)

func TestRetryPolicyFromConfigNil(t *testing.T) {
	if got := retryPolicyFromConfig(nil); got != nil {
		t.Errorf("nil input should return nil, got %+v", got)
	}
}

func TestRetryPolicyFromConfigFull(t *testing.T) {
	mr := 5
	r := &config.RetryConfig{
		MaxRetries:       &mr,
		InitialBackoffMS: 200,
		MaxBackoffMS:     5000,
		Multiplier:       1.5,
	}
	p := retryPolicyFromConfig(r)
	if p == nil {
		t.Fatal("expected policy")
	}
	if p.MaxRetries != 5 {
		t.Errorf("MaxRetries = %d, want 5", p.MaxRetries)
	}
	if p.InitialBackoff != 200*time.Millisecond {
		t.Errorf("InitialBackoff = %v, want 200ms", p.InitialBackoff)
	}
	if p.MaxBackoff != 5*time.Second {
		t.Errorf("MaxBackoff = %v, want 5s", p.MaxBackoff)
	}
	if p.Multiplier != 1.5 {
		t.Errorf("Multiplier = %v, want 1.5", p.Multiplier)
	}
}

func TestRetryPolicyFromConfigPartialInheritsDefaults(t *testing.T) {
	// Only MaxRetries set; backoff fields inherit DefaultRetryPolicy.
	mr := 2
	p := retryPolicyFromConfig(&config.RetryConfig{MaxRetries: &mr})
	if p.MaxRetries != 2 {
		t.Errorf("MaxRetries = %d, want 2", p.MaxRetries)
	}
	if p.InitialBackoff != 1*time.Second {
		t.Errorf("InitialBackoff = %v, want default 1s", p.InitialBackoff)
	}
	if p.Multiplier != 2.0 {
		t.Errorf("Multiplier = %v, want default 2.0", p.Multiplier)
	}
}
