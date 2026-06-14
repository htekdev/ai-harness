// Package context manages conversation history and context windows for the AI harness.
// It handles message accumulation, token-aware truncation, and system prompt management.
package context

import (
	"github.com/htekdev/ai-harness/completion"
)

// Manager handles conversation context, including message history and token management.
type Manager struct {
	systemPrompt string
	messages     []completion.Message
	maxMessages  int
	maxTokens    int
}

// Config holds context manager configuration.
type Config struct {
	SystemPrompt string
	MaxMessages  int
	MaxTokens    int
}

// NewManager creates a new context manager.
func NewManager(cfg Config) *Manager {
	if cfg.MaxMessages == 0 {
		cfg.MaxMessages = 50
	}
	if cfg.MaxTokens == 0 {
		cfg.MaxTokens = 128000
	}

	return &Manager{
		systemPrompt: cfg.SystemPrompt,
		messages:     make([]completion.Message, 0),
		maxMessages:  cfg.MaxMessages,
		maxTokens:    cfg.MaxTokens,
	}
}

// SetSystemPrompt updates the system prompt.
func (m *Manager) SetSystemPrompt(prompt string) {
	m.systemPrompt = prompt
}

// AddMessage appends a message to the conversation history.
func (m *Manager) AddMessage(msg completion.Message) {
	m.messages = append(m.messages, msg)
	m.truncateIfNeeded()
}

// AddMessages appends multiple messages to the conversation history.
func (m *Manager) AddMessages(msgs []completion.Message) {
	m.messages = append(m.messages, msgs...)
	m.truncateIfNeeded()
}

// Messages returns the full message list ready for the API, including the system prompt.
// Always sanitizes the history first so leading orphan tool messages (with no
// preceding assistant `tool_calls`) never reach the provider — that would
// trip OpenAI's "messages with role 'tool' must be a response to a preceding
// message with 'tool_calls'" 400 error.
func (m *Manager) Messages() []completion.Message {
	sanitized := sanitizeHistory(m.messages)
	msgs := make([]completion.Message, 0, len(sanitized)+1)

	if m.systemPrompt != "" {
		msgs = append(msgs, completion.Message{
			Role:    completion.RoleSystem,
			Content: m.systemPrompt,
		})
	}

	msgs = append(msgs, sanitized...)
	return msgs
}

// History returns only the conversation messages (no system prompt).
func (m *Manager) History() []completion.Message {
	out := make([]completion.Message, len(m.messages))
	copy(out, m.messages)
	return out
}

// Clear resets the conversation history but preserves the system prompt.
func (m *Manager) Clear() {
	m.messages = m.messages[:0]
}

// Len returns the number of messages in the history (excluding system prompt).
func (m *Manager) Len() int {
	return len(m.messages)
}

// EstimateTokens provides a rough token estimate for the current context.
// Uses the approximation of ~4 characters per token.
func (m *Manager) EstimateTokens() int {
	total := len(m.systemPrompt) / 4
	for _, msg := range m.messages {
		total += len(msg.Content) / 4
		for _, tc := range msg.ToolCalls {
			total += len(tc.Function.Arguments) / 4
			total += len(tc.Function.Name) / 4
		}
		// Overhead per message (~4 tokens for role, formatting)
		total += 4
	}
	return total
}

// truncateIfNeeded removes oldest messages (keeping system prompt intact)
// when the history exceeds maxMessages or estimated token limit.
//
// Truncation is **pair-aware**: dropping a `role:"assistant"` message that
// carried `tool_calls` also drops the immediately-following `role:"tool"`
// response messages. Without this, the OpenAI API rejects the next
// request with HTTP 400 ("messages with role 'tool' must be a response
// to a preceding message with 'tool_calls'") because the orphan tool
// messages have no anchor.
func (m *Manager) truncateIfNeeded() {
	// Enforce message count limit
	if len(m.messages) > m.maxMessages {
		excess := len(m.messages) - m.maxMessages
		m.messages = dropFromFrontSafely(m.messages, excess)
	}

	// Enforce token limit (iterative trim from the front, one logical
	// group at a time so we never split a tool_calls / tool pair).
	for m.EstimateTokens() > m.maxTokens && len(m.messages) > 1 {
		m.messages = dropFromFrontSafely(m.messages, 1)
	}
}

