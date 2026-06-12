package compose

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

const (
	defaultModelName       = "gpt-4o"
	defaultModelProvider   = "openai"
	defaultModelMaxTokens  = 4096
	defaultTemperature     = 0.7
	defaultAPIKeyEnv       = "GITHUB_TOKEN"
	defaultContextHistory  = 50
	defaultContextTokens   = 128000
	defaultDelegationDepth = 3
	defaultMaxConcurrent   = 5
	defaultMetaTools       = 50
	defaultMetaHooks       = 30
	defaultMetaAgents      = 10
	defaultMetaCallDepth   = 5
)

var defaultIterationsPerDepth = []int{20, 10, 5, 3}

type policyOverride struct {
	Model      *modelPolicyOverride      `yaml:"model"`
	Models     *[]ModelPolicy            `yaml:"models,omitempty"`
	Delegation *delegationPolicyOverride `yaml:"delegation,omitempty"`
	Context    *contextPolicyOverride    `yaml:"context,omitempty"`
	Meta       *metaPolicyOverride       `yaml:"meta,omitempty"`
}

type modelPolicyOverride struct {
	Name        *string  `yaml:"name"`
	Provider    *string  `yaml:"provider"`
	MaxTokens   *int     `yaml:"max_tokens"`
	Temperature *float64 `yaml:"temperature"`
	APIKeyEnv   *string  `yaml:"api_key_env"`
	BaseURL     *string  `yaml:"base_url,omitempty"`
}

type delegationPolicyOverride struct {
	MaxDepth           *int   `yaml:"max_depth,omitempty"`
	MaxConcurrent      *int   `yaml:"max_concurrent,omitempty"`
	IterationsPerDepth *[]int `yaml:"iterations_per_depth,omitempty"`
}

type contextPolicyOverride struct {
	MaxHistory *int                `yaml:"max_history"`
	MaxTokens  *int                `yaml:"max_tokens"`
	Sources    *[]ContextSourceDef `yaml:"sources,omitempty"`
}

type metaPolicyOverride struct {
	Enabled      *bool `yaml:"enabled"`
	MaxTools     *int  `yaml:"max_tools"`
	MaxHooks     *int  `yaml:"max_hooks"`
	MaxAgents    *int  `yaml:"max_agents"`
	MaxCallDepth *int  `yaml:"max_call_depth"`
}

// DefaultPolicy returns the default harness policy.
func DefaultPolicy() Policy {
	return Policy{
		Model: ModelPolicy{
			Name:        defaultModelName,
			Provider:    defaultModelProvider,
			MaxTokens:   defaultModelMaxTokens,
			Temperature: defaultTemperature,
			APIKeyEnv:   defaultAPIKeyEnv,
		},
		Delegation: DelegationPolicy{
			MaxDepth:           defaultDelegationDepth,
			MaxConcurrent:      defaultMaxConcurrent,
			IterationsPerDepth: append([]int(nil), defaultIterationsPerDepth...),
		},
		Context: ContextPolicy{
			MaxHistory: defaultContextHistory,
			MaxTokens:  defaultContextTokens,
		},
		Meta: MetaPolicy{
			Enabled:      true,
			MaxTools:     defaultMetaTools,
			MaxHooks:     defaultMetaHooks,
			MaxAgents:    defaultMetaAgents,
			MaxCallDepth: defaultMetaCallDepth,
		},
	}
}

// LoadPolicy loads the base policy from .harness/identity.md frontmatter, applying defaults.
// All config lives in .md frontmatter — there are NO separate .yaml files.
func LoadPolicy(baseDir string) (Policy, error) {
	policy := DefaultPolicy()
	path := filepath.Join(baseDir, ".harness", "identity.md")
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return policy, nil
	}
	if err != nil {
		return Policy{}, fmt.Errorf("read identity %s: %w", path, err)
	}

	frontmatter, _, err := splitFrontmatter(data)
	if err != nil {
		// No frontmatter means no policy override — use defaults.
		return policy, nil
	}

	override, err := parsePolicyOverride(frontmatter)
	if err != nil {
		return Policy{}, fmt.Errorf("parse policy from identity.md: %w", err)
	}
	policy = mergePolicy(policy, override)
	if err := validatePolicy(policy); err != nil {
		return Policy{}, err
	}
	return policy, nil
}

// ResolvePolicy loads the base policy and applies any agent-specific override
// from .harness/agents/{name}/identity.md frontmatter.
func ResolvePolicy(baseDir, agentName string) (Policy, error) {
	policy, err := LoadPolicy(baseDir)
	if err != nil {
		return Policy{}, err
	}
	if strings.TrimSpace(agentName) == "" {
		return policy, nil
	}

	override, err := loadAgentPolicyOverride(baseDir, agentName)
	if err != nil {
		return Policy{}, err
	}
	policy = mergePolicy(policy, override)
	if err := validatePolicy(policy); err != nil {
		return Policy{}, err
	}
	return policy, nil
}

func loadAgentPolicyOverride(baseDir, agentName string) (policyOverride, error) {
	path := filepath.Join(baseDir, ".harness", "agents", agentName, "identity.md")
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return policyOverride{}, nil
	}
	if err != nil {
		return policyOverride{}, fmt.Errorf("read agent identity %s: %w", path, err)
	}

	frontmatter, _, err := splitFrontmatter(data)
	if err != nil {
		return policyOverride{}, nil // No frontmatter = no override
	}

	override, err := parsePolicyOverride(frontmatter)
	if err != nil {
		return policyOverride{}, fmt.Errorf("parse agent policy from identity.md: %w", err)
	}
	return override, nil
}

