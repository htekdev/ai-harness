// Package config handles YAML-based configuration for the AI harness.
package config

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/htekdev/ai-harness/hooks"
	"gopkg.in/yaml.v3"
)

// Config is the top-level harness configuration.
type Config struct {
	Model      ModelConfig        `yaml:"model" json:"model"`
	Models     []ModelConfig      `yaml:"models,omitempty" json:"models,omitempty"`
	Context    ContextConfig      `yaml:"context" json:"context"`
	Tools      []ToolConfig       `yaml:"tools" json:"tools"`
	Hooks      []HookConfig       `yaml:"hooks" json:"hooks"`
	Delegation DelegationConfig   `yaml:"delegation,omitempty" json:"delegation,omitempty"`
	Meta       *MetaBuiltinConfig `yaml:"meta,omitempty" json:"meta,omitempty"`
}

// MetaBuiltinConfig configures the meta.* Starlark built-ins.
type MetaBuiltinConfig struct {
	Enabled      bool `yaml:"enabled" json:"enabled"`
	MaxTools     int  `yaml:"max_tools" json:"max_tools"`
	MaxHooks     int  `yaml:"max_hooks" json:"max_hooks"`
	MaxAgents    int  `yaml:"max_agents" json:"max_agents"`
	MaxCallDepth int  `yaml:"max_call_depth" json:"max_call_depth"`
}

// DelegationConfig defines delegation behavior.
type DelegationConfig struct {
	MaxDepth           int   `yaml:"max_depth,omitempty" json:"max_depth,omitempty"`
	MaxConcurrent      int   `yaml:"max_concurrent,omitempty" json:"max_concurrent,omitempty"`
	IterationsPerDepth []int `yaml:"iterations_per_depth,omitempty" json:"iterations_per_depth,omitempty"`
}

// ModelConfig defines the LLM provider and parameters.
type ModelConfig struct {
	Name        string  `yaml:"name" json:"name"`
	Provider    string  `yaml:"provider" json:"provider"`
	MaxTokens   int     `yaml:"max_tokens" json:"max_tokens"`
	Temperature float64 `yaml:"temperature" json:"temperature"`
	BaseURL     string  `yaml:"base_url" json:"base_url"`
	APIKeyEnv   string  `yaml:"api_key_env" json:"api_key_env"`
}

// ContextConfig defines context management parameters.
type ContextConfig struct {
	MaxHistory   int                   `yaml:"max_history" json:"max_history"`
	MaxTokens    int                   `yaml:"max_tokens" json:"max_tokens"`
	SystemPrompt string                `yaml:"system_prompt" json:"system_prompt"`
	Sources      []ContextSourceConfig `yaml:"sources,omitempty" json:"sources,omitempty"`
}

// ContextSourceConfig defines a declarative context source in harness.yaml / harness.md.
type ContextSourceConfig struct {
	Name     string `yaml:"name" json:"name"`
	Type     string `yaml:"type" json:"type"`
	Path     string `yaml:"path" json:"path"`
	When     string `yaml:"when,omitempty" json:"when,omitempty"`
	Trigger  string `yaml:"trigger,omitempty" json:"trigger,omitempty"`
	Priority int    `yaml:"priority,omitempty" json:"priority,omitempty"`
	Scope    string `yaml:"scope,omitempty" json:"scope,omitempty"`
	TTL      int    `yaml:"ttl,omitempty" json:"ttl,omitempty"`
}

// ToolConfig defines a tool in configuration.
type ToolConfig struct {
	Name        string                 `yaml:"name" json:"name"`
	Description string                 `yaml:"description" json:"description"`
	Parameters  map[string]ParamConfig `yaml:"parameters" json:"parameters"`
	TimeoutMS   int                    `yaml:"timeout_ms,omitempty" json:"timeout_ms,omitempty"`
	Script      string                 `yaml:"script,omitempty" json:"script,omitempty"`
}

// ParamConfig defines a tool parameter in configuration.
type ParamConfig struct {
	Type        string `yaml:"type" json:"type"`
	Description string `yaml:"description" json:"description"`
	Required    bool   `yaml:"required" json:"required"`
}

// HookConfig defines a hook registration in configuration.
type HookConfig struct {
	Event    string `yaml:"event" json:"event"`
	Handler  string `yaml:"handler" json:"handler"`
	Script   string `yaml:"script,omitempty" json:"script,omitempty"`
	When     string `yaml:"when,omitempty" json:"when,omitempty"`
	Priority int    `yaml:"priority,omitempty" json:"priority,omitempty"`
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
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return &cfg, nil
}

// ParseJSON parses JSON configuration data.
func ParseJSON(data []byte) (*Config, error) {
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}

	applyDefaults(&cfg)
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
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

// Validate ensures the configuration is internally consistent.
func (c *Config) Validate() error {
	var issues []string

	if strings.TrimSpace(c.Model.Name) == "" {
		issues = append(issues, "model.name cannot be empty")
	}
	if c.Model.Temperature < 0 || c.Model.Temperature > 2 {
		issues = append(issues, "model.temperature must be between 0 and 2")
	}
	if c.Model.MaxTokens <= 0 {
		issues = append(issues, "model.max_tokens must be greater than 0")
	}

	seenTools := make(map[string]struct{}, len(c.Tools))
	for i, tool := range c.Tools {
		name := strings.TrimSpace(tool.Name)
		if name == "" {
			issues = append(issues, fmt.Sprintf("tools[%d].name cannot be empty", i))
			continue
		}
		if _, exists := seenTools[name]; exists {
			issues = append(issues, fmt.Sprintf("tool %q is defined more than once", name))
			continue
		}
		if tool.TimeoutMS < 0 {
			issues = append(issues, fmt.Sprintf("tool %q timeout_ms must be >= 0", name))
		}
		seenTools[name] = struct{}{}
	}

	for i, hookCfg := range c.Hooks {
		if !hooks.IsValidEvent(hookCfg.Event) {
			issues = append(issues, fmt.Sprintf("hooks[%d].event %q is invalid", i, hookCfg.Event))
		}
	}

	if len(issues) > 0 {
		return fmt.Errorf("invalid config: %s", strings.Join(issues, "; "))
	}
	return nil
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
