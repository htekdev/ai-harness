// Package config handles YAML-based configuration for the AI harness.
package config

import (
	"encoding/json"
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// Config is the top-level harness configuration.
type Config struct {
	Model   ModelConfig   `yaml:"model" json:"model"`
	Context ContextConfig `yaml:"context" json:"context"`
	Tools   []ToolConfig  `yaml:"tools" json:"tools"`
	Hooks   []HookConfig  `yaml:"hooks" json:"hooks"`
}

// ModelConfig defines the LLM provider and parameters.
type ModelConfig struct {
	Provider    string  `yaml:"provider" json:"provider"`
	Name        string  `yaml:"name" json:"name"`
	MaxTokens   int     `yaml:"max_tokens" json:"max_tokens"`
	Temperature float64 `yaml:"temperature" json:"temperature"`
	BaseURL     string  `yaml:"base_url" json:"base_url"`
	APIKeyEnv   string  `yaml:"api_key_env" json:"api_key_env"`
}

// ContextConfig defines context management parameters.
type ContextConfig struct {
	MaxHistory   int    `yaml:"max_history" json:"max_history"`
	MaxTokens    int    `yaml:"max_tokens" json:"max_tokens"`
	SystemPrompt string `yaml:"system_prompt" json:"system_prompt"`
}

// ToolConfig defines a tool in configuration.
type ToolConfig struct {
	Name        string                 `yaml:"name" json:"name"`
	Description string                 `yaml:"description" json:"description"`
	Parameters  map[string]ParamConfig `yaml:"parameters" json:"parameters"`
}

// ParamConfig defines a tool parameter in configuration.
type ParamConfig struct {
	Type        string `yaml:"type" json:"type"`
	Description string `yaml:"description" json:"description"`
	Required    bool   `yaml:"required" json:"required"`
}

// HookConfig defines a hook registration in configuration.
type HookConfig struct {
	Event   string `yaml:"event" json:"event"`
	Handler string `yaml:"handler" json:"handler"`
}

// Load reads and parses a YAML configuration file.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config file: %w", err)
	}

	return Parse(data)
}

// Parse parses YAML configuration data.
func Parse(data []byte) (*Config, error) {
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}

	applyDefaults(&cfg)
	return &cfg, nil
}

// ParseJSON parses JSON configuration data.
func ParseJSON(data []byte) (*Config, error) {
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}

	applyDefaults(&cfg)
	return &cfg, nil
}

// applyDefaults fills in zero-value fields with sensible defaults.
func applyDefaults(cfg *Config) {
	if cfg.Model.Name == "" {
		cfg.Model.Name = "gpt-4o"
	}
	if cfg.Model.MaxTokens == 0 {
		cfg.Model.MaxTokens = 4096
	}
	if cfg.Model.Temperature == 0 {
		cfg.Model.Temperature = 0.7
	}
	if cfg.Model.Provider == "" {
		cfg.Model.Provider = "openai"
	}
	if cfg.Model.APIKeyEnv == "" {
		cfg.Model.APIKeyEnv = "GITHUB_TOKEN"
	}
	if cfg.Context.MaxHistory == 0 {
		cfg.Context.MaxHistory = 50
	}
	if cfg.Context.MaxTokens == 0 {
		cfg.Context.MaxTokens = 128000
	}
}

// ResolveAPIKey reads the API key from the environment variable specified in config.
func (c *Config) ResolveAPIKey() string {
	return os.Getenv(c.Model.APIKeyEnv)
}

// BaseURL returns the appropriate base URL for the configured provider.
func (c *Config) BaseURL() string {
	if c.Model.BaseURL != "" {
		return c.Model.BaseURL
	}

	switch c.Model.Provider {
	case "copilot":
		return "https://api.githubcopilot.com"
	case "openai":
		return "https://api.openai.com/v1"
	default:
		return "https://api.openai.com/v1"
	}
}
