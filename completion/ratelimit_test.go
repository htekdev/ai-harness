package completion

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestNewRateLimiter_NilWhenDisabled(t *testing.T) {
	if rl := NewRateLimiter(RateLimitPolicy{}); rl != nil {
		t.Fatalf("expected nil limiter for empty policy, got %T", rl)
	}
	if rl := NewRateLimiter(RateLimitPolicy{PerModel: map[string]ModelRateLimit{"x": {RPS: 0}}}); rl != nil {
		t.Fatalf("expected nil limiter when per-model RPS is 0")
	}
}

func TestNewRateLimiter_GlobalOnly(t *testing.T) {
	rl := NewRateLimiter(RateLimitPolicy{GlobalRPS: 100})
	if rl == nil {
		t.Fatal("expected non-nil limiter")
	}
	bl, ok := rl.(*bucketLimiter)
	if !ok {
		t.Fatalf("unexpected type: %T", rl)
	}
	if bl.global == nil {
		t.Fatal("expected global bucket")
	}
	if len(bl.perModel) != 0 {
		t.Fatalf("expected no per-model buckets, got %d", len(bl.perModel))
	}
	if bl.global.burst != 100 {
		t.Errorf("default burst from RPS=100 should be 100, got %v", bl.global.burst)
	}
}

func TestNewRateLimiter_PerModelOnly(t *testing.T) {
	rl := NewRateLimiter(RateLimitPolicy{
		PerModel: map[string]ModelRateLimit{
			"gpt-4o":     {RPS: 5, Burst: 2},
			"claude-3.5": {RPS: 10},
		},
	})
	bl := rl.(*bucketLimiter)
	if bl.global != nil {
		t.Fatal("expected no global bucket")
	}
	if len(bl.perModel) != 2 {
		t.Fatalf("expected 2 per-model buckets, got %d", len(bl.perModel))
	}
	if bl.perModel["gpt-4o"].burst != 2 {
		t.Errorf("gpt-4o burst override: got %v want 2", bl.perModel["gpt-4o"].burst)
	}
}

func TestRateLimiter_BurstThenThrottle(t *testing.T) {
	// 10 RPS, burst 3 → first 3 immediate, 4th waits ~100ms.
	rl := NewRateLimiter(RateLimitPolicy{GlobalRPS: 10, GlobalBurst: 3})
	ctx := context.Background()

	start := time.Now()
	for i := 0; i < 3; i++ {
		if err := rl.Wait(ctx, "m"); err != nil {
			t.Fatalf("burst wait %d: %v", i, err)
		}
	}
	if elapsed := time.Since(start); elapsed > 20*time.Millisecond {
		t.Errorf("burst calls should be near-instant, took %v", elapsed)
	}

	// 4th call should wait ~100ms (1/10s).
	t0 := time.Now()
	if err := rl.Wait(ctx, "m"); err != nil {
		t.Fatalf("throttled wait: %v", err)
	}
	elapsed := time.Since(t0)
	if elapsed < 80*time.Millisecond {
		t.Errorf("4th call should wait ~100ms, got %v", elapsed)
	}
	if elapsed > 250*time.Millisecond {
		t.Errorf("4th call took too long: %v", elapsed)
	}
}

func TestRateLimiter_PerModelIsolation(t *testing.T) {
	// Two models, each at 10 RPS burst 1. Saturating one must not delay the other.
	rl := NewRateLimiter(RateLimitPolicy{
		PerModel: map[string]ModelRateLimit{
			"a": {RPS: 10, Burst: 1},
			"b": {RPS: 10, Burst: 1},
		},
	})
	ctx := context.Background()

	if err := rl.Wait(ctx, "a"); err != nil { // drain a
		t.Fatal(err)
	}
	t0 := time.Now()
	if err := rl.Wait(ctx, "b"); err != nil { // b should be immediate
		t.Fatal(err)
	}
	if elapsed := time.Since(t0); elapsed > 20*time.Millisecond {
		t.Errorf("model b should not wait for model a, took %v", elapsed)
	}
}

func TestRateLimiter_GlobalAcrossModels(t *testing.T) {
	// Global 10 RPS burst 2 with no per-model limits → models share global bucket.
	rl := NewRateLimiter(RateLimitPolicy{GlobalRPS: 10, GlobalBurst: 2})
	ctx := context.Background()

	if err := rl.Wait(ctx, "a"); err != nil {
		t.Fatal(err)
	}
	if err := rl.Wait(ctx, "b"); err != nil {
		t.Fatal(err)
	}
	// Third call from any model should wait for global refill.
	t0 := time.Now()
	if err := rl.Wait(ctx, "c"); err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(t0); elapsed < 80*time.Millisecond {
		t.Errorf("3rd call should wait ~100ms for global refill, got %v", elapsed)
	}
}

