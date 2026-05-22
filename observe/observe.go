// Package observe provides context observability for AI Harness.
//
// Context observability is a first-class concept in AI Harness, answering three
// critical questions for every agent turn:
//
//  1. WHAT is in the context window?
//  2. WHERE did each piece come from? (provenance)
//  3. WHY is it active? (condition evaluation)
//
// The Snapshot type captures a point-in-time view of the composed context,
// including token estimates, artifact provenance, and composition ordering.
package observe

import (
	"fmt"
	"strings"
	"time"

	"github.com/htekdev/ai-harness/artifact"
)

// Snapshot is a point-in-time view of the composed context window.
// It captures everything needed to understand what the agent "sees" and why.
type Snapshot struct {
	// Timestamp when this snapshot was captured.
	Timestamp time.Time

	// Sections are the ordered blocks of context that form the window.
	Sections []Section

	// TokenBudget is the total token limit configured.
	TokenBudget int

	// TokensUsed is the estimated tokens consumed by the current context.
	TokensUsed int

	// TokensAvailable is the remaining tokens available for conversation.
	TokensAvailable int

	// ArtifactCount is the total number of registered artifacts.
	ArtifactCount int

	// ActiveArtifactCount is the number of artifacts that passed condition evaluation.
	ActiveArtifactCount int

	// InactiveArtifacts lists artifacts that were excluded by condition evaluation.
	InactiveArtifacts []InactiveEntry

	// Warnings surfaces issues like token budget overrun or missing dependencies.
	Warnings []string
}

// Section represents a contiguous block of context with provenance metadata.
type Section struct {
	// Name identifies this section.
	Name string

	// Kind categorizes the section's role.
	Kind SectionKind

	// Source is the file path or origin of this content.
	Source string

	// ArtifactType is the type of artifact that contributed this section.
	ArtifactType artifact.Type

	// Priority is the effective priority of the contributing artifact.
	Priority int

	// Condition is the Starlark expression that was evaluated (empty = always active).
	Condition string

	// Content is the actual text injected into the context window.
	Content string

	// TokenEstimate is the approximate tokens consumed by this section.
	TokenEstimate int

	// TokenPercent is the percentage of total budget this section consumes.
	TokenPercent float64

	// Tools lists tool names contributed by this section's artifact.
	Tools []string

	// Hooks lists hook handlers contributed by this section's artifact.
	Hooks []string
}

// SectionKind categorizes a context section.
type SectionKind string

const (
	// KindIdentity is the root agent identity/system prompt.
	KindIdentity SectionKind = "identity"

	// KindContext is injected knowledge/context from an artifact.
	KindContext SectionKind = "context"

	// KindToolSchema is the tool definition block.
	KindToolSchema SectionKind = "tool-schema"

	// KindHistory is the conversation history.
	KindHistory SectionKind = "history"
)

// InactiveEntry represents an artifact excluded by condition evaluation.
type InactiveEntry struct {
	Name      string
	Type      artifact.Type
	Condition string
	Reason    string
	Source    string
}

// Builder constructs a Snapshot from composed results and context state.
type Builder struct {
	budget int
}

// NewBuilder creates a Builder with the given token budget.
func NewBuilder(tokenBudget int) *Builder {
	if tokenBudget <= 0 {
		tokenBudget = 128000
	}
	return &Builder{budget: tokenBudget}
}

