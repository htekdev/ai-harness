package config

import (
	"testing"
)

func TestParseRetryBlockOnModel(t *testing.T) {
	yaml := `
model:
  name: gpt-4o
  provider: openai
  api_key_env: TEST_KEY
  retry:
    max_retries: 5
    initial_backoff_ms: 250
    max_backoff_ms: 10000
    multiplier: 1.5
context:
  max_history: 10
  max_tokens: 8000
`
	cfg, err := Parse([]byte(yaml))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if cfg.Model.Retry == nil {
		t.Fatal("expected model.retry to be parsed")
	}
	if cfg.Model.Retry.MaxRetries == nil || *cfg.Model.Retry.MaxRetries != 5 {
		t.Errorf("max_retries = %v, want 5", cfg.Model.Retry.MaxRetries)
	}
	if cfg.Model.Retry.InitialBackoffMS != 250 {
		t.Errorf("initial_backoff_ms = %d, want 250", cfg.Model.Retry.InitialBackoffMS)
	}
	if cfg.Model.Retry.MaxBackoffMS != 10000 {
		t.Errorf("max_backoff_ms = %d, want 10000", cfg.Model.Retry.MaxBackoffMS)
	}
	if cfg.Model.Retry.Multiplier != 1.5 {
		t.Errorf("multiplier = %v, want 1.5", cfg.Model.Retry.Multiplier)
	}
}

func TestRetryBlockOptional(t *testing.T) {
	yaml := `
model:
  name: gpt-4o
  provider: openai
  api_key_env: TEST_KEY
context:
  max_history: 10
  max_tokens: 8000
`
	cfg, err := Parse([]byte(yaml))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if cfg.Model.Retry != nil {
		t.Errorf("expected nil retry, got %+v", cfg.Model.Retry)
	}
}

func TestRetryValidationRejectsNegatives(t *testing.T) {
	neg := -1
	cases := []struct {
		name  string
		retry RetryConfig
	}{
		{"max_retries", RetryConfig{MaxRetries: &neg}},
		{"initial_backoff_ms", RetryConfig{InitialBackoffMS: -1}},
		{"max_backoff_ms", RetryConfig{MaxBackoffMS: -1}},
		{"multiplier", RetryConfig{Multiplier: -0.5}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cfg := &Config{
				Model: ModelConfig{
					Name:        "gpt-4o",
					Provider:    "openai",
					APIKeyEnv:   "TEST_KEY",
					MaxTokens:   4096,
					Temperature: 0.7,
					Retry:       &c.retry,
				},
				Context: ContextConfig{MaxHistory: 10, MaxTokens: 8000},
			}
			if err := cfg.Validate(); err == nil {
				t.Errorf("expected validation error for negative %s", c.name)
			}
		})
	}
}

func TestRetryZeroMaxRetriesIsValid(t *testing.T) {
	zero := 0
	cfg := &Config{
		Model: ModelConfig{
			Name:        "gpt-4o",
			Provider:    "openai",
			APIKeyEnv:   "TEST_KEY",
			MaxTokens:   4096,
			Temperature: 0.7,
			Retry:       &RetryConfig{MaxRetries: &zero},
		},
		Context: ContextConfig{MaxHistory: 10, MaxTokens: 8000},
	}
	if err := cfg.Validate(); err != nil {
		t.Errorf("expected zero MaxRetries to be valid, got %v", err)
	}
}
