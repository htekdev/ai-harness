package context

import (
	"testing"

	"github.com/htekdev/ai-harness/completion"
)

func TestNewManagerDefaults(t *testing.T) {
	m := NewManager(Config{})
	if m.maxMessages != 50 {
		t.Fatalf("expected default maxMessages 50, got %d", m.maxMessages)
	}
	if m.maxTokens != 128000 {
		t.Fatalf("expected default maxTokens 128000, got %d", m.maxTokens)
	}
}

func TestAddMessageAndHistory(t *testing.T) {
	m := NewManager(Config{SystemPrompt: "You are helpful."})

	m.AddMessage(completion.Message{Role: completion.RoleUser, Content: "Hello"})
	m.AddMessage(completion.Message{Role: completion.RoleAssistant, Content: "Hi there!"})

	if m.Len() != 2 {
		t.Fatalf("expected 2 messages, got %d", m.Len())
	}

	history := m.History()
	if len(history) != 2 {
		t.Fatalf("expected 2 history messages, got %d", len(history))
	}
	if history[0].Content != "Hello" {
		t.Fatalf("unexpected first message: %s", history[0].Content)
	}
}

func TestMessagesIncludesSystemPrompt(t *testing.T) {
	m := NewManager(Config{SystemPrompt: "Be concise."})
	m.AddMessage(completion.Message{Role: completion.RoleUser, Content: "Hi"})

	msgs := m.Messages()
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages (system + user), got %d", len(msgs))
	}
	if msgs[0].Role != completion.RoleSystem {
		t.Fatal("first message should be system")
	}
	if msgs[0].Content != "Be concise." {
		t.Fatalf("unexpected system content: %s", msgs[0].Content)
	}
}

func TestMessagesWithoutSystemPrompt(t *testing.T) {
	m := NewManager(Config{})
	m.AddMessage(completion.Message{Role: completion.RoleUser, Content: "Hi"})

	msgs := m.Messages()
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message (no system prompt), got %d", len(msgs))
	}
}

func TestTruncateOnMaxMessages(t *testing.T) {
	m := NewManager(Config{MaxMessages: 3})

	for i := 0; i < 5; i++ {
		m.AddMessage(completion.Message{Role: completion.RoleUser, Content: "msg"})
	}

	if m.Len() != 3 {
		t.Fatalf("expected 3 messages after truncation, got %d", m.Len())
	}
}

func TestClear(t *testing.T) {
	m := NewManager(Config{SystemPrompt: "keep me"})
	m.AddMessage(completion.Message{Role: completion.RoleUser, Content: "hi"})
	m.Clear()

	if m.Len() != 0 {
		t.Fatalf("expected 0 messages after clear, got %d", m.Len())
	}

	msgs := m.Messages()
	if len(msgs) != 1 || msgs[0].Content != "keep me" {
		t.Fatal("system prompt should survive Clear()")
	}
}

func TestSetSystemPrompt(t *testing.T) {
	m := NewManager(Config{SystemPrompt: "original"})
	m.SetSystemPrompt("updated")

	msgs := m.Messages()
	if msgs[0].Content != "updated" {
		t.Fatalf("expected updated system prompt, got %s", msgs[0].Content)
	}
}

func TestFork(t *testing.T) {
	m := NewManager(Config{SystemPrompt: "test"})
	m.AddMessage(completion.Message{Role: completion.RoleUser, Content: "hello"})

	fork := m.Fork()
	fork.AddMessage(completion.Message{Role: completion.RoleAssistant, Content: "reply"})

	if m.Len() != 1 {
		t.Fatal("original should not be affected by fork")
	}
	if fork.Len() != 2 {
		t.Fatal("fork should have both messages")
	}
}

func TestEstimateTokens(t *testing.T) {
	m := NewManager(Config{SystemPrompt: "short"})
	m.AddMessage(completion.Message{Role: completion.RoleUser, Content: "hello world"})

	tokens := m.EstimateTokens()
	if tokens <= 0 {
		t.Fatal("expected positive token estimate")
	}
}

func TestAddMessages(t *testing.T) {
	m := NewManager(Config{})
	m.AddMessages([]completion.Message{
		{Role: completion.RoleUser, Content: "one"},
		{Role: completion.RoleAssistant, Content: "two"},
	})
	if m.Len() != 2 {
		t.Fatalf("expected 2 messages, got %d", m.Len())
	}
}

func TestTokenTruncation(t *testing.T) {
	// MaxTokens very low — should force truncation
	m := NewManager(Config{MaxTokens: 10, MaxMessages: 100})
	// Each message ~5 tokens (4 overhead + content/4)
	for i := 0; i < 20; i++ {
		m.AddMessage(completion.Message{Role: completion.RoleUser, Content: "this is a longer message to force truncation"})
	}
	if m.Len() >= 20 {
		t.Fatal("expected truncation to reduce message count")
	}
}
