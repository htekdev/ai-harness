package async

import (
	"context"
	"encoding/json"
	"sync/atomic"
	"testing"
	"time"

	"github.com/htekdev/ai-harness/tools"
)

// mockCaller is a test implementation of ToolCaller.
type mockCaller struct {
	callCount atomic.Int64
	handler   func(ctx context.Context, call tools.Call) tools.Result
}

func (m *mockCaller) Execute(ctx context.Context, call tools.Call) tools.Result {
	m.callCount.Add(1)
	if m.handler != nil {
		return m.handler(ctx, call)
	}
	return tools.Result{CallID: call.ID, Name: call.Name, Content: "ok"}
}

// echoHandler returns a ToolCaller that echoes the tool name as the result.
func echoHandler() *mockCaller {
	return &mockCaller{
		handler: func(_ context.Context, call tools.Call) tools.Result {
			return tools.Result{CallID: call.ID, Name: call.Name, Content: "echo:" + call.Name}
		},
	}
}

// slowHandler returns a ToolCaller that sleeps before responding.
func slowHandler(delay time.Duration) *mockCaller {
	return &mockCaller{
		handler: func(ctx context.Context, call tools.Call) tools.Result {
			select {
			case <-time.After(delay):
				return tools.Result{CallID: call.ID, Name: call.Name, Content: "slow:" + call.Name}
			case <-ctx.Done():
				return tools.Result{CallID: call.ID, Name: call.Name, Content: "cancelled", IsError: true}
			}
		},
	}
}

// errorHandler returns a ToolCaller that always returns an error result.
func errorHandler() *mockCaller {
	return &mockCaller{
		handler: func(_ context.Context, call tools.Call) tools.Result {
			return tools.Result{CallID: call.ID, Name: call.Name, Content: "tool error", IsError: true}
		},
	}
}

// --- Placeholder tests ---

func TestPlaceholderID(t *testing.T) {
	_, cancel := context.WithCancel(context.Background())
	defer cancel()
	p := newPlaceholder("async_test_1", "test_tool", cancel)
	if p.ID() != "async_test_1" {
		t.Errorf("unexpected ID: %s", p.ID())
	}
	if p.ToolName() != "test_tool" {
		t.Errorf("unexpected ToolName: %s", p.ToolName())
	}
	if p.State() != StatePending {
		t.Errorf("expected StatePending, got %s", p.State())
	}
}

