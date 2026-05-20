package scripting

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"go.starlark.net/starlark"

	"github.com/htekdev/ai-harness/config"
	"github.com/htekdev/ai-harness/hooks"
	"github.com/htekdev/ai-harness/tools"
)

// newTestMetaEngine creates an Engine with meta built-ins enabled for testing.
func newTestMetaEngine() (*Engine, *MetaContext) {
	engine := NewEngine()
	registry := tools.NewRegistry()
	hookSystem := hooks.NewSystem()
	agents := make(map[string]*config.AgentConfig)

	mc := &MetaContext{
		Registry:   registry,
		HookSystem: hookSystem,
		Agents:     agents,
		Engine:     engine,
		Config:     DefaultMetaConfig(),
	}
	engine.SetMetaContext(mc)
	return engine, mc
}

func TestMetaRegisterTool(t *testing.T) {
	engine, mc := newTestMetaEngine()

	script := `meta.register_tool(
    name = "factorial",
    description = "Compute factorial",
    parameters = {"n": {"type": "number", "description": "Input", "required": True}},
    script = "def run(args):\n    n = int(args['n'])\n    result = 1\n    for i in range(1, n + 1):\n        result = result * i\n    return str(result)\n",
)`
	execMetaScript(t, engine, script)

	// Verify the tool was registered.
	def, found := mc.Registry.Get("factorial")
	if !found {
		t.Fatal("tool 'factorial' not found in registry")
	}
	if def.Description != "Compute factorial" {
		t.Errorf("description = %q, want %q", def.Description, "Compute factorial")
	}
	if len(def.Parameters) != 1 {
		t.Errorf("parameter count = %d, want 1", len(def.Parameters))
	}

	// Invoke the tool.
	result := mc.Registry.Execute(context.Background(), tools.Call{
		ID:        "test-1",
		Name:      "factorial",
		Arguments: json.RawMessage(`{"n": "5"}`),
	})
	if result.IsError {
		t.Fatalf("tool execution error: %s", result.Content)
	}
	if result.Content != "120" {
		t.Errorf("result = %q, want %q", result.Content, "120")
	}
}

func TestMetaRegisterToolReplace(t *testing.T) {
	engine, mc := newTestMetaEngine()

	// Register a tool first.
	script1 := `meta.register_tool(
    name = "greeting",
    description = "Say hello",
    script = "def run(args):\n    return 'hello'\n",
)`
	execMetaScript(t, engine, script1)

	// Try to register again without replace — should fail.
	script2 := `meta.register_tool(
    name = "greeting",
    description = "Say hello v2",
    script = "def run(args):\n    return 'hi'\n",
)`
	err := execMetaScriptErr(engine, script2)
	if err == nil {
		t.Fatal("expected error for duplicate registration")
	}
	if !strings.Contains(err.Error(), "already registered") {
		t.Fatalf("unexpected error: %v", err)
	}

	// Replace should work.
	script3 := `meta.register_tool(
    name = "greeting",
    description = "Say hello v2",
    script = "def run(args):\n    return 'hi'\n",
    replace = True,
)`
	execMetaScript(t, engine, script3)

	result := mc.Registry.Execute(context.Background(), tools.Call{
		ID:        "test-2",
		Name:      "greeting",
		Arguments: json.RawMessage(`{}`),
	})
	if result.Content != "hi" {
		t.Errorf("result = %q, want %q", result.Content, "hi")
	}
}

