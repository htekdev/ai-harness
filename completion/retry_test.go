package completion

import (
	"context"
	"testing"
	"time"
)

func TestDefaultRetryPolicyMatchesLegacySchedule(t *testing.T) {
	p := DefaultRetryPolicy()
	// Legacy schedule: 1s, 2s, 4s for attempts 1..3.
	cases := []struct {
		attempt int
		want    time.Duration
	}{
		{0, 0},
		{1, 1 * time.Second},
		{2, 2 * time.Second},
		{3, 4 * time.Second},
	}
	for _, c := range cases {
		got := p.Backoff(c.attempt)
		if got != c.want {
			t.Errorf("Backoff(%d) = %v, want %v", c.attempt, got, c.want)
		}
	}
}

func TestRetryPolicyCustomMultiplierAndCap(t *testing.T) {
	p := RetryPolicy{
		MaxRetries:     5,
		InitialBackoff: 100 * time.Millisecond,
		MaxBackoff:     500 * time.Millisecond,
		Multiplier:     3.0,
	}
	// 100ms, 300ms, 900ms→capped 500ms, then capped.
	if got, want := p.Backoff(1), 100*time.Millisecond; got != want {
		t.Errorf("attempt 1: got %v want %v", got, want)
	}
	if got, want := p.Backoff(2), 300*time.Millisecond; got != want {
		t.Errorf("attempt 2: got %v want %v", got, want)
	}
	if got, want := p.Backoff(3), 500*time.Millisecond; got != want {
		t.Errorf("attempt 3 cap: got %v want %v", got, want)
	}
	if got, want := p.Backoff(7), 500*time.Millisecond; got != want {
		t.Errorf("attempt 7 cap: got %v want %v", got, want)
	}
}

func TestRetryPolicyZeroValuesUseDefaults(t *testing.T) {
	p := RetryPolicy{MaxRetries: 2} // backoff fields all zero
	// Should fall back to default schedule.
	if got, want := p.Backoff(1), 1*time.Second; got != want {
		t.Errorf("zero policy attempt 1: got %v want %v", got, want)
	}
	if got, want := p.Backoff(2), 2*time.Second; got != want {
		t.Errorf("zero policy attempt 2: got %v want %v", got, want)
	}
}

func TestRetryPolicyConstantBackoffMultiplierOne(t *testing.T) {
	p := RetryPolicy{
		MaxRetries:     4,
		InitialBackoff: 250 * time.Millisecond,
		Multiplier:     1.0,
	}
	for _, attempt := range []int{1, 2, 3, 4} {
		if got, want := p.Backoff(attempt), 250*time.Millisecond; got != want {
			t.Errorf("constant policy attempt %d: got %v want %v", attempt, got, want)
		}
	}
}

func TestNewClientSynthesizesPolicyFromMaxRetries(t *testing.T) {
	c := NewClient(ClientConfig{BaseURL: "http://x", APIKey: "k", MaxRetries: 7})
	if c.config.RetryPolicy == nil {
		t.Fatal("expected synthesized retry policy")
	}
	if c.config.RetryPolicy.MaxRetries != 7 {
		t.Errorf("synth MaxRetries = %d, want 7", c.config.RetryPolicy.MaxRetries)
	}
	// Default backoff parameters should be inherited.
	if c.config.RetryPolicy.InitialBackoff != 1*time.Second {
		t.Errorf("synth InitialBackoff = %v, want 1s", c.config.RetryPolicy.InitialBackoff)
	}
}

func TestNewClientExplicitPolicyWins(t *testing.T) {
	custom := &RetryPolicy{
		MaxRetries:     1,
		InitialBackoff: 50 * time.Millisecond,
		Multiplier:     1.0,
	}
	c := NewClient(ClientConfig{
		BaseURL:     "http://x",
		APIKey:      "k",
		MaxRetries:  9, // should be overridden by policy MaxRetries=1
		RetryPolicy: custom,
	})
	if c.config.RetryPolicy.MaxRetries != 1 {
		t.Errorf("explicit MaxRetries = %d, want 1", c.config.RetryPolicy.MaxRetries)
	}
	if c.config.MaxRetries != 1 {
		t.Errorf("mirrored MaxRetries = %d, want 1", c.config.MaxRetries)
	}
}

func TestWaitForRetryRespectsContextCancel(t *testing.T) {
	c := NewClient(ClientConfig{
		BaseURL: "http://x",
		APIKey:  "k",
		RetryPolicy: &RetryPolicy{
			MaxRetries:     3,
			InitialBackoff: 5 * time.Second,
			Multiplier:     1.0,
		},
	})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := c.waitForRetry(ctx, 1); err == nil {
		t.Fatal("expected context error, got nil")
	}
}