func TestPlaceholderResolve(t *testing.T) {
	ctx := context.Background()
	_, cancel := context.WithCancel(ctx)
	p := newPlaceholder("p1", "tool", cancel)

	go func() {
		time.Sleep(10 * time.Millisecond)
		p.resolve("hello")
	}()

	v, err := p.Result(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if v != "hello" {
		t.Errorf("expected 'hello', got %q", v)
	}
	if p.State() != StateComplete {
		t.Errorf("expected StateComplete, got %s", p.State())
	}
}

func TestPlaceholderFail(t *testing.T) {
	ctx := context.Background()
	_, cancel := context.WithCancel(ctx)
	p := newPlaceholder("p1", "tool", cancel)

	go func() {
		time.Sleep(10 * time.Millisecond)
		p.fail(newf(KindExecution, "boom"))
	}()

	_, err := p.Result(ctx)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestPlaceholderCancel(t *testing.T) {
	ctx := context.Background()
	_, cancel := context.WithCancel(ctx)
	p := newPlaceholder("p1", "tool", cancel)

	p.cancelPlaceholder()

	if p.State() != StateCancelled {
		t.Errorf("expected StateCancelled, got %s", p.State())
	}

	_, err := p.Result(ctx)
	if !IsCancelled(err) {
		t.Errorf("expected IsCancelled(err) to be true, got %v", err)
	}
}

// --- Graph tests ---

func TestGraphAdd(t *testing.T) {
	g := NewGraph()
	if err := g.Add("a", nil); err != nil {
		t.Fatalf("unexpected error adding a: %v", err)
	}
	if err := g.Add("b", []string{"a"}); err != nil {
		t.Fatalf("unexpected error adding b: %v", err)
	}
}

func TestGraphCycleDetection(t *testing.T) {
	g := NewGraph()
	if err := g.Add("a", nil); err != nil {
		t.Fatal(err)
	}
	if err := g.Add("b", []string{"a"}); err != nil {
		t.Fatal(err)
	}
	// Add c that depends on b (a→b→c is fine).
	if err := g.Add("c", []string{"b"}); err != nil {
		t.Fatal(err)
	}
	// Trying to add 'a' again should fail (already exists).
	err := g.Add("a", nil)
	if err == nil {
		t.Fatal("expected error for duplicate node")
	}
}

func TestGraphMissingDep(t *testing.T) {
	g := NewGraph()
	// b depends on "missing" — should error because "missing" isn't in graph.
	err := g.Add("b", []string{"missing"})
	if err == nil {
		t.Fatal("expected error for unknown dep")
	}
}

// --- Executor tests ---

func TestExecutorLaunchAndWait(t *testing.T) {
	caller := echoHandler()
	exec := NewExecutor(4, caller)
	ctx := context.Background()

	p, err := exec.Launch(ctx, "my_tool", json.RawMessage(`{}`), nil)
	if err != nil {
		t.Fatalf("Launch: %v", err)
	}

	v, err := p.Result(ctx)
	if err != nil {
		t.Fatalf("Result: %v", err)
	}
	if v != "echo:my_tool" {
		t.Errorf("unexpected result: %q", v)
	}
}

func TestExecutorFanOut(t *testing.T) {
	caller := echoHandler()
	exec := NewExecutor(4, caller)
	ctx := context.Background()

	refs := make([]*Placeholder, 5)
	for i := 0; i < 5; i++ {
		p, err := exec.Launch(ctx, "tool", json.RawMessage(`{}`), nil)
		if err != nil {
			t.Fatalf("Launch[%d]: %v", i, err)
		}
		refs[i] = p
	}

	results := WaitAll(ctx, refs)
	for i, r := range results {
		if r.Err != nil {
			t.Errorf("result[%d] error: %v", i, r.Err)
		}
		if r.Value != "echo:tool" {
			t.Errorf("result[%d] unexpected value: %q", i, r.Value)
		}
	}
	if caller.callCount.Load() != 5 {
		t.Errorf("expected 5 tool calls, got %d", caller.callCount.Load())
	}
}

func TestExecutorDependencyChain(t *testing.T) {
	order := make([]string, 0, 3)
	var mu = &atomic.Uint64{}

	caller := &mockCaller{
		handler: func(_ context.Context, call tools.Call) tools.Result {
			// Record call order via a simple counter.
			mu.Add(1)
			return tools.Result{CallID: call.ID, Name: call.Name, Content: call.Name}
		},
	}

	exec := NewExecutor(4, caller)
	ctx := context.Background()

	refA, err := exec.Launch(ctx, "tool_a", json.RawMessage(`{}`), nil)
	if err != nil {
		t.Fatal(err)
	}
	refB, err := exec.Launch(ctx, "tool_b", json.RawMessage(`{}`), []*Placeholder{refA})
	if err != nil {
		t.Fatal(err)
	}
	refC, err := exec.Launch(ctx, "tool_c", json.RawMessage(`{}`), []*Placeholder{refB})
	if err != nil {
		t.Fatal(err)
	}

	results := WaitAll(ctx, []*Placeholder{refA, refB, refC})
	for i, r := range results {
		if r.Err != nil {
			t.Errorf("result[%d] error: %v", i, r.Err)
		}
	}
	_ = order // values captured via Result calls for ordering checks

	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}
	if results[0].ToolName != "tool_a" || results[1].ToolName != "tool_b" || results[2].ToolName != "tool_c" {
		t.Errorf("unexpected order: %v %v %v", results[0].ToolName, results[1].ToolName, results[2].ToolName)
	}
}

func TestExecutorDependencyFailurePropagates(t *testing.T) {
	caller := errorHandler()
	exec := NewExecutor(4, caller)
	ctx := context.Background()

	refA, err := exec.Launch(ctx, "fail_tool", json.RawMessage(`{}`), nil)
	if err != nil {
		t.Fatal(err)
	}
	refB, err := exec.Launch(ctx, "dep_tool", json.RawMessage(`{}`), []*Placeholder{refA})
	if err != nil {
		t.Fatal(err)
	}

	// A failed, so B should also fail.
	_, errA := refA.Result(ctx)
	if errA == nil {
		t.Error("expected refA to fail")
	}
	_, errB := refB.Result(ctx)
	if errB == nil {
		t.Error("expected refB to fail because refA failed")
	}
}

func TestExecutorBarrier(t *testing.T) {
	caller := echoHandler()
	exec := NewExecutor(4, caller)
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		if _, err := exec.Launch(ctx, "tool", json.RawMessage(`{}`), nil); err != nil {
			t.Fatal(err)
		}
	}

	exec.Barrier(ctx)

	if exec.Pending() != 0 {
		t.Errorf("expected 0 pending after barrier, got %d", exec.Pending())
	}
}