// Build creates a Snapshot from the artifact registry and compose results.
func (b *Builder) Build(reg *artifact.Registry, composed *artifact.ComposedResult) *Snapshot {
	snap := &Snapshot{
		Timestamp:           time.Now().UTC(),
		TokenBudget:         b.budget,
		ArtifactCount:       reg.Count(),
		ActiveArtifactCount: len(composed.ActiveArtifacts),
		Sections:            make([]Section, 0),
		InactiveArtifacts:   make([]InactiveEntry, 0),
		Warnings:            make([]string, 0),
	}

	// Identity section
	if composed.Identity != "" {
		tokens := estimateTokens(composed.Identity)
		snap.Sections = append(snap.Sections, Section{
			Name:          "identity",
			Kind:          KindIdentity,
			Source:        b.findHarnessSource(composed),
			ArtifactType:  artifact.TypeHarness,
			Priority:      artifact.TypeHarness.Priority(),
			Content:       composed.Identity,
			TokenEstimate: tokens,
		})
	}

	// Context blocks from active artifacts
	for _, ctx := range composed.ContextBlocks {
		tokens := estimateTokens(ctx.Content)
		section := Section{
			Name:          ctx.ArtifactName,
			Kind:          KindContext,
			Source:        ctx.Source,
			ArtifactType:  ctx.ArtifactType,
			Priority:      ctx.ArtifactType.Priority(),
			Content:       ctx.Content,
			TokenEstimate: tokens,
		}
		// Find the artifact to get tools/hooks/condition
		if a, ok := reg.Get(ctx.ArtifactName); ok {
			section.Condition = a.Condition
			section.Priority = a.EffectivePriority()
			for _, t := range a.Tools {
				section.Tools = append(section.Tools, t.Name)
			}
			for _, h := range a.Hooks {
				section.Hooks = append(section.Hooks, h.Handler)
			}
		}
		snap.Sections = append(snap.Sections, section)
	}

	// Tool schema section (aggregate)
	if len(composed.Tools) > 0 {
		schemaContent := b.buildToolSchemaEstimate(composed.Tools)
		tokens := estimateTokens(schemaContent)
		toolNames := make([]string, 0, len(composed.Tools))
		for _, t := range composed.Tools {
			toolNames = append(toolNames, t.Name)
		}
		snap.Sections = append(snap.Sections, Section{
			Name:          "tool-definitions",
			Kind:          KindToolSchema,
			Content:       schemaContent,
			TokenEstimate: tokens,
			Tools:         toolNames,
		})
	}

	// Calculate totals
	totalTokens := 0
	for i := range snap.Sections {
		totalTokens += snap.Sections[i].TokenEstimate
	}
	snap.TokensUsed = totalTokens
	snap.TokensAvailable = b.budget - totalTokens

	// Compute percentages
	for i := range snap.Sections {
		if b.budget > 0 {
			snap.Sections[i].TokenPercent = float64(snap.Sections[i].TokenEstimate) / float64(b.budget) * 100
		}
	}

	// Find inactive artifacts
	allArtifacts := reg.All()
	activeNames := make(map[string]bool)
	for _, a := range composed.ActiveArtifacts {
		activeNames[a.Name] = true
	}
	for _, a := range allArtifacts {
		if !activeNames[a.Metadata.Name] {
			snap.InactiveArtifacts = append(snap.InactiveArtifacts, InactiveEntry{
				Name:      a.Metadata.Name,
				Type:      a.Metadata.Type,
				Condition: a.Condition,
				Reason:    "condition evaluated to false",
				Source:    a.Source,
			})
		}
	}

	// Generate warnings
	if snap.TokensUsed > b.budget {
		snap.Warnings = append(snap.Warnings,
			fmt.Sprintf("token budget exceeded: %d/%d (%.1f%% over)",
				snap.TokensUsed, b.budget,
				float64(snap.TokensUsed-b.budget)/float64(b.budget)*100))
	} else if float64(snap.TokensUsed) > float64(b.budget)*0.9 {
		snap.Warnings = append(snap.Warnings,
			fmt.Sprintf("token budget nearly full: %d/%d (%.1f%% used)",
				snap.TokensUsed, b.budget,
				float64(snap.TokensUsed)/float64(b.budget)*100))
	}

	return snap
}

// findHarnessSource locates the source of the harness identity artifact.
func (b *Builder) findHarnessSource(composed *artifact.ComposedResult) string {
	for _, a := range composed.ActiveArtifacts {
		if a.Type == artifact.TypeHarness {
			return a.Source
		}
	}
	return ""
}

// buildToolSchemaEstimate generates a representative tool schema string for token estimation.
func (b *Builder) buildToolSchemaEstimate(tools []artifact.ToolDef) string {
	var sb strings.Builder
	for _, t := range tools {
		sb.WriteString(fmt.Sprintf("tool: %s\n  description: %s\n", t.Name, t.Description))
		for name, p := range t.Parameters {
			sb.WriteString(fmt.Sprintf("  param: %s (%s", name, p.Type))
			if p.Required {
				sb.WriteString(", required")
			}
			sb.WriteString(")\n")
		}
	}
	return sb.String()
}

// estimateTokens provides a rough token estimate using ~4 chars per token.
func estimateTokens(content string) int {
	if content == "" {
		return 0
	}
	return len(content)/4 + 4 // +4 for message overhead
}

