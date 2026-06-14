package delegation

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/htekdev/ai-harness/scripting"
)

func TestDelegateToolDefinition(t *testing.T) {
	def := DelegateToolDefinition()
	if def.Name != "delegate" {
		t.Errorf("expected name 'delegate', got %q", def.Name)
	}
	if len(def.Parameters) != 8 {
		t.Errorf("expected 8 parameters, got %d", len(def.Parameters))
	}
}

func TestConvertParams(t *testing.T) {
	params := map[string]ParamSpec{
		"name": {Type: "string", Description: "A name", Required: true},
		"age":  {Type: "number", Description: "An age", Required: false},
	}

	result := convertParams(params)
	if len(result) != 2 {
		t.Fatalf("expected 2 params, got %d", len(result))
	}

	found := map[string]bool{}
	for _, p := range result {
		found[p.Name] = true
	}
	if !found["name"] || !found["age"] {
		t.Errorf("missing expected params: %v", found)
	}
}

func TestDelegator_CreateDelegateToolHandler_MissingTask(t *testing.T) {
	engine := scripting.NewEngine()
	d := NewDelegator(DelegatorConfig{
		Engine: engine,
	})

	handler := d.CreateDelegateToolHandler()
	args := json.RawMessage(`{"tools":[{"name":"t","description":"d","parameters":{},"script":"def run(args):\n    return \"ok\""}]}`)
	_, err := handler(context.Background(), args)
	if err == nil {
		t.Fatal("expected error for missing task")
	}
}

func TestDelegator_CreateDelegateToolHandler_MissingTools(t *testing.T) {
	engine := scripting.NewEngine()
	d := NewDelegator(DelegatorConfig{
		Engine: engine,
	})

	handler := d.CreateDelegateToolHandler()
	args := json.RawMessage(`{"task":"do something","tools":[]}`)
	_, err := handler(context.Background(), args)
	if err == nil {
		t.Fatal("expected error for empty tools")
	}
}

func TestDelegator_CreateDelegateToolHandler_BadScript(t *testing.T) {
	engine := scripting.NewEngine()
	d := NewDelegator(DelegatorConfig{
		Engine: engine,
	})

	handler := d.CreateDelegateToolHandler()
	args := json.RawMessage(`{"task":"test","tools":[{"name":"bad","description":"d","parameters":{},"script":"this is not valid starlark!!!"}]}`)
	_, err := handler(context.Background(), args)
	if err == nil {
		t.Fatal("expected error for bad script")
	}
}

func TestRequest_Unmarshal(t *testing.T) {
	data := `{
		"task": "do the thing",
		"tools": [
			{
				"name": "tool1",
				"description": "a tool",
				"parameters": {"x": {"type": "string", "required": true}},
				"script": "def run(args):\n    return args[\"x\"]"
			}
		],
		"hooks": [
			{
				"event": "tool.pre",
				"handler": "guard",
				"when": "payload.get(\"name\", \"\") == \"tool1\"",
				"script": "def handle(event, payload):\n    return continue()"
			}
		],
		"system_prompt": "You are helpful."
	}`

	var req Request
	if err := json.Unmarshal([]byte(data), &req); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if req.Task != "do the thing" {
		t.Errorf("task: %q", req.Task)
	}
	if len(req.Tools) != 1 {
		t.Errorf("tools: %d", len(req.Tools))
	}
	if len(req.Hooks) != 1 {
		t.Errorf("hooks: %d", len(req.Hooks))
	}
	if req.Hooks[0].When != `payload.get("name", "") == "tool1"` {
		t.Errorf("unexpected when: %q", req.Hooks[0].When)
	}
	if req.SystemPrompt != "You are helpful." {
		t.Errorf("system_prompt: %q", req.SystemPrompt)
	}
}
