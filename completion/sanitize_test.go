package completion

import (
	"testing"
)

func TestSanitizeMessages_DropsOrphanToolMessage(t *testing.T) {
	in := []Message{
		{Role: RoleSystem, Content: "s"},
		{Role: RoleUser, Content: "u"},
		{Role: RoleTool, ToolCallID: "call_orphan", Content: "stale"},
	}
	out := SanitizeMessages(in)
	if len(out) != 2 {
		t.Fatalf("expected orphan to be dropped, got %+v", out)
	}
	if err := ValidateMessages(out); err != nil {
		t.Fatalf("sanitize output should validate, got %v", err)
	}
}

func TestSanitizeMessages_KeepsValidToolRound(t *testing.T) {
	in := []Message{
		{Role: RoleUser, Content: "go"},
		{Role: RoleAssistant, ToolCalls: []ToolCall{{ID: "c1", Type: "function", Function: FunctionCall{Name: "f"}}}},
		{Role: RoleTool, ToolCallID: "c1", Content: "ok"},
		{Role: RoleAssistant, Content: "done"},
	}
	out := SanitizeMessages(in)
	if len(out) != 4 {
		t.Fatalf("expected 4 messages, got %d", len(out))
	}
	if err := ValidateMessages(out); err != nil {
		t.Fatalf("sanitize output should validate, got %v", err)
	}
}

func TestSanitizeMessages_DropsUnansweredToolCallEnvelope(t *testing.T) {
	// Assistant tool_calls envelope with no matching tool result and no
	// text content — drop entirely.
	in := []Message{
		{Role: RoleUser, Content: "u"},
		{Role: RoleAssistant, ToolCalls: []ToolCall{{ID: "c1", Type: "function", Function: FunctionCall{Name: "f"}}}},
		{Role: RoleUser, Content: "next"},
	}
	out := SanitizeMessages(in)
	if len(out) != 2 {
		t.Fatalf("expected envelope to be dropped, got %+v", out)
	}
	for _, m := range out {
		if len(m.ToolCalls) != 0 {
			t.Fatalf("residual tool_calls in output: %+v", m)
		}
	}
	if err := ValidateMessages(out); err != nil {
		t.Fatalf("sanitize output should validate, got %v", err)
	}
}

func TestSanitizeMessages_StripsToolCallsButKeepsTextContent(t *testing.T) {
	// Assistant has both content and an unanswered tool_call — keep
	// content, drop tool_calls.
	in := []Message{
		{Role: RoleUser, Content: "u"},
		{Role: RoleAssistant, Content: "thinking out loud", ToolCalls: []ToolCall{{ID: "c1", Type: "function", Function: FunctionCall{Name: "f"}}}},
		{Role: RoleUser, Content: "next"},
	}
	out := SanitizeMessages(in)
	if len(out) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(out))
	}
	if len(out[1].ToolCalls) != 0 {
		t.Fatalf("expected tool_calls stripped, got %+v", out[1].ToolCalls)
	}
	if out[1].Content != "thinking out loud" {
		t.Fatalf("expected content preserved, got %q", out[1].Content)
	}
	if err := ValidateMessages(out); err != nil {
		t.Fatalf("sanitize output should validate, got %v", err)
	}
}

func TestSanitizeMessages_DropsToolAfterUserInterrupt(t *testing.T) {
	// User turn between assistant tool_calls and tool result invalidates
	// the window. Sanitize should drop the late tool result AND collapse
	// the assistant envelope (no answers in window).
	in := []Message{
		{Role: RoleAssistant, ToolCalls: []ToolCall{{ID: "c1", Type: "function", Function: FunctionCall{Name: "f"}}}},
		{Role: RoleUser, Content: "interrupt"},
		{Role: RoleTool, ToolCallID: "c1", Content: "late"},
	}
	out := SanitizeMessages(in)
	if err := ValidateMessages(out); err != nil {
		t.Fatalf("sanitize output should validate, got %v\noutput=%+v", err, out)
	}
	for _, m := range out {
		if m.Role == RoleTool {
			t.Fatalf("expected no tool messages to survive, got %+v", out)
		}
	}
}

func TestSanitizeMessages_DropsDuplicateToolResult(t *testing.T) {
	in := []Message{
		{Role: RoleAssistant, ToolCalls: []ToolCall{{ID: "c1", Type: "function", Function: FunctionCall{Name: "f"}}}},
		{Role: RoleTool, ToolCallID: "c1", Content: "first"},
		{Role: RoleTool, ToolCallID: "c1", Content: "duplicate"},
	}
	out := SanitizeMessages(in)
	count := 0
	for _, m := range out {
		if m.Role == RoleTool {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("expected exactly 1 tool message after sanitize, got %d (%+v)", count, out)
	}
	if err := ValidateMessages(out); err != nil {
		t.Fatalf("sanitize output should validate, got %v", err)
	}
}

func TestSanitizeMessages_PartialAnswerKeepsAnsweredOnly(t *testing.T) {
	// Two parallel tool_calls, only one answered — keep the answered
	// pair, drop the unanswered tool_call from the envelope.
	in := []Message{
		{Role: RoleAssistant, ToolCalls: []ToolCall{
			{ID: "c1", Type: "function", Function: FunctionCall{Name: "f"}},
			{ID: "c2", Type: "function", Function: FunctionCall{Name: "g"}},
		}},
		{Role: RoleTool, ToolCallID: "c1", Content: "r1"},
		{Role: RoleUser, Content: "next"},
	}
	out := SanitizeMessages(in)
	if err := ValidateMessages(out); err != nil {
		t.Fatalf("sanitize output should validate, got %v\noutput=%+v", err, out)
	}
	// Assistant should retain c1, not c2.
	if len(out[0].ToolCalls) != 1 || out[0].ToolCalls[0].ID != "c1" {
		t.Fatalf("expected only c1 retained, got %+v", out[0].ToolCalls)
	}
}

func TestSanitizeMessages_NilAndEmpty(t *testing.T) {
	if got := SanitizeMessages(nil); got != nil {
		t.Fatalf("nil in should return nil, got %+v", got)
	}
	if got := SanitizeMessages([]Message{}); len(got) != 0 {
		t.Fatalf("empty in should return empty, got %+v", got)
	}
}
