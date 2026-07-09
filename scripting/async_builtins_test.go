package scripting

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/htekdev/ai-harness/async"
	"github.com/htekdev/ai-harness/tools"
)

// mockAsyncToolCaller is a simple ToolCaller for testing.
type mockAsyncToolCaller struct {
	handler func(ctx context.Context, call tools.Call) tools.Result
}

func (m *mockAsyncToolCaller) Execute(ctx context.Context, call tools.Call) tools.Result {
	if m.handler != nil {
		return m.handler(ctx, call)
	}
	return tools.Result{CallID: call.ID, Name: call.Name, Content: "echo:" + call.Name}
}

// makeAsyncCtx builds a context with an async executor wired up.
func makeAsyncCtx(maxConcurrent int, caller async.ToolCaller) context.Context {
	exec := async.NewExecutor(maxConcurrent, caller)
	ctx := WithTurnState(context.Background())
	return async.WithExecutor(ctx, exec)
}

func TestAsyncLaunchWaitAll(t *testing.T) {
	caller := &mockAsyncToolCaller{
		handler: func(_ context.Context, call tools.Call) tools.Result {
			return tools.Result{CallID: call.ID, Name: call.Name, Content: "result-" + call.Name}
		},
	}
	ctx := makeAsyncCtx(4, caller)

	engine := NewEngine()
	runner, err := engine.CompileToolScript("async_fanout", `
def run(args):
    refs = []
    for i in range(1, 4):
        ref = parallel.launch("test_tool_" + str(i), {"n": i})
        refs.append(ref)
    results = parallel.wait_all(refs)
    parts = [r["result"] for r in results]
    return json.encode({"count": len(parts), "first": parts[0]})
`)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	out, err := runner.Run(ctx, json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("run: %v", err)
	}

	var payload struct {
		Count int    `json:"count"`
		First string `json:"first"`
	}
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if payload.Count != 3 {
		t.Errorf("expected count=3, got %d", payload.Count)
	}
	if !strings.HasPrefix(payload.First, "result-") {
		t.Errorf("unexpected first result: %q", payload.First)
	}
}

func TestAsyncLaunchNoExecutorError(t *testing.T) {
	engine := NewEngine()
	runner, err := engine.CompileToolScript("no_exec", `
def run(args):
    ref = parallel.launch("some_tool", {})
    return "launched"
`)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	// Context has no executor — should return an error.
	ctx := WithTurnState(context.Background())
	_, err = runner.Run(ctx, json.RawMessage(`{}`))
	if err == nil {
		t.Fatal("expected error when no executor in context")
	}
	if !strings.Contains(err.Error(), "parallel executor not available") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestAsyncWaitAll_ErrorResult(t *testing.T) {
	caller := &mockAsyncToolCaller{
		handler: func(_ context.Context, call tools.Call) tools.Result {
			return tools.Result{CallID: call.ID, Name: call.Name, Content: "tool failed", IsError: true}
		},
	}
	ctx := makeAsyncCtx(4, caller)

	engine := NewEngine()
	runner, err := engine.CompileToolScript("async_err", `
def run(args):
    ref = parallel.launch("failing_tool", {})
    results = parallel.wait_all([ref])
    r = results[0]
    return json.encode({"is_error": r["is_error"], "result": r["result"]})
`)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	out, err := runner.Run(ctx, json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("run: %v", err)
	}

	var payload struct {
		IsError bool   `json:"is_error"`
		Result  string `json:"result"`
	}
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !payload.IsError {
		t.Errorf("expected is_error=true")
	}
}

func TestAsyncRace(t *testing.T) {
	caller := &mockAsyncToolCaller{
		handler: func(_ context.Context, call tools.Call) tools.Result {
			return tools.Result{CallID: call.ID, Name: call.Name, Content: "winner"}
		},
	}
	ctx := makeAsyncCtx(4, caller)

	engine := NewEngine()
	runner, err := engine.CompileToolScript("async_race", `
def run(args):
    ref_a = parallel.launch("tool_a", {})
    ref_b = parallel.launch("tool_b", {})
    winner = parallel.race([ref_a, ref_b])
    return winner["result"]
`)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	out, err := runner.Run(ctx, json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if out != "winner" {
		t.Errorf("expected 'winner', got %q", out)
	}
}

func TestAsyncDependsOn(t *testing.T) {
	var callOrder []string
	caller := &mockAsyncToolCaller{
		handler: func(_ context.Context, call tools.Call) tools.Result {
			callOrder = append(callOrder, call.Name)
			return tools.Result{CallID: call.ID, Name: call.Name, Content: call.Name + "-done"}
		},
	}
	ctx := makeAsyncCtx(4, caller)

	engine := NewEngine()
	runner, err := engine.CompileToolScript("async_deps", `
def run(args):
    ref_a = parallel.launch("tool_a", {})
    ref_b = parallel.launch("tool_b", {}, depends_on=[ref_a])
    results = parallel.wait_all([ref_a, ref_b])
    return json.encode([r["result"] for r in results])
`)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	out, err := runner.Run(ctx, json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("run: %v", err)
	}

	var parts []string
	if err := json.Unmarshal([]byte(out), &parts); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(parts) != 2 {
		t.Errorf("expected 2 results, got %d", len(parts))
	}
	if parts[0] != "tool_a-done" || parts[1] != "tool_b-done" {
		t.Errorf("unexpected results: %v", parts)
	}
}
