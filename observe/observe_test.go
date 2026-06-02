package observe

import (
	"strings"
	"testing"

	"github.com/htekdev/ai-harness/artifact"
)

func TestNewBuilder(t *testing.T) {
	t.Run("default budget", func(t *testing.T) {
		b := NewBuilder(0)
		if b.budget != 128000 {
			t.Errorf("expected default budget 128000, got %d", b.budget)
		}
	})

	t.Run("custom budget", func(t *testing.T) {
		b := NewBuilder(200000)
		if b.budget != 200000 {
			t.Errorf("expected budget 200000, got %d", b.budget)
		}
	})

	t.Run("negative budget uses default", func(t *testing.T) {
		b := NewBuilder(-1)
		if b.budget != 128000 {
			t.Errorf("expected default budget 128000, got %d", b.budget)
		}
	})
}

func TestEstimateTokens(t *testing.T) {
	tests := []struct {
		input string
		want  int
	}{
		{"", 0},
		{"hello", 5},                     // 5/4 + 4 = 5
		{strings.Repeat("a", 100), 29},   // 100/4 + 4 = 29
		{strings.Repeat("a", 1000), 254}, // 1000/4 + 4 = 254
	}

	for _, tt := range tests {
		got := estimateTokens(tt.input)
		if got != tt.want {
			t.Errorf("estimateTokens(%d chars) = %d, want %d", len(tt.input), got, tt.want)
		}
	}
}

func buildTestRegistry(t *testing.T) (*artifact.Registry, *artifact.ComposedResult) {
	t.Helper()

	reg := artifact.NewRegistry()

	harness := &artifact.Artifact{
		Metadata: artifact.Metadata{
			Name:        "my-agent",
			Type:        artifact.TypeHarness,
			Version:     "1.0.0",
			Description: "Test harness",
		},
		Context: "You are a helpful coding assistant.\nYou write Go code.",
		Source:  ".harness/identity.md",
	}
	if err := reg.Register(harness); err != nil {
		t.Fatalf("register harness: %v", err)
	}

	builtin := &artifact.Artifact{
		Metadata: artifact.Metadata{
			Name:        "core-tools",
			Type:        artifact.TypeBuiltin,
			Version:     "1.0.0",
			Description: "Core tool definitions",
		},
		Context: "Use these tools for file operations and shell access.",
		Source:  ".harness/builtins/core-tools.md",
		Tools: []artifact.ToolDef{
			{Name: "read_file", Description: "Read a file", Parameters: map[string]artifact.ParamDef{"path": {Type: "string", Required: true}}},
			{Name: "write_file", Description: "Write a file", Parameters: map[string]artifact.ParamDef{"path": {Type: "string", Required: true}, "content": {Type: "string", Required: true}}},
			{Name: "shell", Description: "Run a shell command", Parameters: map[string]artifact.ParamDef{"command": {Type: "string", Required: true}}},
		},
		Hooks: []artifact.HookDef{
			{Event: "onPreToolUse", Handler: "file-guard"},
		},
	}
	if err := reg.Register(builtin); err != nil {
		t.Fatalf("register builtin: %v", err)
	}

	plugin := &artifact.Artifact{
		Metadata: artifact.Metadata{
			Name:        "git-ops",
			Type:        artifact.TypePlugin,
			Version:     "0.5.0",
			Description: "Git operations plugin",
		},
		Context: "Git workflow helper.",
		Source:  ".harness/plugins/git-ops.md",
		Tools: []artifact.ToolDef{
			{Name: "git_commit", Description: "Create a commit", Parameters: map[string]artifact.ParamDef{"message": {Type: "string", Required: true}}},
		},
	}
	if err := reg.Register(plugin); err != nil {
		t.Fatalf("register plugin: %v", err)
	}

	conditionalPlugin := &artifact.Artifact{
		Metadata: artifact.Metadata{
			Name:        "deploy-tools",
			Type:        artifact.TypePlugin,
			Version:     "1.0.0",
			Description: "Deployment tools (conditional)",
		},
		Condition: "ctx.get('env') == 'production'",
		Context:   "Production deployment helpers.",
		Source:    ".harness/plugins/deploy-tools.md",
	}
	if err := reg.Register(conditionalPlugin); err != nil {
		t.Fatalf("register conditional: %v", err)
	}

	compaction := &artifact.Artifact{
		Metadata: artifact.Metadata{
			Name:    "review-compaction",
			Type:    artifact.TypeCompaction,
			Version: "1.0.0",
		},
		Source: ".harness/compaction/review.md",
		Compaction: artifact.CompactionDef{
			Triggers: []artifact.CompactionTrigger{
				{TokenThreshold: 0.0001, Strategy: "truncate"},
				{TokenThreshold: 0.0002, Strategy: "summarize"},
			},
			Retention: artifact.CompactionRetention{
				AlwaysKeep: []string{"system_prompt"},
				Summarize:  []string{"tool_results"},
				Drop:       []string{"exploratory_logs"},
			},
			Strategies: map[string]artifact.CompactionStrategy{
				"truncate":  {Description: "trim"},
				"summarize": {Prompt: "summarize"},
			},
		},
	}
	if err := reg.Register(compaction); err != nil {
		t.Fatalf("register compaction: %v", err)
	}

	// Compose without the conditional artifact active
	composer := artifact.NewComposer(reg)
	composed, err := composer.Compose(func(condition string) (bool, error) {
		// Only activate artifacts without conditions
		return false, nil
	})
	if err != nil {
		t.Fatalf("compose: %v", err)
	}

	return reg, composed
}

