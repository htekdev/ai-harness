package artifact

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/htekdev/ai-harness/compose"
	"github.com/htekdev/ai-harness/scripting"
)

// Composer resolves a set of registered artifacts into a unified harness view.
// It handles composition ordering, override resolution, and conflict detection.
type Composer struct {
	registry *Registry
}

// NewComposer creates a Composer backed by the given registry.
func NewComposer(reg *Registry) *Composer {
	return &Composer{registry: reg}
}

// ComposedResult is the output of artifact composition — the unified
// view of all active artifacts merged by priority order.
type ComposedResult struct {
	// Identity is the merged identity context (from harness + overrides).
	Identity string

	// Tools is the deduplicated, ordered list of all tools.
	// Higher-priority artifacts' tools override lower-priority ones with the same name.
	Tools []ToolDef

	// Hooks is the merged list of all hooks, ordered by artifact priority.
	Hooks []HookDef

	// ContextBlocks is all context content from active artifacts, in priority order.
	ContextBlocks []ContextEntry

	// Models is the merged list of model configurations.
	Models []ModelDef

	// ActiveArtifacts lists the artifacts that contributed to this result.
	ActiveArtifacts []ArtifactSummary
}

// ContextEntry represents a block of context from an active artifact.
type ContextEntry struct {
	ArtifactName string
	ArtifactType Type
	Content      string
	Source       string
}

// ArtifactSummary is a lightweight view of an artifact for composition reports.
type ArtifactSummary struct {
	Name     string
	Type     Type
	Version  string
	Priority int
	Source   string
	Active   bool
}

// Compose evaluates all registered artifacts and merges them by priority.
// The evalFn is called for each artifact's condition to determine if it's active.
// Pass nil for evalFn to treat all artifacts as active.
func (c *Composer) Compose(evalFn func(condition string) (bool, error)) (*ComposedResult, error) {
	var active []*Artifact
	var err error

	if evalFn == nil {
		active = c.registry.All()
	} else {
		active, err = c.registry.Resolve(evalFn)
		if err != nil {
			return nil, err
		}
	}

	result := &ComposedResult{
		Tools:           make([]ToolDef, 0),
		Hooks:           make([]HookDef, 0),
		ContextBlocks:   make([]ContextEntry, 0),
		Models:          make([]ModelDef, 0),
		ActiveArtifacts: make([]ArtifactSummary, 0, len(active)),
	}

	// Track tools by name for override resolution
	toolMap := make(map[string]ToolDef)
	toolOrder := make([]string, 0)

	// Process artifacts in priority order (lowest first, overrides last)
	for _, a := range active {
		result.ActiveArtifacts = append(result.ActiveArtifacts, ArtifactSummary{
			Name:     a.Metadata.Name,
			Type:     a.Metadata.Type,
			Version:  a.Metadata.Version,
			Priority: a.EffectivePriority(),
			Source:   a.Source,
			Active:   true,
		})

		// Identity: harness type provides the base identity
		if a.Metadata.Type == TypeHarness && a.Context != "" {
			result.Identity = a.Context
		}

		// Context blocks from all non-harness artifacts
		if a.Context != "" && a.Metadata.Type != TypeHarness {
			result.ContextBlocks = append(result.ContextBlocks, ContextEntry{
				ArtifactName: a.Metadata.Name,
				ArtifactType: a.Metadata.Type,
				Content:      a.Context,
				Source:       a.Source,
			})
		}

		// Override identity if an override provides context
		if a.Metadata.Type == TypeOverride && a.Context != "" {
			// Override context is appended, not replacing identity
			result.ContextBlocks = append(result.ContextBlocks, ContextEntry{
				ArtifactName: a.Metadata.Name,
				ArtifactType: a.Metadata.Type,
				Content:      a.Context,
				Source:       a.Source,
			})
		}

		// Tools: higher priority overrides same-named tools
		for _, tool := range a.Tools {
			if _, exists := toolMap[tool.Name]; !exists {
				toolOrder = append(toolOrder, tool.Name)
			}
			toolMap[tool.Name] = tool
		}

		// Hooks: all hooks are merged (no override, just priority ordering)
		result.Hooks = append(result.Hooks, a.Hooks...)

		// Models
		result.Models = append(result.Models, a.Models...)
	}

	// Rebuild tools list in discovery order (but with overridden definitions)
	for _, name := range toolOrder {
		result.Tools = append(result.Tools, toolMap[name])
	}

	return result, nil
}

// Summary returns a human-readable summary of the registry contents.
func (c *Composer) Summary() string {
	var sb strings.Builder
	all := c.registry.All()

	sb.WriteString(fmt.Sprintf("Artifact Registry: %d artifacts\n", len(all)))
	sb.WriteString(strings.Repeat("─", 60) + "\n")

	byType := make(map[Type][]*Artifact)
	for _, a := range all {
		byType[a.Metadata.Type] = append(byType[a.Metadata.Type], a)
	}

	for _, t := range AllTypes() {
		arts := byType[t]
		if len(arts) == 0 {
			continue
		}
		sb.WriteString(fmt.Sprintf("\n[%s] (%d)\n", t, len(arts)))
		for _, a := range arts {
			version := ""
			if a.Metadata.Version != "" {
				version = " v" + a.Metadata.Version
			}
			sb.WriteString(fmt.Sprintf("  • %s%s — %s\n", a.Metadata.Name, version, a.Metadata.Description))
		}
	}

	return sb.String()
}

// ErrNilTurnContext is returned by EvaluateConditions when no turn state
// is available on the provided context.
var ErrNilTurnContext = errors.New("turn state not available in context")

// EvaluateConditions re-runs all artifact condition expressions against the
// current turn state and updates each artifact's Active field in place.
// Non-fatal: per-artifact condition errors retain prior active state.
// Returns ErrNilTurnContext if ctx has no turn state attached.
func (c *Composer) EvaluateConditions(ctx context.Context) error {
	values, ok := scripting.TurnStateValues(ctx)
	if !ok {
		return ErrNilTurnContext
	}

	condCtx := compose.ConditionContext{
		Values: values,
	}

	return c.registry.UpdateConditions(func(condition string) (bool, error) {
		return compose.EvaluateCondition(condition, condCtx)
	})
}
