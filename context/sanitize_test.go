package context

import (
	"testing"

	"github.com/htekdev/ai-harness/completion"
)

// helper: tool_calls assistant message
func asstWithCalls(content string, ids ...string) completion.Message {
	tcs := make([]completion.ToolCall, 0, len(ids))
	for _, id := range ids {
		tcs = append(tcs, completion.ToolCall{
			ID:       id,
			Type:     "function",
			Function: completion.FunctionCall{Name: "fn", Arguments: "{}"},
		})
	}
	return completion.Message{
		Role:      completion.RoleAssistant,
		Content:   content,
		ToolCalls: tcs,
	}
}

func toolResp(callID, content string) completion.Message {
	return completion.Message{
		Role:       completion.RoleTool,
		ToolCallID: callID,
		Content:    content,
	}
}

// TestSanitizeHistory_StripsLeadingOrphanTool reproduces the bug Hector hit:
// truncation dropped an assistant(tool_calls) message but kept the
// following tool responses, leaving them orphaned at the front of history.
// Without sanitization the API would 400 with the "messages with role
// 'tool' must be a response to a preceding message with 'tool_calls'" error.
func TestSanitizeHistory_StripsLeadingOrphanTool(t *testing.T) {
	t.Parallel()
	// Orphan tool at front — the assistant that produced it is gone.
	history := []completion.Message{
		toolResp("call_A", "tool result for A"),
		{Role: completion.RoleAssistant, Content: "ok done"},
		{Role: completion.RoleUser, Content: "next?"},
	}
	got := sanitizeHistory(history)
	if len(got) != 2 {
		t.Fatalf("expected 2 messages after sanitization, got %d: %+v", len(got), got)
	}
	if got[0].Role != completion.RoleAssistant {
		t.Fatalf("expected first message to be assistant after sanitization, got %s", got[0].Role)
	}
}

// TestSanitizeHistory_KeepsValidToolPair confirms the happy path is not
// mutated — a well-formed history must pass through untouched.
func TestSanitizeHistory_KeepsValidToolPair(t *testing.T) {
	t.Parallel()
	history := []completion.Message{
		{Role: completion.RoleUser, Content: "do thing"},
		asstWithCalls("calling", "call_A", "call_B"),
		toolResp("call_A", "A-result"),
		toolResp("call_B", "B-result"),
		{Role: completion.RoleAssistant, Content: "all done"},
	}
	got := sanitizeHistory(history)
	if len(got) != len(history) {
		t.Fatalf("expected unchanged history, got len=%d (want %d)", len(got), len(history))
	}
	for i := range history {
		if got[i].Role != history[i].Role {
			t.Errorf("position %d role mismatch: got %s want %s", i, got[i].Role, history[i].Role)
		}
	}
}

// TestSanitizeHistory_DropsPartialAssistantToolGroup covers the case
// where an assistant declared two tool_calls but only one response was
// recorded. Drop the whole bundle so the API sees a consistent prefix.
func TestSanitizeHistory_DropsPartialAssistantToolGroup(t *testing.T) {
	t.Parallel()
	history := []completion.Message{
		{Role: completion.RoleUser, Content: "u1"},
		asstWithCalls("calling", "call_A", "call_B"),
		toolResp("call_A", "A-result"),
		// call_B is missing — partial group.
		{Role: completion.RoleAssistant, Content: "recovered"},
	}
	got := sanitizeHistory(history)
	wantContents := []string{"u1", "recovered"}
	if len(got) != len(wantContents) {
		t.Fatalf("expected %d messages, got %d: %+v", len(wantContents), len(got), got)
	}
	for i, want := range wantContents {
		if got[i].Content != want {
			t.Errorf("position %d content: got %q want %q", i, got[i].Content, want)
		}
	}
}

// TestTruncateIfNeeded_PairAware drives the original bug end-to-end via
// the public API: build a history that overflows maxMessages, then call
// Messages() and verify the returned slice never starts with a tool
// response.
func TestTruncateIfNeeded_PairAware(t *testing.T) {
	t.Parallel()
	m := NewManager(Config{MaxMessages: 4, MaxTokens: 1_000_000})

	// Simulate the live-bug pattern: many tool-call rounds.
	m.AddMessage(completion.Message{Role: completion.RoleUser, Content: "u1"})
	m.AddMessage(asstWithCalls("call1", "id1"))
	m.AddMessage(toolResp("id1", "r1"))
	m.AddMessage(asstWithCalls("call2", "id2"))
	m.AddMessage(toolResp("id2", "r2"))
	m.AddMessage(asstWithCalls("call3", "id3"))
	m.AddMessage(toolResp("id3", "r3"))
	m.AddMessage(completion.Message{Role: completion.RoleUser, Content: "u2"})

	out := m.Messages()
	// Skip system prompt — there's no system prompt configured here.
	first := out[0]
	if first.Role == completion.RoleTool {
		t.Fatalf("Messages() must never start with a tool role; got %+v (full: %+v)", first, out)
	}
	// Every tool message must have a preceding assistant tool_calls
	// in the returned slice with a matching CallID.
	for i, msg := range out {
		if msg.Role != completion.RoleTool {
			continue
		}
		found := false
		for _, prior := range out[:i] {
			if prior.Role != completion.RoleAssistant {
				continue
			}
			for _, tc := range prior.ToolCalls {
				if tc.ID == msg.ToolCallID {
					found = true
					break
				}
			}
			if found {
				break
			}
		}
		if !found {
			t.Fatalf("orphan tool message at position %d (call_id=%s) survived truncation", i, msg.ToolCallID)
		}
	}
}

// TestTruncateIfNeeded_TokenLoopIsPairAware exercises the per-message
// trim loop (token-based) — it used to slice [1:] one message at a time
// without considering pairing.
func TestTruncateIfNeeded_TokenLoopIsPairAware(t *testing.T) {
	t.Parallel()
	// Very low token budget to force the inner loop to actually run.
	m := NewManager(Config{MaxMessages: 100, MaxTokens: 30})
	for i := 0; i < 5; i++ {
		m.AddMessage(asstWithCalls("call", "id"+string(rune('A'+i))))
		m.AddMessage(toolResp("id"+string(rune('A'+i)), "result that is long enough to push tokens over budget"))
	}
	out := m.Messages()
	if len(out) > 0 && out[0].Role == completion.RoleTool {
		t.Fatalf("token-based truncation left an orphan tool message at the head: %+v", out)
	}
}

// TestNeedsSanitization_FastPath asserts that well-formed histories
// skip the allocating sanitize code path.
func TestNeedsSanitization_FastPath(t *testing.T) {
	t.Parallel()
	if needsSanitization(nil) {
		t.Errorf("nil should not need sanitization")
	}
	if needsSanitization([]completion.Message{
		{Role: completion.RoleUser, Content: "hi"},
		{Role: completion.RoleAssistant, Content: "hello"},
	}) {
		t.Errorf("plain text-only history should not need sanitization")
	}
	good := []completion.Message{
		asstWithCalls("calling", "X"),
		toolResp("X", "done"),
	}
	if needsSanitization(good) {
		t.Errorf("well-formed tool pair should not need sanitization")
	}
	bad := []completion.Message{
		toolResp("X", "orphan"),
	}
	if !needsSanitization(bad) {
		t.Errorf("orphan tool should need sanitization")
	}
}