func TestMetaRegisterToolInvalidName(t *testing.T) {
	engine, _ := newTestMetaEngine()

	script := `meta.register_tool(
    name = "INVALID NAME!",
    description = "bad",
    script = "def run(args):\n    return 'x'\n",
)`
	err := execMetaScriptErr(engine, script)
	if err == nil {
		t.Fatal("expected error for invalid name")
	}
	if !strings.Contains(err.Error(), "invalid name") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestMetaRegisterToolLimit(t *testing.T) {
	engine, mc := newTestMetaEngine()
	mc.Config.MaxTools = 2

	for i := 0; i < 2; i++ {
		name := "tool-" + string(rune('a'+i))
		script := `meta.register_tool(name = "` + name + `", description = "tool", script = "def run(args):\n    return 'ok'\n")`
		execMetaScript(t, engine, script)
	}

	script := `meta.register_tool(name = "tool-c", description = "tool", script = "def run(args):\n    return 'ok'\n")`
	err := execMetaScriptErr(engine, script)
	if err == nil {
		t.Fatal("expected limit error")
	}
	if !strings.Contains(err.Error(), "registration limit") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestMetaRegisterHook(t *testing.T) {
	engine, mc := newTestMetaEngine()

	script := `meta.register_hook(
    name = "test-guard",
    event = "tool.pre",
    script = "def handle(event, payload):\n    if payload.get('name') == 'blocked-tool':\n        return block('not allowed')\n    return allow()\n",
)`
	execMetaScript(t, engine, script)

	// Verify hook was registered.
	handlers := mc.HookSystem.HandlersFor(hooks.EventToolPre)
	if len(handlers) != 1 {
		t.Fatalf("handler count = %d, want 1", len(handlers))
	}
	if handlers[0].Name != "test-guard" {
		t.Errorf("handler name = %q, want %q", handlers[0].Name, "test-guard")
	}

	// Test that the hook blocks.
	result := mc.HookSystem.Dispatch(context.Background(), hooks.EventToolPre, map[string]any{
		"name": "blocked-tool",
	})
	if result.Action != hooks.ActionBlock {
		t.Errorf("expected ActionBlock, got %d", result.Action)
	}

	// Test that allowed tools pass.
	result = mc.HookSystem.Dispatch(context.Background(), hooks.EventToolPre, map[string]any{
		"name": "safe-tool",
	})
	if result.Action != hooks.ActionContinue {
		t.Errorf("expected ActionContinue, got %d", result.Action)
	}
}

func TestMetaRegisterHookPriorityFloor(t *testing.T) {
	engine, mc := newTestMetaEngine()

	script := `meta.register_hook(
    name = "low-priority",
    event = "tool.pre",
    priority = 10,
    script = "def handle(event, payload):\n    return allow()\n",
)`
	execMetaScript(t, engine, script)

	handlers := mc.HookSystem.HandlersFor(hooks.EventToolPre)
	if len(handlers) != 1 {
		t.Fatalf("handler count = %d, want 1", len(handlers))
	}
	if handlers[0].Priority < 50 {
		t.Errorf("priority = %d, want >= 50 (floor enforcement)", handlers[0].Priority)
	}
}

func TestMetaRegisterHookBlocksMetaHookEvent(t *testing.T) {
	engine, _ := newTestMetaEngine()

	script := `meta.register_hook(
    name = "bad-hook",
    event = "meta.register_hook",
    script = "def handle(event, payload):\n    return allow()\n",
)`
	err := execMetaScriptErr(engine, script)
	if err == nil {
		t.Fatal("expected error for meta.register_hook event")
	}
	if !strings.Contains(err.Error(), "cannot register hooks for meta.register_hook") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestMetaRegisterAgent(t *testing.T) {
	engine, mc := newTestMetaEngine()

	script := `meta.register_agent(
    name = "math-specialist",
    model = "gpt-4o-mini",
    system_prompt = "You are a math expert.",
    tools = ["calculator", "factorial"],
)`
	execMetaScript(t, engine, script)

	agent, ok := mc.Agents["math-specialist"]
	if !ok {
		t.Fatal("agent 'math-specialist' not found")
	}
	if agent.Model != "gpt-4o-mini" {
		t.Errorf("model = %q, want %q", agent.Model, "gpt-4o-mini")
	}
	if agent.SystemPrompt != "You are a math expert." {
		t.Errorf("system_prompt = %q, want %q", agent.SystemPrompt, "You are a math expert.")
	}
	if len(agent.Tools) != 2 {
		t.Errorf("tools count = %d, want 2", len(agent.Tools))
	}
}

func TestMetaCallTool(t *testing.T) {
	engine, mc := newTestMetaEngine()

	// Register a tool via Go.
	_ = mc.Registry.Register(tools.Definition{
		Name:        "double",
		Description: "Double a number",
		Parameters:  []tools.Parameter{{Name: "n", Type: tools.TypeNumber}},
	}, func(ctx context.Context, args json.RawMessage) (string, error) {
		var a struct{ N int `json:"n"` }
		json.Unmarshal(args, &a)
		b, _ := json.Marshal(a.N * 2)
		return string(b), nil
	})

	script := `
def run(args):
    result = meta.call_tool("double", {"n": 21})
    return result
`
	runner, err := engine.CompileToolScript("test-call", script)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	result, err := runner.Run(context.Background(), json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if result != "42" {
		t.Errorf("result = %q, want %q", result, "42")
	}
}

func TestMetaCallToolRecursionLimit(t *testing.T) {
	engine, mc := newTestMetaEngine()
	mc.Config.MaxCallDepth = 3

	// Register a recursive tool via meta.
	script := `meta.register_tool(
    name = "recursive-tool",
    description = "Recursion test",
    script = "def run(args):\n    return meta.call_tool('recursive-tool', {})\n",
)`
	execMetaScript(t, engine, script)

	// Call it — should eventually hit depth limit.
	result := mc.Registry.Execute(context.Background(), tools.Call{
		ID:        "test-recurse",
		Name:      "recursive-tool",
		Arguments: json.RawMessage(`{}`),
	})
	// The result should contain a depth error (propagated as tool output).
	if !strings.Contains(result.Content, "maximum call depth") {
		t.Errorf("expected depth limit error, got: %s", result.Content)
	}
}

func TestMetaCallToolHookBlocks(t *testing.T) {
	engine, mc := newTestMetaEngine()

	// Register a governance hook that blocks "forbidden-tool".
	mc.HookSystem.Register(hooks.Registration{
		Name:     "governance",
		Event:    hooks.EventToolPre,
		Priority: 10,
		Handler: func(ctx context.Context, event hooks.Event, payload any) hooks.Result {
			if m, ok := payload.(map[string]any); ok {
				if m["name"] == "forbidden-tool" {
					return hooks.Result{Action: hooks.ActionBlock, Reason: "forbidden"}
				}
			}
			return hooks.Result{Action: hooks.ActionContinue}
		},
	})

	_ = mc.Registry.Register(tools.Definition{
		Name: "forbidden-tool", Description: "forbidden",
	}, func(ctx context.Context, args json.RawMessage) (string, error) {
		return "secret", nil
	})

	script := `
def run(args):
    return meta.call_tool("forbidden-tool", {})
`
	runner, err := engine.CompileToolScript("test-block", script)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	result, err := runner.Run(context.Background(), json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(result, "blocked") {
		t.Errorf("expected blocked message, got: %q", result)
	}
}

func TestMetaListTools(t *testing.T) {
	engine, mc := newTestMetaEngine()

	_ = mc.Registry.Register(tools.Definition{
		Name: "alpha", Description: "Alpha tool",
		Parameters: []tools.Parameter{{Name: "x", Type: tools.TypeString}},
	}, func(ctx context.Context, args json.RawMessage) (string, error) { return "", nil })

	_ = mc.Registry.Register(tools.Definition{
		Name: "beta-tool", Description: "Beta tool",
	}, func(ctx context.Context, args json.RawMessage) (string, error) { return "", nil })

	_ = mc.Registry.Register(tools.Definition{
		Name: "gamma", Description: "Gamma tool",
	}, func(ctx context.Context, args json.RawMessage) (string, error) { return "", nil })

	// List all tools.
	script := `
def run(args):
    tools = meta.list_tools()
    return str(len(tools))
`
	runner, err := engine.CompileToolScript("test-list", script)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	result, err := runner.Run(context.Background(), json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if result != "3" {
		t.Errorf("result = %q, want %q", result, "3")
	}

	// List with pattern.
	script2 := `
def run(args):
    tools = meta.list_tools(pattern = "beta-*")
    return str(len(tools))
`
	runner2, err := engine.CompileToolScript("test-list-pattern", script2)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	result2, err := runner2.Run(context.Background(), json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if result2 != "1" {
		t.Errorf("result = %q, want %q", result2, "1")
	}
}

func TestMetaGovernanceBlocksRegistration(t *testing.T) {
	engine, mc := newTestMetaEngine()

	// Governance hook that blocks tool registration.
	mc.HookSystem.Register(hooks.Registration{
		Name:     "no-tools",
		Event:    "meta.register_tool",
		Priority: 10,
		Handler: func(ctx context.Context, event hooks.Event, payload any) hooks.Result {
			return hooks.Result{Action: hooks.ActionBlock, Reason: "tool registration disabled"}
		},
	})

	script := `meta.register_tool(
    name = "sneaky",
    description = "should not work",
    script = "def run(args):\n    return 'nope'\n",
)`
	err := execMetaScriptErr(engine, script)
	if err == nil {
		t.Fatal("expected governance block error")
	}
	if !strings.Contains(err.Error(), "blocked by governance") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestMetaNotEnabled(t *testing.T) {
	engine := NewEngine()
	// No meta context set — meta module should not exist in builtins.
	script := `x = meta.list_tools()`
	err := func() error {
		thread := &starlark.Thread{Name: "no-meta"}
		_, err := starlark.ExecFile(thread, "test.star", script, engine.builtins)
		return err
	}()
	if err == nil {
		t.Fatal("expected error for undefined 'meta'")
	}
	if !strings.Contains(err.Error(), "meta") {
		t.Fatalf("unexpected error: %v", err)
	}
}

// --- Helpers ---

// execMetaScript executes a standalone Starlark script with the meta-enabled builtins.
func execMetaScript(t *testing.T, engine *Engine, script string) {
	t.Helper()
	thread := &starlark.Thread{Name: "meta-test"}
	thread.SetLocal(threadContextKey, context.Background())
	_, err := starlark.ExecFile(thread, "meta-test.star", script, engine.builtins)
	if err != nil {
		t.Fatalf("exec: %v", err)
	}
}

func execMetaScriptErr(engine *Engine, script string) error {
	thread := &starlark.Thread{Name: "meta-test"}
	thread.SetLocal(threadContextKey, context.Background())
	_, err := starlark.ExecFile(thread, "meta-test.star", script, engine.builtins)
	return err
}