func TestRateLimiter_ContextCancel(t *testing.T) {
	rl := NewRateLimiter(RateLimitPolicy{GlobalRPS: 1, GlobalBurst: 1})
	ctx := context.Background()
	if err := rl.Wait(ctx, "m"); err != nil { // drain
		t.Fatal(err)
	}

	ctx2, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	t0 := time.Now()
	err := rl.Wait(ctx2, "m")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected DeadlineExceeded, got %v", err)
	}
	if elapsed := time.Since(t0); elapsed > 200*time.Millisecond {
		t.Errorf("cancel should be honored quickly, took %v", elapsed)
	}
}

func TestRateLimiter_ConcurrentSafety(t *testing.T) {
	rl := NewRateLimiter(RateLimitPolicy{GlobalRPS: 1000, GlobalBurst: 50})
	ctx := context.Background()

	var wg sync.WaitGroup
	const goroutines = 20
	const callsEach = 5
	errs := make(chan error, goroutines*callsEach)
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < callsEach; j++ {
				if err := rl.Wait(ctx, "m"); err != nil {
					errs <- err
				}
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Errorf("concurrent wait error: %v", err)
	}
}

func TestDefaultBurst(t *testing.T) {
	cases := []struct {
		rps  float64
		want int
	}{
		{0, 1},
		{0.1, 1},
		{1.0, 1},
		{1.5, 2},
		{10.0, 10},
		{10.5, 11},
	}
	for _, c := range cases {
		if got := defaultBurst(c.rps); got != c.want {
			t.Errorf("defaultBurst(%v) = %d, want %d", c.rps, got, c.want)
		}
	}
}

// --- Client integration ---

func TestClient_RateLimiterWiredFromConfig(t *testing.T) {
	c := NewClient(ClientConfig{
		BaseURL:   "http://example",
		APIKey:    "k",
		RateLimit: RateLimitPolicy{GlobalRPS: 5},
	})
	if c.limiter == nil {
		t.Fatal("expected limiter to be built from RateLimit policy")
	}
}

func TestClient_RateLimiterOverride(t *testing.T) {
	custom := &countingLimiter{}
	c := NewClient(ClientConfig{
		BaseURL:     "http://example",
		APIKey:      "k",
		RateLimit:   RateLimitPolicy{GlobalRPS: 5}, // should be ignored
		RateLimiter: custom,
	})
	if c.limiter != custom {
		t.Fatal("explicit RateLimiter should override RateLimit policy")
	}
}

func TestClient_RateLimiter_ConsultedPerAttempt(t *testing.T) {
	var attempts int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&attempts, 1)
		if n < 3 {
			// Return 503 to trigger retry.
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte("transient"))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"x","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`))
	}))
	defer srv.Close()

	limiter := &countingLimiter{}
	c := NewClient(ClientConfig{
		BaseURL: srv.URL,
		APIKey:  "k",
		Model:   "gpt-test",
		RetryPolicy: &RetryPolicy{
			MaxRetries:     3,
			InitialBackoff: time.Millisecond,
			MaxBackoff:     time.Millisecond,
			Multiplier:     1.0,
		},
		RateLimiter: limiter,
	})

	resp, err := c.Complete(context.Background(), Request{
		Messages: []Message{{Role: RoleUser, Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if resp.Choices[0].Message.Content != "ok" {
		t.Errorf("unexpected response: %+v", resp)
	}
	if got, want := atomic.LoadInt32(&attempts), int32(3); got != want {
		t.Errorf("server attempts: got %d want %d", got, want)
	}
	if got, want := limiter.calls(), 3; got != want {
		t.Errorf("rate limiter calls: got %d want %d (one per attempt)", got, want)
	}
	if model := limiter.lastModel(); model != "gpt-test" {
		t.Errorf("rate limiter should see model id, got %q", model)
	}
}

func TestClient_RateLimiter_CancelAbortsRequest(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("server should not be called when limiter blocks")
		w.WriteHeader(500)
	}))
	defer srv.Close()

	// Limiter that always returns ctx.Err() to simulate exhausted budget.
	c := NewClient(ClientConfig{
		BaseURL:     srv.URL,
		APIKey:      "k",
		RateLimiter: &blockingLimiter{},
	})

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	_, err := c.Complete(ctx, Request{Messages: []Message{{Role: RoleUser, Content: "hi"}}})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected DeadlineExceeded from limiter, got %v", err)
	}
}

// --- test doubles ---

type countingLimiter struct {
	mu         sync.Mutex
	n          int
	lastModelV string
}

func (l *countingLimiter) Wait(ctx context.Context, model string) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.n++
	l.lastModelV = model
	return nil
}

func (l *countingLimiter) calls() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.n
}

func (l *countingLimiter) lastModel() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.lastModelV
}

type blockingLimiter struct{}

func (b *blockingLimiter) Wait(ctx context.Context, model string) error {
	<-ctx.Done()
	return ctx.Err()
}
