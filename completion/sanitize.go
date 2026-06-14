package completion

// SanitizeMessages returns a copy of msgs with any orphan tool messages
// and dangling assistant tool_calls envelopes removed, so the result
// satisfies the OpenAI Chat Completions ordering contract enforced by
// ValidateMessages.
//
// Sanitization rules (matched to the validator):
//
//  1. A role:tool message is dropped if it is not preceded by an
//     assistant message whose tool_calls block contains its tool_call_id
//     (orphan after history compaction or turn edit).
//  2. A role:tool message with empty tool_call_id is dropped.
//  3. An assistant message that carries a tool_calls envelope but is NOT
//     followed by tool messages covering EVERY advertised id is rewritten
//     to drop the unanswered tool_calls (keeping its text content). This
//     prevents the next user turn from looking like a response to a
//     half-completed tool round.
//  4. A user/system turn between an assistant tool_calls envelope and its
//     tool results invalidates the window — subsequent tool messages
//     bound to that envelope are dropped, and the assistant envelope is
//     rewritten per rule 3.
//
// Sanitize is intentionally lossy on the side of provider compatibility:
// dropping a stale tool result is preferred over leaking an HTTP 400 to
// the user. The harness still logs typed errors via ValidateMessages if
// any sanitized output somehow remains malformed (defense in depth).
func SanitizeMessages(msgs []Message) []Message {
	if len(msgs) == 0 {
		return msgs
	}

	// Two-pass: identify which assistant indexes have at least one
	// satisfied tool_call (so we know whether to keep their tool_calls
	// envelope as-is or rewrite it). We track per-assistant the set of
	// ids that DO get answered before the window closes.
	type window struct {
		assistantIdx int
		open         bool
		ids          map[string]bool // id -> answered?
	}
	var win window
	answered := make(map[int]map[string]bool, 4) // assistantIdx -> ids answered

	closeWindow := func() {
		if win.open {
			answered[win.assistantIdx] = win.ids
		}
		win = window{}
	}

	for i, m := range msgs {
		switch m.Role {
		case RoleAssistant:
			closeWindow()
			if len(m.ToolCalls) > 0 {
				ids := make(map[string]bool, len(m.ToolCalls))
				for _, tc := range m.ToolCalls {
					if tc.ID != "" {
						ids[tc.ID] = false
					}
				}
				win = window{assistantIdx: i, open: true, ids: ids}
			}
		case RoleTool:
			if win.open {
				if _, ok := win.ids[m.ToolCallID]; ok && m.ToolCallID != "" {
					win.ids[m.ToolCallID] = true
					continue
				}
			}
			// Orphan or unknown id — handled in the rewrite pass.
		default:
			closeWindow()
		}
	}
	closeWindow()

	// Rewrite pass.
	out := make([]Message, 0, len(msgs))
	win = window{}
	for i, m := range msgs {
		switch m.Role {
		case RoleAssistant:
			win = window{}
			if len(m.ToolCalls) > 0 {
				ids := answered[i]
				kept := make([]ToolCall, 0, len(m.ToolCalls))
				for _, tc := range m.ToolCalls {
					if tc.ID != "" && ids[tc.ID] {
						kept = append(kept, tc)
					}
				}
				if len(kept) == 0 {
					// All tool_calls unanswered — drop the envelope but
					// preserve text content if any.
					if m.Content == "" {
						// Pure orphan envelope; skip entirely.
						continue
					}
					m.ToolCalls = nil
				} else {
					m.ToolCalls = kept
					win = window{assistantIdx: i, open: true, ids: make(map[string]bool, len(kept))}
					for _, tc := range kept {
						win.ids[tc.ID] = true
					}
				}
			}
			out = append(out, m)
		case RoleTool:
			if win.open && m.ToolCallID != "" && win.ids[m.ToolCallID] {
				out = append(out, m)
				delete(win.ids, m.ToolCallID) // consume; duplicates dropped
				continue
			}
			// Orphan / duplicate / window-broken — drop silently.
		default:
			win = window{}
			out = append(out, m)
		}
	}

	return out
}