// dropFromFrontSafely removes n messages from the front of msgs, then
// keeps removing while the next message is a `role:"tool"` response —
// because such a message would now be an orphan with no preceding
// `tool_calls` assistant message. This preserves the invariant that
// every tool response in the returned slice has a valid anchor.
func dropFromFrontSafely(msgs []completion.Message, n int) []completion.Message {
	if n <= 0 {
		return msgs
	}
	if n >= len(msgs) {
		return msgs[:0]
	}
	msgs = msgs[n:]
	for len(msgs) > 0 && msgs[0].Role == completion.RoleTool {
		msgs = msgs[1:]
	}
	return msgs
}

// sanitizeHistory returns msgs with leading orphan `role:"tool"` messages
// stripped and, additionally, with any `role:"assistant"` message whose
// `tool_calls` were not all followed by matching `role:"tool"` responses
// stripped along with its partial responses. This is a defensive guard
// invoked on every Messages() call — even if truncateIfNeeded has a bug
// in some future code path, the API never sees a malformed sequence.
//
// The two failure modes this protects against:
//
//  1. Leading orphan tool: history starts with `role:"tool"` because the
//     preceding assistant message with tool_calls was dropped.
//
//  2. Mid-history orphan assistant: an assistant message declares
//     tool_calls=[A,B] but only A's response is present (or none) —
//     drop the assistant and any partial responses so the rest of the
//     history stays valid.
//
// The function never allocates if msgs is already valid (the common
// path), so the per-call cost is minimal.
func sanitizeHistory(msgs []completion.Message) []completion.Message {
	if !needsSanitization(msgs) {
		return msgs
	}

	out := make([]completion.Message, 0, len(msgs))
	for i := 0; i < len(msgs); i++ {
		msg := msgs[i]

		// Skip orphan tool responses (no preceding assistant tool_calls
		// that referenced this CallID survives in `out`).
		if msg.Role == completion.RoleTool {
			if !hasMatchingToolCallInPrefix(out, msg.ToolCallID) {
				continue
			}
			out = append(out, msg)
			continue
		}

		// For assistant messages with tool_calls, require that every
		// declared call_id has a corresponding `role:"tool"` response
		// in the contiguous run that immediately follows. If any are
		// missing, drop the assistant message AND the partial run.
		if msg.Role == completion.RoleAssistant && len(msg.ToolCalls) > 0 {
			runEnd := i + 1
			seen := make(map[string]bool, len(msg.ToolCalls))
			for runEnd < len(msgs) && msgs[runEnd].Role == completion.RoleTool {
				seen[msgs[runEnd].ToolCallID] = true
				runEnd++
			}
			complete := true
			for _, tc := range msg.ToolCalls {
				if !seen[tc.ID] {
					complete = false
					break
				}
			}
			if !complete {
				// Skip the assistant message and the partial tool run.
				i = runEnd - 1
				continue
			}
		}

		out = append(out, msg)
	}
	return out
}

// needsSanitization is a fast pre-check that returns true iff
// sanitizeHistory would make any change. Avoids the allocation in the
// common case where the history is already well-formed.
func needsSanitization(msgs []completion.Message) bool {
	for i, msg := range msgs {
		if msg.Role == completion.RoleTool {
			if !hasMatchingToolCallInPrefix(msgs[:i], msg.ToolCallID) {
				return true
			}
		}
		if msg.Role == completion.RoleAssistant && len(msg.ToolCalls) > 0 {
			runEnd := i + 1
			seen := make(map[string]bool, len(msg.ToolCalls))
			for runEnd < len(msgs) && msgs[runEnd].Role == completion.RoleTool {
				seen[msgs[runEnd].ToolCallID] = true
				runEnd++
			}
			for _, tc := range msg.ToolCalls {
				if !seen[tc.ID] {
					return true
				}
			}
		}
	}
	return false
}

// hasMatchingToolCallInPrefix reports whether any earlier assistant
// message in prefix declared a tool call with the given CallID. Used by
// sanitizeHistory to decide if a `role:"tool"` message has a valid
// anchor.
func hasMatchingToolCallInPrefix(prefix []completion.Message, callID string) bool {
	if callID == "" {
		return false
	}
	for _, m := range prefix {
		if m.Role != completion.RoleAssistant {
			continue
		}
		for _, tc := range m.ToolCalls {
			if tc.ID == callID {
				return true
			}
		}
	}
	return false
}

// Fork creates a copy of the context manager for branching conversations.
func (m *Manager) Fork() *Manager {
	msgs := make([]completion.Message, len(m.messages))
	copy(msgs, m.messages)

	return &Manager{
		systemPrompt: m.systemPrompt,
		messages:     msgs,
		maxMessages:  m.maxMessages,
		maxTokens:    m.maxTokens,
	}
}