func TestBuildSnapshot(t *testing.T) {
	reg, composed := buildTestRegistry(t)
	builder := NewBuilder(128000)
	snap := builder.Build(reg, composed)

	t.Run("basic fields", func(t *testing.T) {
		if snap.TokenBudget != 128000 {
			t.Errorf("expected budget 128000, got %d", snap.TokenBudget)
		}
		if snap.ArtifactCount != 5 {
			t.Errorf("expected 5 total artifacts, got %d", snap.ArtifactCount)
		}
		if snap.ActiveArtifactCount != 4 {
			t.Errorf("expected 4 active artifacts, got %d", snap.ActiveArtifactCount)
		}
		if snap.Timestamp.IsZero() {
			t.Error("timestamp should not be zero")
		}
	})

	t.Run("sections created", func(t *testing.T) {
		// identity + 2 context blocks (builtin + plugin) + tool-schema = 4
		if len(snap.Sections) < 3 {
			t.Errorf("expected at least 3 sections, got %d", len(snap.Sections))
		}

		// First section should be identity
		if snap.Sections[0].Kind != KindIdentity {
			t.Errorf("first section should be identity, got %s", snap.Sections[0].Kind)
		}
		if snap.Sections[0].Name != "identity" {
			t.Errorf("identity section name = %q", snap.Sections[0].Name)
		}
	})

	t.Run("token accounting", func(t *testing.T) {
		if snap.TokensUsed <= 0 {
			t.Error("tokens used should be positive")
		}
		if snap.TokensAvailable != snap.TokenBudget-snap.TokensUsed {
			t.Errorf("tokens available = %d, want %d", snap.TokensAvailable, snap.TokenBudget-snap.TokensUsed)
		}
	})

	t.Run("inactive artifacts tracked", func(t *testing.T) {
		if len(snap.InactiveArtifacts) != 1 {
			t.Errorf("expected 1 inactive artifact, got %d", len(snap.InactiveArtifacts))
		}
		if len(snap.InactiveArtifacts) > 0 {
			if snap.InactiveArtifacts[0].Name != "deploy-tools" {
				t.Errorf("expected deploy-tools inactive, got %q", snap.InactiveArtifacts[0].Name)
			}
		}
	})

	t.Run("token percent calculated", func(t *testing.T) {
		for _, s := range snap.Sections {
			if s.TokenEstimate > 0 && s.TokenPercent <= 0 {
				t.Errorf("section %q has tokens=%d but percent=%.2f", s.Name, s.TokenEstimate, s.TokenPercent)
			}
		}
	})

	t.Run("compaction state", func(t *testing.T) {
		if snap.Compaction == nil {
			t.Fatal("expected compaction state to be present")
		}
		if !snap.Compaction.Triggered {
			t.Fatal("expected compaction to be triggered with low thresholds")
		}
		if len(snap.Compaction.AppliedStrategies) == 0 {
			t.Fatal("expected applied compaction strategies")
		}
		if len(snap.Compaction.Dropped) == 0 || snap.Compaction.Dropped[0] != "exploratory_logs" {
			t.Fatalf("unexpected compaction dropped list: %+v", snap.Compaction.Dropped)
		}
	})
}

func TestBuildSnapshotWarnings(t *testing.T) {
	reg := artifact.NewRegistry()
	harness := &artifact.Artifact{
		Metadata: artifact.Metadata{
			Name:        "my-agent",
			Type:        artifact.TypeHarness,
			Version:     "1.0.0",
			Description: "Huge harness",
		},
		Context: strings.Repeat("x", 500), // 500 chars = ~129 tokens
		Source:  ".harness/identity.md",
	}
	if err := reg.Register(harness); err != nil {
		t.Fatalf("register: %v", err)
	}

	composer := artifact.NewComposer(reg)
	composed, err := composer.Compose(nil)
	if err != nil {
		t.Fatalf("compose: %v", err)
	}

	t.Run("budget exceeded warning", func(t *testing.T) {
		builder := NewBuilder(50) // tiny budget
		snap := builder.Build(reg, composed)
		if len(snap.Warnings) == 0 {
			t.Error("expected at least one warning for budget overrun")
		}
		found := false
		for _, w := range snap.Warnings {
			if strings.Contains(w, "exceeded") {
				found = true
			}
		}
		if !found {
			t.Errorf("expected 'exceeded' warning, got: %v", snap.Warnings)
		}
	})

	t.Run("near-full warning", func(t *testing.T) {
		builder := NewBuilder(140) // just above 90%
		snap := builder.Build(reg, composed)
		found := false
		for _, w := range snap.Warnings {
			if strings.Contains(w, "nearly full") {
				found = true
			}
		}
		if !found {
			t.Logf("tokens used: %d, budget: %d", snap.TokensUsed, 140)
			// Only warn if actually near full
			if float64(snap.TokensUsed) > float64(140)*0.9 {
				t.Errorf("expected 'nearly full' warning, got: %v", snap.Warnings)
			}
		}
	})
}