func parsePolicyOverride(data []byte) (policyOverride, error) {
	var override policyOverride
	if err := yaml.Unmarshal(data, &override); err != nil {
		return policyOverride{}, err
	}
	return override, nil
}

func mergePolicy(base Policy, override policyOverride) Policy {
	merged := base

	if override.Model != nil {
		if override.Model.Name != nil {
			merged.Model.Name = *override.Model.Name
		}
		if override.Model.Provider != nil {
			merged.Model.Provider = *override.Model.Provider
		}
		if override.Model.MaxTokens != nil {
			merged.Model.MaxTokens = *override.Model.MaxTokens
		}
		if override.Model.Temperature != nil {
			merged.Model.Temperature = *override.Model.Temperature
		}
		if override.Model.APIKeyEnv != nil {
			merged.Model.APIKeyEnv = *override.Model.APIKeyEnv
		}
		if override.Model.BaseURL != nil {
			merged.Model.BaseURL = *override.Model.BaseURL
		}
	}

	if override.Models != nil {
		merged.Models = append([]ModelPolicy(nil), (*override.Models)...)
	}

	if override.Delegation != nil {
		if override.Delegation.MaxDepth != nil {
			merged.Delegation.MaxDepth = *override.Delegation.MaxDepth
		}
		if override.Delegation.MaxConcurrent != nil {
			merged.Delegation.MaxConcurrent = *override.Delegation.MaxConcurrent
		}
		if override.Delegation.IterationsPerDepth != nil {
			merged.Delegation.IterationsPerDepth = append([]int(nil), (*override.Delegation.IterationsPerDepth)...)
		}
	}

	if override.Context != nil {
		if override.Context.MaxHistory != nil {
			merged.Context.MaxHistory = *override.Context.MaxHistory
		}
		if override.Context.MaxTokens != nil {
			merged.Context.MaxTokens = *override.Context.MaxTokens
		}
		if override.Context.Sources != nil {
			merged.Context.Sources = append([]ContextSourceDef(nil), (*override.Context.Sources)...)
		}
	}

	if override.Meta != nil {
		if override.Meta.Enabled != nil {
			merged.Meta.Enabled = *override.Meta.Enabled
		}
		if override.Meta.MaxTools != nil {
			merged.Meta.MaxTools = *override.Meta.MaxTools
		}
		if override.Meta.MaxHooks != nil {
			merged.Meta.MaxHooks = *override.Meta.MaxHooks
		}
		if override.Meta.MaxAgents != nil {
			merged.Meta.MaxAgents = *override.Meta.MaxAgents
		}
		if override.Meta.MaxCallDepth != nil {
			merged.Meta.MaxCallDepth = *override.Meta.MaxCallDepth
		}
	}

	return merged
}

func validatePolicy(policy Policy) error {
	issues := make([]string, 0)

	if strings.TrimSpace(policy.Model.Name) == "" {
		issues = append(issues, "model.name cannot be empty")
	}
	if strings.TrimSpace(policy.Model.Provider) == "" {
		issues = append(issues, "model.provider cannot be empty")
	}
	if strings.TrimSpace(policy.Model.APIKeyEnv) == "" {
		issues = append(issues, "model.api_key_env cannot be empty")
	}
	if policy.Model.MaxTokens <= 0 {
		issues = append(issues, "model.max_tokens must be greater than 0")
	}
	if policy.Model.Temperature < 0 || policy.Model.Temperature > 2 {
		issues = append(issues, "model.temperature must be between 0 and 2")
	}
	if policy.Context.MaxHistory <= 0 {
		issues = append(issues, "context.max_history must be greater than 0")
	}
	if policy.Context.MaxTokens <= 0 {
		issues = append(issues, "context.max_tokens must be greater than 0")
	}
	if policy.Delegation.MaxDepth <= 0 {
		issues = append(issues, "delegation.max_depth must be greater than 0")
	}
	if policy.Delegation.MaxConcurrent <= 0 {
		issues = append(issues, "delegation.max_concurrent must be greater than 0")
	}
	for i, count := range policy.Delegation.IterationsPerDepth {
		if count <= 0 {
			issues = append(issues, fmt.Sprintf("delegation.iterations_per_depth[%d] must be greater than 0", i))
		}
	}
	if len(policy.Delegation.IterationsPerDepth) == 0 {
		issues = append(issues, "delegation.iterations_per_depth must not be empty")
	}
	if policy.Meta.MaxTools <= 0 {
		issues = append(issues, "meta.max_tools must be greater than 0")
	}
	if policy.Meta.MaxHooks <= 0 {
		issues = append(issues, "meta.max_hooks must be greater than 0")
	}
	if policy.Meta.MaxAgents <= 0 {
		issues = append(issues, "meta.max_agents must be greater than 0")
	}
	if policy.Meta.MaxCallDepth <= 0 {
		issues = append(issues, "meta.max_call_depth must be greater than 0")
	}

	for i, model := range policy.Models {
		if strings.TrimSpace(model.Name) == "" {
			issues = append(issues, fmt.Sprintf("models[%d].name cannot be empty", i))
		}
		if model.MaxTokens <= 0 {
			issues = append(issues, fmt.Sprintf("models[%d].max_tokens must be greater than 0", i))
		}
		if model.Temperature < 0 || model.Temperature > 2 {
			issues = append(issues, fmt.Sprintf("models[%d].temperature must be between 0 and 2", i))
		}
	}

	if len(issues) > 0 {
		return fmt.Errorf("invalid policy: %s", strings.Join(issues, "; "))
	}
	return nil
}
