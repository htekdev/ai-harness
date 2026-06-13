package tools

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/htekdev/ai-harness/harness/errs"
)

// Phase 5.3: tool registry errors are typed as KindTool so hooks/dashboards
// can react without parsing message strings.

func TestRegister_EmptyName_IsKindTool(t *testing.T) {
	r := NewRegistry()
	err := r.Register(Definition{}, func(context.Context, json.RawMessage) (string, error) { return "", nil })
	if err == nil {
		t.Fatal("expected error for empty tool name")
	}
	if k := errs.KindOf(err); k != errs.KindTool {
		t.Fatalf("KindOf = %v, want KindTool", k)
	}
	var typed *errs.Error
	if !errors.As(err, &typed) || typed.Op != "tools.register" {
		t.Fatalf("expected typed error with op=tools.register, got %v", err)
	}
}

func TestRegister_NilHandler_IsKindTool(t *testing.T) {
	r := NewRegistry()
	err := r.Register(Definition{Name: "x"}, nil)
	if k := errs.KindOf(err); k != errs.KindTool {
		t.Fatalf("KindOf = %v, want KindTool", k)
	}
}

func TestRegister_Duplicate_IsKindTool(t *testing.T) {
	r := NewRegistry()
	def := Definition{Name: "x"}
	h := Handler(func(context.Context, json.RawMessage) (string, error) { return "", nil })
	if err := r.Register(def, h); err != nil {
		t.Fatalf("first register: %v", err)
	}
	err := r.Register(def, h)
	if k := errs.KindOf(err); k != errs.KindTool {
		t.Fatalf("KindOf = %v, want KindTool", k)
	}
}