func TestSnapshotFormatSummary(t *testing.T) {
	reg, composed := buildTestRegistry(t)
	builder := NewBuilder(128000)
	snap := builder.Build(reg, composed)

	output := snap.Format(FormatSummary)

	checks := []string{
		"Context Window Snapshot",
		"Tokens:",
		"Available:",
		"Artifacts:",
		"Sections",
	}
	for _, check := range checks {
		if !strings.Contains(output, check) {
			t.Errorf("summary output missing %q", check)
		}
	}
}

func TestSnapshotFormatDetailed(t *testing.T) {
	reg, composed := buildTestRegistry(t)
	builder := NewBuilder(128000)
	snap := builder.Build(reg, composed)

	output := snap.Format(FormatDetailed)

	checks := []string{
		"Detailed Provenance",
		"Token Budget:",
		"Section 1:",
		"Kind:",
		"Source:",
		"Tokens:",
	}
	for _, check := range checks {
		if !strings.Contains(output, check) {
			t.Errorf("detailed output missing %q", check)
		}
	}
}

func TestSnapshotFormatJSON(t *testing.T) {
	reg, composed := buildTestRegistry(t)
	builder := NewBuilder(128000)
	snap := builder.Build(reg, composed)

	output := snap.Format(FormatJSON)

	if !strings.HasPrefix(output, "{") {
		t.Error("JSON output should start with {")
	}
	checks := []string{
		"\"timestamp\":",
		"\"token_budget\":",
		"\"tokens_used\":",
		"\"sections\":",
		"\"warnings\":",
		"\"compaction\":",
	}
	for _, check := range checks {
		if !strings.Contains(output, check) {
			t.Errorf("JSON output missing %q", check)
		}
	}
}

func TestSnapshotToolSection(t *testing.T) {
	reg, composed := buildTestRegistry(t)
	builder := NewBuilder(128000)
	snap := builder.Build(reg, composed)

	// Find tool-schema section
	var toolSection *Section
	for i := range snap.Sections {
		if snap.Sections[i].Kind == KindToolSchema {
			toolSection = &snap.Sections[i]
			break
		}
	}

	if toolSection == nil {
		t.Fatal("no tool-schema section found")
	}

	if len(toolSection.Tools) == 0 {
		t.Error("tool section should list tool names")
	}

	// Should include tools from all active artifacts
	expectedTools := map[string]bool{"read_file": false, "write_file": false, "shell": false, "git_commit": false}
	for _, name := range toolSection.Tools {
		if _, ok := expectedTools[name]; ok {
			expectedTools[name] = true
		}
	}
	for name, found := range expectedTools {
		if !found {
			t.Errorf("expected tool %q in tool section", name)
		}
	}
}

func TestSnapshotProvenanceTracking(t *testing.T) {
	reg, composed := buildTestRegistry(t)
	builder := NewBuilder(128000)
	snap := builder.Build(reg, composed)

	// Every context section should have a source
	for _, s := range snap.Sections {
		if s.Kind == KindContext {
			if s.Source == "" {
				t.Errorf("context section %q missing source provenance", s.Name)
			}
			if s.ArtifactType == "" {
				t.Errorf("context section %q missing artifact type", s.Name)
			}
		}
	}
}

func TestEmptyRegistry(t *testing.T) {
	reg := artifact.NewRegistry()
	composer := artifact.NewComposer(reg)
	composed, err := composer.Compose(nil)
	if err != nil {
		t.Fatalf("compose: %v", err)
	}

	builder := NewBuilder(128000)
	snap := builder.Build(reg, composed)

	if snap.ArtifactCount != 0 {
		t.Errorf("expected 0 artifacts, got %d", snap.ArtifactCount)
	}
	if snap.TokensUsed != 0 {
		t.Errorf("expected 0 tokens used, got %d", snap.TokensUsed)
	}
	if len(snap.Sections) != 0 {
		t.Errorf("expected 0 sections, got %d", len(snap.Sections))
	}
}
