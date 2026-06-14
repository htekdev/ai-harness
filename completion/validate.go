package completion

import (
	"github.com/htekdev/ai-harness/harness/errs"
)

// ValidateMessages enforces the OpenAI/Copilot Chat Completions ordering
// contract for tool messages before a request is dispatched. The provider
// rejects (with HTTP 400 "messages with role 'tool' must be a response to a
// preceeding message with 'tool_calls'") any conversation where:
//
//  1. A role:tool message appears without an immediately-preceding assistant
//     message that carried a tool_calls block; OR
//  2. A role:tool message references a tool_call_id that does not appear in
//     the most recent assistant tool_calls envelope.
//
// In long-running sessions (Telegram, IDE) compaction or turn editing can
// orphan tool results — surfacing those as a typed pre-flight error keeps
// the failure local and actionable instead of leaking provider semantics.
//
// The check is intentionally conservative: it rejects, never silently
// repairs. Repair is a higher-level concern (history compaction).
func ValidateMessages(msgs []Message) error {
	// pendingCallIDs is the set of tool_call_ids advertised by the most
	// recent assistant message's tool_calls block. It is reset to nil on
	// any non-(assistant|tool) boundary or once an assistant message
	// without tool_calls is seen.
	var pendingCallIDs map[string]struct{}

	for i, m := range msgs {
		switch m.Role {
		case RoleAssistant:
			if len(m.ToolCalls) == 0 {
				pendingCallIDs = nil
				continue
			}
			pendingCallIDs = make(map[string]struct{}, len(m.ToolCalls))
			for _, tc := range m.ToolCalls {
				if tc.ID == "" {
					return errs.Newf(errs.KindInvalidConversation,
						"completion.validate",
						"assistant message at index %d has a tool_call with empty id", i)
				}
				pendingCallIDs[tc.ID] = struct{}{}
			}

		case RoleTool:
			if m.ToolCallID == "" {
				return errs.Newf(errs.KindInvalidConversation,
					"completion.validate",
					"tool message at index %d has empty tool_call_id", i)
			}
			if pendingCallIDs == nil {
				return errs.Newf(errs.KindInvalidConversation,
					"completion.validate",
					"tool message at index %d (tool_call_id=%s) is not preceded by an assistant tool_calls message",
					i, m.ToolCallID)
			}
			if _, ok := pendingCallIDs[m.ToolCallID]; !ok {
				return errs.Newf(errs.KindInvalidConversation,
					"completion.validate",
					"tool message at index %d references tool_call_id %q not present in preceding assistant tool_calls",
					i, m.ToolCallID)
			}
			// Consume the matching id so a duplicate tool message for the
			// same call is also flagged.
			delete(pendingCallIDs, m.ToolCallID)

		default:
			// system / user (or future roles) reset the tool-call window —
			// the provider only honors tool messages that immediately
			// follow the assistant tool_calls envelope.
			pendingCallIDs = nil
		}
	}

	return nil
}
