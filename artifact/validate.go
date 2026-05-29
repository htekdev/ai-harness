package artifact

import (
	"fmt"
	"regexp"
	"strings"
)

// namePattern validates artifact names: lowercase alphanumeric with hyphens.
var namePattern = regexp.MustCompile(`^[a-z][a-z0-9-]*[a-z0-9]$`)

// ValidationError contains one or more validation issues for an artifact.
type ValidationError struct {
	ArtifactName string
	Issues       []string
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("artifact %q has %d validation issue(s): %s",
		e.ArtifactName, len(e.Issues), strings.Join(e.Issues, "; "))
}

// Validate checks an artifact against its type-specific rules.
func Validate(a *Artifact) error {
	issues := make([]string, 0)

	// Common validations (all types)
	issues = append(issues, validateCommon(a)...)

	// Type-specific validations
	switch a.Metadata.Type {
	case TypeOverride:
		issues = append(issues, validateOverride(a)...)
	case TypeHarness:
		issues = append(issues, validateHarness(a)...)
	case TypeBuiltin:
		issues = append(issues, validateBuiltin(a)...)
	case TypeExtension:
		issues = append(issues, validateExtension(a)...)
	case TypePlugin:
		issues = append(issues, validatePlugin(a)...)
	case TypeModel:
		issues = append(issues, validateModel(a)...)
	default:
		issues = append(issues, fmt.Sprintf("unknown type %q", a.Metadata.Type))
	}

	if len(issues) > 0 {
		return &ValidationError{
			ArtifactName: a.Metadata.Name,
			Issues:       issues,
		}
	}
	return nil
}

func validateCommon(a *Artifact) []string {
	issues := make([]string, 0)

	if strings.TrimSpace(a.Metadata.Name) == "" {
		issues = append(issues, "name is required")
	} else if len(a.Metadata.Name) < 2 {
		issues = append(issues, "name must be at least 2 characters")
	} else if !namePattern.MatchString(a.Metadata.Name) {
		issues = append(issues, "name must be lowercase alphanumeric with hyphens (e.g. 'my-artifact')")
	}

	if !a.Metadata.Type.Valid() {
		issues = append(issues, fmt.Sprintf("type %q is not valid", a.Metadata.Type))
	}

	// Version is optional but must be semver-like if present
	if a.Metadata.Version != "" {
		if !isValidVersion(a.Metadata.Version) {
			issues = append(issues, fmt.Sprintf("version %q is not valid semver (expected X.Y.Z)", a.Metadata.Version))
		}
	}

	// Check for duplicate tool names
	toolNames := make(map[string]bool)
	for _, tool := range a.Tools {
		if strings.TrimSpace(tool.Name) == "" {
			issues = append(issues, "tool name cannot be empty")
		} else if toolNames[tool.Name] {
			issues = append(issues, fmt.Sprintf("duplicate tool name %q", tool.Name))
		}
		toolNames[tool.Name] = true
	}

	// Check for duplicate hook events+handlers
	hookKeys := make(map[string]bool)
	for _, hook := range a.Hooks {
		if strings.TrimSpace(hook.Event) == "" {
			issues = append(issues, "hook event cannot be empty")
		}
		key := hook.Event + ":" + hook.Handler
		if hookKeys[key] {
			issues = append(issues, fmt.Sprintf("duplicate hook %s:%s", hook.Event, hook.Handler))
		}
		hookKeys[key] = true
	}

	return issues
}

func validateOverride(a *Artifact) []string {
	issues := make([]string, 0)
	// Overrides must specify what they override via depends_on or context
	if len(a.Tools) == 0 && len(a.Hooks) == 0 && a.Context == "" {
		issues = append(issues, "override must provide at least one tool, hook, or context block")
	}
	return issues
}

func validateHarness(a *Artifact) []string {
	issues := make([]string, 0)
	// Harness artifacts must have context (the identity prompt)
	if strings.TrimSpace(a.Context) == "" {
		issues = append(issues, "harness artifact must provide identity context")
	}
	return issues
}

func validateBuiltin(a *Artifact) []string {
	issues := make([]string, 0)
	// Builtins must have at least one tool or hook (they provide capabilities)
	if len(a.Tools) == 0 && len(a.Hooks) == 0 {
		issues = append(issues, "builtin must provide at least one tool or hook")
	}
	// Builtins should have a version
	if a.Metadata.Version == "" {
		issues = append(issues, "builtin should declare a version")
	}
	return issues
}

func validatePlugin(a *Artifact) []string {
	issues := make([]string, 0)
	// Plugins must have a description
	if strings.TrimSpace(a.Metadata.Description) == "" {
		issues = append(issues, "plugin must have a description")
	}
	return issues
}

func validateExtension(a *Artifact) []string {
	issues := make([]string, 0)
	if len(a.Tools) == 0 && len(a.Hooks) == 0 && strings.TrimSpace(a.Context) == "" {
		issues = append(issues, "extension must provide at least one tool, hook, or context block")
	}
	return issues
}

func validateModel(a *Artifact) []string {
	issues := make([]string, 0)
	// Model artifacts must define at least one model
	if len(a.Models) == 0 {
		issues = append(issues, "model artifact must define at least one model")
	}
	for i, m := range a.Models {
		if strings.TrimSpace(m.Name) == "" {
			issues = append(issues, fmt.Sprintf("models[%d].name is required", i))
		}
		if strings.TrimSpace(m.Provider) == "" {
			issues = append(issues, fmt.Sprintf("models[%d].provider is required", i))
		}
		if m.MaxTokens <= 0 {
			issues = append(issues, fmt.Sprintf("models[%d].max_tokens must be > 0", i))
		}
	}
	// Model artifacts shouldn't have tools or hooks
	if len(a.Tools) > 0 {
		issues = append(issues, "model artifact should not define tools")
	}
	if len(a.Hooks) > 0 {
		issues = append(issues, "model artifact should not define hooks")
	}
	return issues
}

// isValidVersion checks for basic semver format X.Y.Z with optional pre-release.
func isValidVersion(v string) bool {
	parts := strings.SplitN(v, "-", 2)
	segments := strings.Split(parts[0], ".")
	if len(segments) < 2 || len(segments) > 3 {
		return false
	}
	for _, seg := range segments {
		if seg == "" {
			return false
		}
		for _, c := range seg {
			if c < '0' || c > '9' {
				return false
			}
		}
	}
	return true
}
