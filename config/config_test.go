package config

import (
	"testing"
)

func TestParseYAML(t *testing.T) {
	yaml := []byte(`
model:
  provider: copilot
  name: gpt-4o
  max_tokens: 8192
  temperature: 0.5
  api_key_env: GITHUB_TOKEN

context:
  max_history: 100
  max_tokens: 200000
  system_prompt: "You are a coding assistant."

tools:
  - name: read_file
    description: Read a file
    parameters:
      path:
        type: string
        description: File path
        required: true

hooks:
  - event: tool.pre
    handler: audit_log
`)

	cfg, err := Parse(yaml)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.Model.Provider != "copilot" {
		t.Fatalf("expected provider 'copilot', got %q", cfg.Model.Provider)
	}
	if cfg.Model.Name != "gpt-4o" {
		t.Fatalf("expected model 'gpt-4o', got %q", cfg.Model.Name)
	}
	if cfg.Model.MaxTokens != 8192 {
		t.Fatalf("expected max_tokens 8192, got %d", cfg.Model.MaxTokens)
	}
	if cfg.Context.MaxHistory != 100 {
		t.Fatalf("expected max_history 100, got %d", cfg.Context.MaxHistory)
	}
	if cfg.Context.SystemPrompt != "You are a coding assistant." {
		t.Fatalf("unexpected system prompt: %s", cfg.Context.SystemPrompt)
	}
	if len(cfg.Tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(cfg.Tools))
	}
	if cfg.Tools[0].Name != "read_file" {
		t.Fatalf("expected tool 'read_file', got %q", cfg.Tools[0].Name)
	}
	if len(cfg.Hooks) != 1 {
		t.Fatalf("expected 1 hook, got %d", len(cfg.Hooks))
	}
}

func TestParseDefaults(t *testing.T) {
	cfg, err := Parse([]byte(`{}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.Model.Name != "gpt-4o" {
		t.Fatalf("expected default model name 'gpt-4o', got %q", cfg.Model.Name)
	}
	if cfg.Model.MaxTokens != 4096 {
		t.Fatalf("expected default max_tokens 4096, got %d", cfg.Model.MaxTokens)
	}
	if cfg.Model.Temperature != 0.7 {
		t.Fatalf("expected default temperature 0.7, got %f", cfg.Model.Temperature)
	}
	if cfg.Model.Provider != "openai" {
		t.Fatalf("expected default provider 'openai', got %q", cfg.Model.Provider)
	}
	if cfg.Context.MaxHistory != 50 {
		t.Fatalf("expected default max_history 50, got %d", cfg.Context.MaxHistory)
	}
}

func TestParseJSON(t *testing.T) {
	data := []byte(`{"model":{"provider":"openai","name":"gpt-4o-mini","max_tokens":2048}}`)
	cfg, err := ParseJSON(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Model.Name != "gpt-4o-mini" {
		t.Fatalf("expected 'gpt-4o-mini', got %q", cfg.Model.Name)
	}
}

func TestBaseURL(t *testing.T) {
	tests := []struct {
		provider string
		baseURL  string
		expected string
	}{
		{"copilot", "", "https://api.githubcopilot.com"},
		{"openai", "", "https://api.openai.com/v1"},
		{"custom", "https://my-api.com", "https://my-api.com"},
		{"", "", "https://api.openai.com/v1"},
	}

	for _, tt := range tests {
		cfg := &Config{Model: ModelConfig{Provider: tt.provider, BaseURL: tt.baseURL}}
		if got := cfg.BaseURL(); got != tt.expected {
			t.Errorf("provider=%q baseURL=%q: expected %q, got %q", tt.provider, tt.baseURL, tt.expected, got)
		}
	}
}

func TestParseInvalidYAML(t *testing.T) {
	_, err := Parse([]byte(`{{{invalid`))
	if err == nil {
		t.Fatal("expected error for invalid YAML")
	}
}

func TestParseInvalidJSON(t *testing.T) {
	_, err := ParseJSON([]byte(`not json`))
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}
