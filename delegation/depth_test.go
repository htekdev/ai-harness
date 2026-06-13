package delegation

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/htekdev/ai-harness/harness/errs"
)

func TestGetDepth_Default(t *testing.T) {
	ctx := context.Background()
	if got := GetDepth(ctx); got != 0 {
		t.Errorf("expected depth 0, got %d", got)
	}
}

func TestWithDepth(t *testing.T) {
	ctx := context.Background()
	ctx = WithDepth(ctx, 3)
	if got := GetDepth(ctx); got != 3 {
		t.Errorf("expected depth 3, got %d", got)
	}
}

func TestDepthLimit(t *testing.T) {
	d := NewDelegator(DelegatorConfig{
		MaxDepth: 2,
	})

	// Simulate being at depth 2 (should be blocked)
	ctx := WithDepth(context.Background(), 2)
	_, err := d.Execute(ctx, Request{
		Task: "test",
		Tools: []ToolSpec{
			{Name: "t", Description: "d", Script: "def run(args):\n    return \"ok\""},
		},
	})
	if err == nil {
		t.Fatal("expected depth limit error")
	}
	if !contains(err.Error(), "depth limit") {
		t.Errorf("unexpected error: %v", err)
	}
	// Phase 5.3: depth-limit errors must be classified as KindDelegation so
	// dashboards / hooks can react without parsing message text.
	if k := errs.KindOf(err); k != errs.KindDelegation {
		t.Errorf("KindOf(depth-limit err) = %v, want KindDelegation", k)
	}
}

func TestMaxHardDepthEnforced(t *testing.T) {
	d := NewDelegator(DelegatorConfig{
		MaxDepth: 100, // exceeds hard cap
	})
	if d.maxDepth != MaxHardDepth {
		t.Errorf("expected maxDepth capped at %d, got %d", MaxHardDepth, d.maxDepth)
	}
}

func TestTaskStore_SubmitAndComplete(t *testing.T) {
	ts := NewTaskStore(5, 1*time.Second)
	defer ts.Close()

	entry, err := ts.Submit("task-1")
	if err != nil {
		t.Fatalf("submit error: %v", err)
	}
	if entry.Status != TaskStatusRunning {
		t.Errorf("expected running, got %s", entry.Status)
	}

	ts.Complete("task-1", &Result{Response: "done"})

	got, ok := ts.Get("task-1")
	if !ok {
		t.Fatal("task not found")
	}
	if got.Status != TaskStatusCompleted {
		t.Errorf("expected completed, got %s", got.Status)
	}
	if got.Result.Response != "done" {
		t.Errorf("expected response 'done', got %q", got.Result.Response)
	}
}

func TestTaskStore_SubmitAndFail(t *testing.T) {
	ts := NewTaskStore(5, 1*time.Second)
	defer ts.Close()

	ts.Submit("task-2")
	ts.Fail("task-2", fmt.Errorf("something broke"))

	got, ok := ts.Get("task-2")
	if !ok {
		t.Fatal("task not found")
	}
	if got.Status != TaskStatusFailed {
		t.Errorf("expected failed, got %s", got.Status)
	}
}

func TestTaskStore_Wait(t *testing.T) {
	ts := NewTaskStore(5, 1*time.Second)
	defer ts.Close()

	ts.Submit("task-3")

	go func() {
		time.Sleep(50 * time.Millisecond)
		ts.Complete("task-3", &Result{Response: "waited"})
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	entry, err := ts.Wait(ctx, "task-3")
	if err != nil {
		t.Fatalf("wait error: %v", err)
	}
	if entry.Result.Response != "waited" {
		t.Errorf("expected 'waited', got %q", entry.Result.Response)
	}
}

func TestTaskStore_WaitTimeout(t *testing.T) {
	ts := NewTaskStore(5, 1*time.Second)
	defer ts.Close()

	ts.Submit("task-4")

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err := ts.Wait(ctx, "task-4")
	if err == nil {
		t.Fatal("expected timeout error")
	}

	// Clean up to release semaphore
	ts.Complete("task-4", &Result{})
}

func TestTaskStore_NotFound(t *testing.T) {
	ts := NewTaskStore(5, 1*time.Second)
	defer ts.Close()

	_, ok := ts.Get("nonexistent")
	if ok {
		t.Error("expected not found")
	}
}

func TestMergeToolSpecs(t *testing.T) {
	base := []ToolSpec{
		{Name: "a", Description: "base-a"},
		{Name: "b", Description: "base-b"},
	}
	overrides := []ToolSpec{
		{Name: "b", Description: "override-b"},
		{Name: "c", Description: "new-c"},
	}

	result := mergeToolSpecs(base, overrides)

	if len(result) != 3 {
		t.Fatalf("expected 3 tools, got %d", len(result))
	}

	// Check b was overridden
	for _, t2 := range result {
		if t2.Name == "b" && t2.Description != "override-b" {
			t.Errorf("expected b to be overridden, got %q", t2.Description)
		}
	}
}

func TestAgentResolver(t *testing.T) {
	d := NewDelegator(DelegatorConfig{
		AgentResolver: func(name string) (*ResolvedAgent, error) {
			if name == "test-agent" {
				return &ResolvedAgent{
					SystemPrompt: "You are test",
					Model:        "gpt-4o-mini",
					Tools: []ToolSpec{
						{Name: "hello", Description: "says hi", Script: "def run(args):\n    return \"hi\""},
					},
				}, nil
			}
			return nil, fmt.Errorf("not found")
		},
	})

	// Request with agent name but no tools
	ctx := context.Background()
	_, err := d.Execute(ctx, Request{
		Task:  "say hello",
		Agent: "unknown-agent",
	})
	if err == nil {
		t.Fatal("expected error for unknown agent")
	}

	// Can't fully test success without a real completion client, but verify depth works
	ctx = WithDepth(ctx, 3) // at max depth (default 3)
	_, err = d.Execute(ctx, Request{
		Task:  "say hello",
		Agent: "test-agent",
	})
	if err == nil || !contains(err.Error(), "depth limit") {
		t.Errorf("expected depth limit, got: %v", err)
	}
}

func contains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
