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
func (m *Manager) Messages() []completion.Message {
	msgs := make([]completion.Message, 0, len(m.messages)+1)

	if m.systemPrompt != "" {
		msgs = append(msgs, completion.Message{
			Role:    completion.RoleSystem,
			Content: m.systemPrompt,
		})
	}

	msgs = append(msgs, m.messages...)
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
func (m *Manager) truncateIfNeeded() {
	// Enforce message count limit
	if len(m.messages) > m.maxMessages {
		excess := len(m.messages) - m.maxMessages
		m.messages = m.messages[excess:]
	}

	// Enforce token limit (iterative trim from the front)
	for m.EstimateTokens() > m.maxTokens && len(m.messages) > 1 {
		m.messages = m.messages[1:]
	}
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
