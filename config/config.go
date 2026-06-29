// Package config handles YAML-based configuration for the AI harness.
package config

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/htekdev/ai-harness/harness/errs"
	"github.com/htekdev/ai-harness/hooks"
	"gopkg.in/yaml.v3"
)

// Config is the top-level harness configuration.
type Config struct {
	Model       ModelConfig        `yaml:"model" json:"model"`
	Models      []ModelConfig      `yaml:"models,omitempty" json:"models,omitempty"`
	Context     ContextConfig      `yaml:"context" json:"context"`
	Tools       []ToolConfig       `yaml:"tools" json:"tools"`
	ToolsPolicy *ToolsPolicyConfig `yaml:"tools_policy,omitempty" json:"tools_policy,omitempty"`
	Hooks       []HookConfig       `yaml:"hooks" json:"hooks"`
	Delegation  DelegationConfig   `yaml:"delegation,omitempty" json:"delegation,omitempty"`
	Meta        *MetaBuiltinConfig `yaml:"meta,omitempty" json:"meta,omitempty"`
	Serve       *ServeConfig       `yaml:"serve,omitempty" json:"serve,omitempty"`
	Network     *NetworkConfig     `yaml:"network,omitempty" json:"network,omitempty"`
}

// NetworkConfig configures the harness network sandbox enforced by the
// http.* Starlark built-ins (Phase 5.5 — Production Hardening).
//
// When AllowedDomains is non-empty, the sandbox switches to default-deny:
// only listed domains (and their sub-domains) may be reached. Each entry
// is a domain name; the special entry "*" disables host filtering while
// still rejecting non-http(s) schemes. See scripting.NewNetworkSandbox
// for the full matching rules.
//
// When this block is omitted (or AllowedDomains is empty), behaviour is
// unchanged from pre-5.5: scripts may reach any host. This preserves
// back-compat for every existing config.
type NetworkConfig struct {
	AllowedDomains []string `yaml:"allowed_domains,omitempty" json:"allowed_domains,omitempty"`
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
	Name        string       `yaml:"name" json:"name"`
	Provider    string       `yaml:"provider" json:"provider"`
	MaxTokens   int          `yaml:"max_tokens" json:"max_tokens"`
	Temperature float64      `yaml:"temperature" json:"temperature"`
	BaseURL     string       `yaml:"base_url" json:"base_url"`
	APIKeyEnv   string       `yaml:"api_key_env" json:"api_key_env"`
	Retry       *RetryConfig `yaml:"retry,omitempty" json:"retry,omitempty"`
}

// RetryConfig configures completion-client retry behavior per model.
// All fields are optional; zero values mean "use the harness default".
type RetryConfig struct {
	MaxRetries       *int    `yaml:"max_retries,omitempty" json:"max_retries,omitempty"`
	InitialBackoffMS int     `yaml:"initial_backoff_ms,omitempty" json:"initial_backoff_ms,omitempty"`
	MaxBackoffMS     int     `yaml:"max_backoff_ms,omitempty" json:"max_backoff_ms,omitempty"`
	Multiplier       float64 `yaml:"multiplier,omitempty" json:"multiplier,omitempty"`
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

// ToolsPolicyConfig declares per-session governance over which registered
// tools the agent may invoke. Patterns support shell-style globs (e.g.
// "fs.*"). Mode is optional — "allowlist" or "denylist"; when omitted it is
// inferred from the lists (non-empty allow ⇒ allowlist, otherwise
// denylist). Deny entries always win over Allow.
type ToolsPolicyConfig struct {
	Mode  string   `yaml:"mode,omitempty" json:"mode,omitempty"`
	Allow []string `yaml:"allow,omitempty" json:"allow,omitempty"`
	Deny  []string `yaml:"deny,omitempty" json:"deny,omitempty"`
}

// IsEmpty reports whether the config carries no governance rules.
func (t *ToolsPolicyConfig) IsEmpty() bool {
	return t == nil || (len(t.Allow) == 0 && len(t.Deny) == 0 && t.Mode == "")
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
		return nil, errs.Wrap(errs.KindConfig, "config.load", err, "read config file")
	}

	return Parse(data)
}

// Parse parses YAML configuration data.
func Parse(data []byte) (*Config, error) {
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, errs.Wrap(errs.KindConfig, "config.parse", err, "parse config")
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
		return nil, errs.Wrap(errs.KindConfig, "config.parsejson", err, "parse config")
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
	if err := validateRetry("model", c.Model.Retry); err != nil {
		issues = append(issues, err.Error())
	}
	for i, m := range c.Models {
		if err := validateRetry(fmt.Sprintf("models[%d]", i), m.Retry); err != nil {
			issues = append(issues, err.Error())
		}
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

	if !c.ToolsPolicy.IsEmpty() {
		mode := strings.ToLower(strings.TrimSpace(c.ToolsPolicy.Mode))
		if mode != "" && mode != "allowlist" && mode != "denylist" {
			issues = append(issues, fmt.Sprintf("tools_policy.mode %q must be allowlist|denylist", c.ToolsPolicy.Mode))
		}
		for i, p := range c.ToolsPolicy.Allow {
			if strings.TrimSpace(p) == "" {
				issues = append(issues, fmt.Sprintf("tools_policy.allow[%d] is empty", i))
			}
		}
		for i, p := range c.ToolsPolicy.Deny {
			if strings.TrimSpace(p) == "" {
				issues = append(issues, fmt.Sprintf("tools_policy.deny[%d] is empty", i))
			}
		}
	}

	if err := c.Serve.Validate(); err != nil {
		issues = append(issues, err.Error())
	}

	if len(issues) > 0 {
		return errs.Newf(errs.KindConfig, "config.validate", "invalid config: %s", strings.Join(issues, "; "))
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

// validateRetry checks per-field bounds on a RetryConfig.
func validateRetry(prefix string, r *RetryConfig) error {
	if r == nil {
		return nil
	}
	if r.MaxRetries != nil && *r.MaxRetries < 0 {
		return fmt.Errorf("%s.retry.max_retries must be >= 0", prefix)
	}
	if r.InitialBackoffMS < 0 {
		return fmt.Errorf("%s.retry.initial_backoff_ms must be >= 0", prefix)
	}
	if r.MaxBackoffMS < 0 {
		return fmt.Errorf("%s.retry.max_backoff_ms must be >= 0", prefix)
	}
	if r.Multiplier < 0 {
		return fmt.Errorf("%s.retry.multiplier must be >= 0", prefix)
	}
	return nil
}
