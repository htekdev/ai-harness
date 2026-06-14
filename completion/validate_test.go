package completion

import (
	"errors"
	"strings"
	"testing"

	"github.com/htekdev/ai-harness/harness/errs"
)

func TestValidateMessages_HappyPath(t *testing.T) {
	msgs := []Message{
		{Role: RoleSystem, Content: "system"},
		{Role: RoleUser, Content: "hi"},
		{Role: RoleAssistant, ToolCalls: []ToolCall{
			{ID: "call_1", Type: "function", Function: FunctionCall{Name: "x", Arguments: "{}"}},
		}},
		{Role: RoleTool, ToolCallID: "call_1", Content: "ok"},
		{Role: RoleAssistant, Content: "done"},
		{Role: RoleUser, Content: "next"},
	}
	if err := ValidateMessages(msgs); err != nil {
		t.Fatalf("expected nil, got %v", err)
	}
}

func TestValidateMessages_OrphanToolMessage(t *testing.T) {
	// Reproduces issue #89: tool result with no preceding assistant tool_calls.
	msgs := []Message{
		{Role: RoleSystem, Content: "system"},
		{Role: RoleUser, Content: "hi"},
		{Role: RoleTool, ToolCallID: "call_orphan", Content: "result"},
	}
	err := ValidateMessages(msgs)
	if err == nil {
		t.Fatal("expected validation error, got nil")
	}
	if errs.KindOf(err) != errs.KindInvalidConversation {
		t.Fatalf("expected KindInvalidConversation, got %v", errs.KindOf(err))
	}
	if !strings.Contains(err.Error(), "not preceded by an assistant tool_calls") {
		t.Fatalf("unexpected message: %v", err)
	}
}

func TestValidateMessages_ToolAfterAssistantWithoutToolCalls(t *testing.T) {
	msgs := []Message{
		{Role: RoleUser, Content: "hi"},
		{Role: RoleAssistant, Content: "no tools used"},
		{Role: RoleTool, ToolCallID: "call_x", Content: "stale"},
	}
	if err := ValidateMessages(msgs); err == nil {
		t.Fatal("expected validation error, got nil")
	}
}

func TestValidateMessages_UserBetweenAssistantAndTool(t *testing.T) {
	// A user turn between the assistant tool_calls and the tool result
	// invalidates the window — the provider rejects this layout.
	msgs := []Message{
		{Role: RoleAssistant, ToolCalls: []ToolCall{{ID: "c1", Type: "function", Function: FunctionCall{Name: "f"}}}},
		{Role: RoleUser, Content: "interrupt"},
		{Role: RoleTool, ToolCallID: "c1", Content: "late"},
	}
	if err := ValidateMessages(msgs); err == nil {
		t.Fatal("expected validation error, got nil")
	}
}

func TestValidateMessages_UnknownToolCallID(t *testing.T) {
	msgs := []Message{
		{Role: RoleAssistant, ToolCalls: []ToolCall{{ID: "c1", Type: "function", Function: FunctionCall{Name: "f"}}}},
		{Role: RoleTool, ToolCallID: "c2", Content: "wrong id"},
	}
	if err := ValidateMessages(msgs); err == nil {
		t.Fatal("expected validation error, got nil")
	}
}

func TestValidateMessages_DuplicateToolMessage(t *testing.T) {
	msgs := []Message{
		{Role: RoleAssistant, ToolCalls: []ToolCall{{ID: "c1", Type: "function", Function: FunctionCall{Name: "f"}}}},
		{Role: RoleTool, ToolCallID: "c1", Content: "first"},
		{Role: RoleTool, ToolCallID: "c1", Content: "duplicate"},
	}
	if err := ValidateMessages(msgs); err == nil {
		t.Fatal("expected validation error on duplicate tool result")
	}
}

func TestValidateMessages_EmptyToolCallID(t *testing.T) {
	msgs := []Message{
		{Role: RoleAssistant, ToolCalls: []ToolCall{{ID: "c1", Type: "function", Function: FunctionCall{Name: "f"}}}},
		{Role: RoleTool, Content: "missing id"},
	}
	if err := ValidateMessages(msgs); err == nil {
		t.Fatal("expected validation error on empty tool_call_id")
	}
}

func TestValidateMessages_AssistantToolCallEmptyID(t *testing.T) {
	msgs := []Message{
		{Role: RoleAssistant, ToolCalls: []ToolCall{{ID: "", Type: "function", Function: FunctionCall{Name: "f"}}}},
	}
	if err := ValidateMessages(msgs); err == nil {
		t.Fatal("expected validation error on assistant tool_call with empty id")
	}
}

func TestValidateMessages_ParallelToolCalls(t *testing.T) {
	msgs := []Message{
		{Role: RoleAssistant, ToolCalls: []ToolCall{
			{ID: "c1", Type: "function", Function: FunctionCall{Name: "f"}},
			{ID: "c2", Type: "function", Function: FunctionCall{Name: "g"}},
		}},
		{Role: RoleTool, ToolCallID: "c1", Content: "r1"},
		{Role: RoleTool, ToolCallID: "c2", Content: "r2"},
	}
	if err := ValidateMessages(msgs); err != nil {
		t.Fatalf("expected nil for parallel tool calls, got %v", err)
	}
}

func TestValidateMessages_EmptyAndNoTools(t *testing.T) {
	if err := ValidateMessages(nil); err != nil {
		t.Fatalf("nil messages should pass, got %v", err)
	}
	msgs := []Message{
		{Role: RoleSystem, Content: "s"},
		{Role: RoleUser, Content: "u"},
		{Role: RoleAssistant, Content: "a"},
	}
	if err := ValidateMessages(msgs); err != nil {
		t.Fatalf("plain conversation should pass, got %v", err)
	}
}

func TestValidateMessages_ErrorIsTypedAndMatchable(t *testing.T) {
	msgs := []Message{{Role: RoleTool, ToolCallID: "x", Content: "orphan"}}
	err := ValidateMessages(msgs)
	if err == nil {
		t.Fatal("expected error")
	}
	// errors.Is via Kind match.
	if !errors.Is(err, &errs.Error{Kind: errs.KindInvalidConversation}) {
		t.Fatalf("errors.Is should match by Kind, got %v", err)
	}
}
