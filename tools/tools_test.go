package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
)

func echoHandler(ctx context.Context, args json.RawMessage) (string, error) {
	return string(args), nil
}

func TestRegisterAndExecute(t *testing.T) {
	reg := NewRegistry()

	def := Definition{
		Name:        "echo",
		Description: "Echoes back the input",
		Parameters: []Parameter{
			{Name: "message", Type: TypeString, Description: "Message to echo", Required: true},
		},
	}

	err := reg.Register(def, echoHandler)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	result := reg.Execute(context.Background(), Call{
		ID:        "call-1",
		Name:      "echo",
		Arguments: json.RawMessage(`{"message":"hello"}`),
	})

	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content)
	}
	if result.CallID != "call-1" {
		t.Fatalf("expected call ID 'call-1', got %q", result.CallID)
	}
	if result.Content != `{"message":"hello"}` {
		t.Fatalf("unexpected content: %s", result.Content)
	}
}

func TestRegisterDuplicate(t *testing.T) {
	reg := NewRegistry()
	def := Definition{Name: "dup", Description: "test"}

	err := reg.Register(def, echoHandler)
	if err != nil {
		t.Fatal(err)
	}

	err = reg.Register(def, echoHandler)
	if err == nil {
		t.Fatal("expected error for duplicate registration")
	}
}

func TestRegisterEmptyName(t *testing.T) {
	reg := NewRegistry()
	err := reg.Register(Definition{Name: "", Description: "bad"}, echoHandler)
	if err == nil {
		t.Fatal("expected error for empty name")
	}
}

func TestRegisterNilHandler(t *testing.T) {
	reg := NewRegistry()
	err := reg.Register(Definition{Name: "valid", Description: "test"}, nil)
	if err == nil {
		t.Fatal("expected error for nil handler")
	}
}

func TestExecuteNonExistent(t *testing.T) {
	reg := NewRegistry()
	result := reg.Execute(context.Background(), Call{ID: "x", Name: "ghost", Arguments: nil})
	if !result.IsError {
		t.Fatal("expected error for non-existent tool")
	}
}

func TestUnregister(t *testing.T) {
	reg := NewRegistry()
	reg.Register(Definition{Name: "temp", Description: "temporary"}, echoHandler)

	removed := reg.Unregister("temp")
	if !removed {
		t.Fatal("expected Unregister to return true")
	}

	removed = reg.Unregister("temp")
	if removed {
		t.Fatal("expected Unregister to return false for already-removed tool")
	}
}

func TestList(t *testing.T) {
	reg := NewRegistry()
	reg.Register(Definition{Name: "a", Description: "tool a"}, echoHandler)
	reg.Register(Definition{Name: "b", Description: "tool b"}, echoHandler)

	defs := reg.List()
	if len(defs) != 2 {
		t.Fatalf("expected 2 tools, got %d", len(defs))
	}
}

func TestGet(t *testing.T) {
	reg := NewRegistry()
	reg.Register(Definition{Name: "finder", Description: "find things"}, echoHandler)

	def, ok := reg.Get("finder")
	if !ok {
		t.Fatal("expected to find tool")
	}
	if def.Description != "find things" {
		t.Fatalf("unexpected description: %s", def.Description)
	}

	_, ok = reg.Get("nonexistent")
	if ok {
		t.Fatal("expected not to find nonexistent tool")
	}
}

func TestToOpenAIFormat(t *testing.T) {
	reg := NewRegistry()
	reg.Register(Definition{
		Name:        "search",
		Description: "Search for things",
		Parameters: []Parameter{
			{Name: "query", Type: TypeString, Description: "Search query", Required: true},
			{Name: "limit", Type: TypeNumber, Description: "Max results", Required: false},
		},
	}, echoHandler)

	format := reg.ToOpenAIFormat()
	if len(format) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(format))
	}

	tool := format[0]
	if tool["type"] != "function" {
		t.Fatalf("expected type 'function', got %v", tool["type"])
	}

	fn := tool["function"].(map[string]any)
	if fn["name"] != "search" {
		t.Fatalf("expected name 'search', got %v", fn["name"])
	}

	params := fn["parameters"].(map[string]any)
	required := params["required"].([]string)
	if len(required) != 1 || required[0] != "query" {
		t.Fatalf("unexpected required: %v", required)
	}
}

func TestExecuteWithError(t *testing.T) {
	reg := NewRegistry()
	reg.Register(Definition{Name: "fail", Description: "always fails"}, func(ctx context.Context, args json.RawMessage) (string, error) {
		return "", fmt.Errorf("intentional failure")
	})

	result := reg.Execute(context.Background(), Call{ID: "c1", Name: "fail"})
	if !result.IsError {
		t.Fatal("expected error result")
	}
	if result.Content == "" {
		t.Fatal("expected error message in content")
	}
}
