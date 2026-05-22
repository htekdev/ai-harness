// Package artifact implements the typed artifact registry for AI Harness.
//
// Artifacts are the fundamental building blocks of a harness configuration.
// Each artifact has a declared type that determines its validation rules,
// composition priority, and runtime behavior.
//
// The artifact type taxonomy (from highest to lowest priority):
//
//   - override: Project-local overrides that supersede any other artifact
//   - harness: The root harness identity and policy artifact
//   - builtin: Core capabilities shipped with the harness runtime
//   - plugin: Third-party or user-authored capability bundles
//   - model: Provider/model onboarding artifacts
package artifact

import (
	"fmt"
	"strings"
	"time"
)

// Type represents the artifact taxonomy.
type Type string

const (
	// TypeOverride is a project-local override that supersedes other artifacts.
	// Priority: 100 (highest). Only one override per name is allowed.
	TypeOverride Type = "override"

	// TypeHarness is the root harness identity/policy artifact.
	// Priority: 80. Exactly one harness artifact per project.
	TypeHarness Type = "harness"

	// TypeBuiltin is a core capability shipped with the runtime.
	// Priority: 60. Read-only; cannot be modified by users.
	TypeBuiltin Type = "builtin"

	// TypePlugin is a user-authored or third-party capability bundle.
	// Priority: 40. The primary extensibility surface.
	TypePlugin Type = "plugin"

	// TypeModel is a provider/model onboarding artifact.
	// Priority: 20 (lowest). Pluggable model configuration.
	TypeModel Type = "model"
)

// AllTypes returns all valid artifact types in priority order (highest first).
func AllTypes() []Type {
	return []Type{TypeOverride, TypeHarness, TypeBuiltin, TypePlugin, TypeModel}
}

// Priority returns the composition priority for this artifact type.
// Higher priority artifacts are resolved later and can override lower ones.
func (t Type) Priority() int {
	switch t {
	case TypeOverride:
		return 100
	case TypeHarness:
		return 80
	case TypeBuiltin:
		return 60
	case TypePlugin:
		return 40
	case TypeModel:
		return 20
	default:
		return 0
	}
}

// Valid returns true if this is a recognized artifact type.
func (t Type) Valid() bool {
	switch t {
	case TypeOverride, TypeHarness, TypeBuiltin, TypePlugin, TypeModel:
		return true
	default:
		return false
	}
}

// String implements fmt.Stringer.
func (t Type) String() string {
	return string(t)
}

// ParseType parses a string into an artifact Type.
func ParseType(s string) (Type, error) {
	t := Type(strings.TrimSpace(strings.ToLower(s)))
	if !t.Valid() {
		return "", fmt.Errorf("unknown artifact type %q; valid types: override, harness, builtin, plugin, model", s)
	}
	return t, nil
}

// Metadata holds the identity and provenance of an artifact.
type Metadata struct {
	// Name is the unique identifier for this artifact within its type.
	Name string `yaml:"name"`

	// Type declares the artifact's role in the taxonomy.
	Type Type `yaml:"type"`

	// Version is the semantic version of this artifact (e.g. "1.0.0").
	Version string `yaml:"version,omitempty"`

	// Description is a human-readable summary.
	Description string `yaml:"description,omitempty"`

	// Author identifies who created this artifact.
	Author string `yaml:"author,omitempty"`

	// Tags are free-form labels for categorization and filtering.
	Tags []string `yaml:"tags,omitempty"`

	// DependsOn lists artifact names this artifact requires.
	DependsOn []string `yaml:"depends_on,omitempty"`

	// CreatedAt is when this artifact was first registered.
	CreatedAt time.Time `yaml:"-"`
}

// Artifact is the fundamental unit of harness composition.
// Each artifact bundles identity metadata with capability definitions.
type Artifact struct {
	// Metadata contains typed identity information.
	Metadata Metadata

	// Condition is a Starlark expression controlling when this artifact is active.
	// Empty string means always active.
	Condition string `yaml:"condition,omitempty"`

	// Context is the markdown body — prose injected into the agent's knowledge.
	Context string `yaml:"-"`

	// Tools defined by this artifact.
	Tools []ToolDef `yaml:"tools,omitempty"`

	// Hooks defined by this artifact.
	Hooks []HookDef `yaml:"hooks,omitempty"`

	// Models defined by this artifact (only valid for TypeModel).
	Models []ModelDef `yaml:"models,omitempty"`

	// Source is the file path this artifact was loaded from.
	Source string `yaml:"-"`

	// Priority override (0 = use type default).
	PriorityOverride int `yaml:"priority,omitempty"`
}

// EffectivePriority returns the priority used for composition ordering.
// If PriorityOverride is set, it takes precedence over the type default.
func (a *Artifact) EffectivePriority() int {
	if a.PriorityOverride > 0 {
		return a.PriorityOverride
	}
	return a.Metadata.Type.Priority()
}

// ToolDef defines a tool provided by an artifact.
type ToolDef struct {
	Name        string              `yaml:"name"`
	Description string              `yaml:"description"`
	Parameters  map[string]ParamDef `yaml:"parameters,omitempty"`
	TimeoutMS   int                 `yaml:"timeout_ms,omitempty"`
	Script      string              `yaml:"script,omitempty"`
}

// ParamDef defines a tool parameter.
type ParamDef struct {
	Type        string `yaml:"type"`
	Required    bool   `yaml:"required"`
	Description string `yaml:"description,omitempty"`
}

// HookDef defines a lifecycle hook provided by an artifact.
type HookDef struct {
	Event    string `yaml:"event"`
	Handler  string `yaml:"handler"`
	Priority int    `yaml:"priority,omitempty"`
	When     string `yaml:"when,omitempty"`
	Script   string `yaml:"script,omitempty"`
	Tool     string `yaml:"tool,omitempty"`
	Action   string `yaml:"action,omitempty"`
	Reason   string `yaml:"reason,omitempty"`
}

// ModelDef defines a model/provider configuration in a model artifact.
type ModelDef struct {
	Name        string  `yaml:"name"`
	Provider    string  `yaml:"provider"`
	MaxTokens   int     `yaml:"max_tokens"`
	Temperature float64 `yaml:"temperature"`
	APIKeyEnv   string  `yaml:"api_key_env"`
	BaseURL     string  `yaml:"base_url,omitempty"`
}