// Format renders the snapshot in the specified output format.
func (snap *Snapshot) Format(mode FormatMode) string {
	switch mode {
	case FormatSummary:
		return snap.formatSummary()
	case FormatDetailed:
		return snap.formatDetailed()
	case FormatJSON:
		return snap.formatJSON()
	default:
		return snap.formatSummary()
	}
}

// FormatMode controls the output verbosity.
type FormatMode int

const (
	// FormatSummary shows a high-level overview of context composition.
	FormatSummary FormatMode = iota
	// FormatDetailed shows per-section provenance and token breakdown.
	FormatDetailed
	// FormatJSON outputs the snapshot as structured JSON.
	FormatJSON
)

func (snap *Snapshot) formatSummary() string {
	var sb strings.Builder

	sb.WriteString("Context Window Snapshot\n")
	sb.WriteString(strings.Repeat("═", 60) + "\n\n")

	// Token budget bar
	usedPct := float64(snap.TokensUsed) / float64(snap.TokenBudget) * 100
	barLen := 40
	filled := int(usedPct / 100 * float64(barLen))
	if filled > barLen {
		filled = barLen
	}
	bar := strings.Repeat("█", filled) + strings.Repeat("░", barLen-filled)
	sb.WriteString(fmt.Sprintf("  Tokens: [%s] %d / %d (%.1f%%)\n", bar, snap.TokensUsed, snap.TokenBudget, usedPct))
	sb.WriteString(fmt.Sprintf("  Available: %d tokens for conversation\n", snap.TokensAvailable))
	sb.WriteString(fmt.Sprintf("  Artifacts: %d active / %d total\n\n", snap.ActiveArtifactCount, snap.ArtifactCount))

	// Sections overview
	sb.WriteString("  Sections (composition order):\n")
	sb.WriteString(strings.Repeat("─", 60) + "\n")

	for _, s := range snap.Sections {
		pctStr := fmt.Sprintf("%.1f%%", s.TokenPercent)
		kindStr := string(s.Kind)
		sb.WriteString(fmt.Sprintf("  %-20s %-12s %6d tok  %6s\n", s.Name, kindStr, s.TokenEstimate, pctStr))
	}

	// Inactive artifacts
	if len(snap.InactiveArtifacts) > 0 {
		sb.WriteString(fmt.Sprintf("\n  Inactive (%d):\n", len(snap.InactiveArtifacts)))
		for _, ia := range snap.InactiveArtifacts {
			sb.WriteString(fmt.Sprintf("    ✗ %s (%s) — %s\n", ia.Name, ia.Type, ia.Reason))
		}
	}

	// Warnings
	if len(snap.Warnings) > 0 {
		sb.WriteString("\n  ⚠ Warnings:\n")
		for _, w := range snap.Warnings {
			sb.WriteString(fmt.Sprintf("    • %s\n", w))
		}
	}

	return sb.String()
}