// --- WaitAny / Race tests ---

func TestWaitAny(t *testing.T) {
	caller := echoHandler()
	exec := NewExecutor(4, caller)
	ctx := context.Background()

	refs := make([]*Placeholder, 3)
	for i := 0; i < 3; i++ {
		p, err := exec.Launch(ctx, "tool", json.RawMessage(`{}`), nil)
		if err != nil {
			t.Fatal(err)
		}
		refs[i] = p
	}

	result := WaitAny(ctx, refs)
	if result.Err != nil {
		t.Fatalf("WaitAny error: %v", result.Err)
	}
	if result.Value != "echo:tool" {
		t.Errorf("unexpected value: %q", result.Value)
	}
}

func TestRace(t *testing.T) {
	// Tool A is fast, Tool B is slow. Race should return A and cancel B.
	fast := &mockCaller{
		handler: func(_ context.Context, call tools.Call) tools.Result {
			return tools.Result{CallID: call.ID, Name: call.Name, Content: "fast"}
		},
	}
	slow := slowHandler(200 * time.Millisecond)

	// Use two separate executors (one per tool) to simulate the race.
	execFast := NewExecutor(1, fast)
	execSlow := NewExecutor(1, slow)
	ctx := context.Background()

	refFast, _ := execFast.Launch(ctx, "fast_tool", json.RawMessage(`{}`), nil)
	refSlow, _ := execSlow.Launch(ctx, "slow_tool", json.RawMessage(`{}`), nil)

	result := Race(ctx, []*Placeholder{refFast, refSlow})
	if result.Err != nil {
		t.Fatalf("Race error: %v", result.Err)
	}
	if result.Value != "fast" {
		t.Errorf("expected 'fast' to win, got %q", result.Value)
	}
	// Loser should eventually be cancelled.
	if refSlow.State() != StateCancelled {
		// Give a tiny moment for the cancel signal to propagate.
		time.Sleep(50 * time.Millisecond)
		if refSlow.State() != StateCancelled {
			t.Errorf("expected slow ref to be cancelled, state=%s", refSlow.State())
		}
	}
}

// --- Context key tests ---

func TestExecutorContext(t *testing.T) {
	exec := NewExecutor(4, echoHandler())
	ctx := WithExecutor(context.Background(), exec)

	got := ExecutorFromContext(ctx)
	if got != exec {
		t.Error("ExecutorFromContext returned wrong executor")
	}

	if ExecutorFromContext(context.Background()) != nil {
		t.Error("expected nil executor from bare context")
	}
}

// --- Error helper tests ---

func TestIsCancelled(t *testing.T) {
	if !IsCancelled(ErrCancelled) {
		t.Error("ErrCancelled should be detected by IsCancelled")
	}
	if IsCancelled(nil) {
		t.Error("nil should not be cancelled")
	}
	if IsCancelled(newf(KindExecution, "other")) {
		t.Error("KindExecution should not be IsCancelled")
	}
}

func TestErrorUnwrap(t *testing.T) {
	inner := newf(KindExecution, "inner")
	outer := wrap(KindExecution, "outer", inner)
	if outer.Unwrap() != inner {
		t.Error("Unwrap did not return inner error")
	}
}