func (snap *Snapshot) formatDetailed() string {
	var sb strings.Builder

	sb.WriteString("Context Window — Detailed Provenance\n")
	sb.WriteString(strings.Repeat("═", 70) + "\n\n")

	// Token budget
	usedPct := float64(snap.TokensUsed) / float64(snap.TokenBudget) * 100
	sb.WriteString(fmt.Sprintf("Token Budget: %d / %d (%.1f%% used)\n", snap.TokensUsed, snap.TokenBudget, usedPct))
	sb.WriteString(fmt.Sprintf("Captured: %s\n\n", snap.Timestamp.Format(time.RFC3339)))

	for i, s := range snap.Sections {
		sb.WriteString(fmt.Sprintf("┌─ Section %d: %s\n", i+1, s.Name))
		sb.WriteString(fmt.Sprintf("│  Kind:       %s\n", s.Kind))
		if s.Source != "" {
			sb.WriteString(fmt.Sprintf("│  Source:     %s\n", s.Source))
		}
		if s.ArtifactType != "" {
			sb.WriteString(fmt.Sprintf("│  Artifact:   [%s] priority=%d\n", s.ArtifactType, s.Priority))
		}
		if s.Condition != "" {
			sb.WriteString(fmt.Sprintf("│  Condition:  %s\n", s.Condition))
		}
		sb.WriteString(fmt.Sprintf("│  Tokens:     %d (%.1f%% of budget)\n", s.TokenEstimate, s.TokenPercent))
		if len(s.Tools) > 0 {
			sb.WriteString(fmt.Sprintf("│  Tools:      %s\n", strings.Join(s.Tools, ", ")))
		}
		if len(s.Hooks) > 0 {
			sb.WriteString(fmt.Sprintf("│  Hooks:      %s\n", strings.Join(s.Hooks, ", ")))
		}

		// Content preview (first 3 lines)
		if s.Content != "" {
			lines := strings.SplitN(s.Content, "\n", 4)
			preview := lines
			if len(lines) > 3 {
				preview = lines[:3]
			}
			sb.WriteString("│  Preview:\n")
			for _, line := range preview {
				if len(line) > 70 {
					line = line[:67] + "..."
				}
				sb.WriteString(fmt.Sprintf("│    %s\n", line))
			}
			if len(lines) > 3 {
				sb.WriteString("│    ...\n")
			}
		}
		sb.WriteString("└" + strings.Repeat("─", 69) + "\n\n")
	}

	// Inactive artifacts
	if len(snap.InactiveArtifacts) > 0 {
		sb.WriteString(fmt.Sprintf("Inactive Artifacts (%d):\n", len(snap.InactiveArtifacts)))
		sb.WriteString(strings.Repeat("─", 70) + "\n")
		for _, ia := range snap.InactiveArtifacts {
			sb.WriteString(fmt.Sprintf("  ✗ [%s] %s\n", ia.Type, ia.Name))
			sb.WriteString(fmt.Sprintf("    Condition: %s\n", ia.Condition))
			sb.WriteString(fmt.Sprintf("    Source:    %s\n\n", ia.Source))
		}
	}

	// Warnings
	if len(snap.Warnings) > 0 {
		sb.WriteString("Warnings:\n")
		for _, w := range snap.Warnings {
			sb.WriteString(fmt.Sprintf("  ⚠ %s\n", w))
		}
	}

	return sb.String()
}

func (snap *Snapshot) formatJSON() string {
	var sb strings.Builder
	sb.WriteString("{\n")
	sb.WriteString(fmt.Sprintf("  \"timestamp\": %q,\n", snap.Timestamp.Format(time.RFC3339)))
	sb.WriteString(fmt.Sprintf("  \"token_budget\": %d,\n", snap.TokenBudget))
	sb.WriteString(fmt.Sprintf("  \"tokens_used\": %d,\n", snap.TokensUsed))
	sb.WriteString(fmt.Sprintf("  \"tokens_available\": %d,\n", snap.TokensAvailable))
	sb.WriteString(fmt.Sprintf("  \"artifacts_total\": %d,\n", snap.ArtifactCount))
	sb.WriteString(fmt.Sprintf("  \"artifacts_active\": %d,\n", snap.ActiveArtifactCount))

	sb.WriteString("  \"sections\": [\n")
	for i, s := range snap.Sections {
		sb.WriteString("    {\n")
		sb.WriteString(fmt.Sprintf("      \"name\": %q,\n", s.Name))
		sb.WriteString(fmt.Sprintf("      \"kind\": %q,\n", s.Kind))
		sb.WriteString(fmt.Sprintf("      \"source\": %q,\n", s.Source))
		if s.ArtifactType != "" {
			sb.WriteString(fmt.Sprintf("      \"artifact_type\": %q,\n", s.ArtifactType))
		}
		sb.WriteString(fmt.Sprintf("      \"priority\": %d,\n", s.Priority))
		sb.WriteString(fmt.Sprintf("      \"tokens\": %d,\n", s.TokenEstimate))
		sb.WriteString(fmt.Sprintf("      \"token_percent\": %.2f\n", s.TokenPercent))
		if i < len(snap.Sections)-1 {
			sb.WriteString("    },\n")
		} else {
			sb.WriteString("    }\n")
		}
	}
	sb.WriteString("  ],\n")

	sb.WriteString(fmt.Sprintf("  \"warnings\": ["))
	for i, w := range snap.Warnings {
		if i > 0 {
			sb.WriteString(", ")
		}
		sb.WriteString(fmt.Sprintf("%q", w))
	}
	sb.WriteString("]\n")
	sb.WriteString("}\n")

	return sb.String()
}
