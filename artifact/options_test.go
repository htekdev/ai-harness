package artifact

import (
	"context"
	"testing"

	"github.com/htekdev/ai-harness/scripting"
)

func TestComposeWith_ActiveFilter(t *testing.T) {
	reg := NewRegistry()

	active := &Artifact{
		Metadata: Metadata{Name: "active-plugin", Type: TypePlugin, Description: "active"},
		Tools:    []ToolDef{{Name: "t1", Description: "active tool"}},
	}
	inactive := &Artifact{
		Metadata:  Metadata{Name: "inactive-plugin", Type: TypePlugin, Description: "inactive"},
		Condition: `ctx.get("mode") == "special"`,
		Tools:     []ToolDef{{Name: "t2", Description: "inactive tool"}},
	}
	harness := &Artifact{
		Metadata: Metadata{Name: "my-harness", Type: TypeHarness},
		Context:  "You are a coding assistant.",
	}

	for _, a := range []*Artifact{active, inactive, harness} {
		if err := reg.Register(a); err != nil {
			t.Fatal(err)
		}
	}

	// Simulate EvaluateConditions marking inactive-plugin as inactive
	inactive.Active = false

	composer := NewComposer(reg)

	// Default ComposeWith: only Active artifacts
	result, err := composer.ComposeWith()
	if err != nil {
		t.Fatalf("ComposeWith() failed: %v", err)
	}

	if len(result.Tools) != 1 {
		t.Errorf("expected 1 tool (inactive excluded), got %d", len(result.Tools))
	}
	if len(result.ActiveArtifacts) != 2 { // harness + active-plugin
		t.Errorf("expected 2 active artifacts, got %d", len(result.ActiveArtifacts))
	}
	if result.Identity != "You are a coding assistant." {
		t.Errorf("identity should come from harness, got %q", result.Identity)
	}
}

func TestComposeWith_IncludeInactive(t *testing.T) {
	reg := NewRegistry()

	a1 := &Artifact{
		Metadata: Metadata{Name: "p1", Type: TypePlugin, Description: "plugin one"},
		Tools:    []ToolDef{{Name: "t1", Description: "tool one"}},
	}
	a2 := &Artifact{
		Metadata:  Metadata{Name: "p2", Type: TypePlugin, Description: "plugin two"},
		Condition: "never",
		Tools:     []ToolDef{{Name: "t2", Description: "tool two"}},
	}

	for _, a := range []*Artifact{a1, a2} {
		if err := reg.Register(a); err != nil {
			t.Fatal(err)
		}
	}
	a2.Active = false

	composer := NewComposer(reg)

	// WithIncludeInactive includes everything
	result, err := composer.ComposeWith(WithIncludeInactive())
	if err != nil {
		t.Fatalf("ComposeWith(IncludeInactive) failed: %v", err)
	}
	if len(result.Tools) != 2 {
		t.Errorf("expected 2 tools (include inactive), got %d", len(result.Tools))
	}
	if len(result.ActiveArtifacts) != 2 {
		t.Errorf("expected 2 artifacts in result, got %d", len(result.ActiveArtifacts))
	}
	// Verify the Active field is reported correctly
	for _, s := range result.ActiveArtifacts {
		if s.Name == "p2" && s.Active {
			t.Error("p2 should report Active=false in summary")
		}
	}
}

func TestComposeWith_TypeFilter(t *testing.T) {
	reg := NewRegistry()

	harness := &Artifact{
		Metadata: Metadata{Name: "my-harness", Type: TypeHarness},
		Context:  "identity",
	}
	builtin := &Artifact{
		Metadata: Metadata{Name: "core", Type: TypeBuiltin, Version: "1.0.0"},
		Tools:    []ToolDef{{Name: "exec", Description: "exec"}},
	}
	plugin := &Artifact{
		Metadata: Metadata{Name: "git", Type: TypePlugin, Description: "git ops"},
		Tools:    []ToolDef{{Name: "git-status", Description: "git status"}},
	}
	model := &Artifact{
		Metadata: Metadata{Name: "openai", Type: TypeModel},
		Models:   []ModelDef{{Name: "gpt-4o", Provider: "openai", MaxTokens: 128000}},
	}

	for _, a := range []*Artifact{harness, builtin, plugin, model} {
		if err := reg.Register(a); err != nil {
			t.Fatal(err)
		}
	}

	composer := NewComposer(reg)

	// Only plugins
	result, err := composer.ComposeWith(WithTypeFilter(TypePlugin))
	if err != nil {
		t.Fatal(err)
	}
	if len(result.ActiveArtifacts) != 1 {
		t.Errorf("expected 1 artifact (plugin only), got %d", len(result.ActiveArtifacts))
	}
	if result.ActiveArtifacts[0].Name != "git" {
		t.Errorf("expected 'git', got %q", result.ActiveArtifacts[0].Name)
	}

	// Builtins + plugins
	result, err = composer.ComposeWith(WithTypeFilter(TypeBuiltin, TypePlugin))
	if err != nil {
		t.Fatal(err)
	}
	if len(result.ActiveArtifacts) != 2 {
		t.Errorf("expected 2 artifacts, got %d", len(result.ActiveArtifacts))
	}
}

func TestComposeWith_TagFilter(t *testing.T) {
	reg := NewRegistry()

	tagged := &Artifact{
		Metadata: Metadata{
			Name:        "tagged-plugin",
			Type:        TypePlugin,
			Description: "tagged",
			Tags:        []string{"governance", "security"},
		},
		Tools: []ToolDef{{Name: "audit", Description: "audit tool"}},
	}
	untagged := &Artifact{
		Metadata: Metadata{Name: "plain-plugin", Type: TypePlugin, Description: "plain"},
		Tools:    []ToolDef{{Name: "plain", Description: "plain tool"}},
	}

	for _, a := range []*Artifact{tagged, untagged} {
		if err := reg.Register(a); err != nil {
			t.Fatal(err)
		}
	}

	composer := NewComposer(reg)

	result, err := composer.ComposeWith(WithTagFilter("governance"))
	if err != nil {
		t.Fatal(err)
	}
	if len(result.ActiveArtifacts) != 1 {
		t.Errorf("expected 1 artifact (tag filter), got %d", len(result.ActiveArtifacts))
	}
	if result.ActiveArtifacts[0].Name != "tagged-plugin" {
		t.Errorf("expected 'tagged-plugin', got %q", result.ActiveArtifacts[0].Name)
	}
}

func TestComposeWith_EvalFn(t *testing.T) {
	reg := NewRegistry()

	always := &Artifact{
		Metadata: Metadata{Name: "always", Type: TypePlugin, Description: "always active"},
		Tools:    []ToolDef{{Name: "t1", Description: "always"}},
	}
	conditional := &Artifact{
		Metadata:  Metadata{Name: "cond", Type: TypePlugin, Description: "conditional"},
		Condition: "is_prod",
		Tools:     []ToolDef{{Name: "t2", Description: "conditional"}},
	}

	for _, a := range []*Artifact{always, conditional} {
		if err := reg.Register(a); err != nil {
			t.Fatal(err)
		}
	}

	composer := NewComposer(reg)

	// EvalFn that activates everything
	result, err := composer.ComposeWith(WithEvalFn(func(cond string) (bool, error) {
		return true, nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Tools) != 2 {
		t.Errorf("expected 2 tools, got %d", len(result.Tools))
	}

	// EvalFn that excludes conditional
	result, err = composer.ComposeWith(WithEvalFn(func(cond string) (bool, error) {
		return false, nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Tools) != 1 {
		t.Errorf("expected 1 tool (conditional excluded), got %d", len(result.Tools))
	}
}

func TestCompose_NilRespectsActiveField(t *testing.T) {
	reg := NewRegistry()

	a1 := &Artifact{
		Metadata: Metadata{Name: "plugin-a", Type: TypePlugin, Description: "a"},
		Tools:    []ToolDef{{Name: "ta", Description: "a"}},
	}
	a2 := &Artifact{
		Metadata:  Metadata{Name: "plugin-b", Type: TypePlugin, Description: "b"},
		Condition: `ctx.get("turn", 0) > 5`,
		Tools:     []ToolDef{{Name: "tb", Description: "b"}},
	}

	for _, a := range []*Artifact{a1, a2} {
		if err := reg.Register(a); err != nil {
			t.Fatal(err)
		}
	}

	// Simulate EvaluateConditions setting plugin-b to inactive
	a2.Active = false

	composer := NewComposer(reg)

	// Compose(nil) should now filter by Active field
	result, err := composer.Compose(nil)
	if err != nil {
		t.Fatalf("Compose(nil) failed: %v", err)
	}
	if len(result.Tools) != 1 {
		t.Errorf("expected 1 tool (inactive excluded by Active field), got %d", len(result.Tools))
	}
	if result.Tools[0].Name != "ta" {
		t.Errorf("expected tool 'ta', got %q", result.Tools[0].Name)
	}
}

func TestCompose_NilBackwardCompat_AllDefaultActive(t *testing.T) {
	// When no EvaluateConditions has been called, all artifacts are Active=true
	// by default after registration. Compose(nil) should include them all.
	reg := NewRegistry()

	a1 := &Artifact{
		Metadata: Metadata{Name: "p1", Type: TypePlugin, Description: "one"},
		Tools:    []ToolDef{{Name: "t1", Description: "one"}},
	}
	a2 := &Artifact{
		Metadata: Metadata{Name: "p2", Type: TypePlugin, Description: "two"},
		Tools:    []ToolDef{{Name: "t2", Description: "two"}},
	}

	for _, a := range []*Artifact{a1, a2} {
		if err := reg.Register(a); err != nil {
			t.Fatal(err)
		}
	}

	composer := NewComposer(reg)
	result, err := composer.Compose(nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Tools) != 2 {
		t.Errorf("expected 2 tools (backward compat: all active by default), got %d", len(result.Tools))
	}
}

func TestRegistry_Active(t *testing.T) {
	reg := NewRegistry()

	a1 := &Artifact{
		Metadata: Metadata{Name: "a1", Type: TypePlugin, Description: "one"},
	}
	a2 := &Artifact{
		Metadata: Metadata{Name: "a2", Type: TypePlugin, Description: "two"},
	}
	a3 := &Artifact{
		Metadata: Metadata{Name: "a3", Type: TypeBuiltin, Version: "1.0.0"},
		Tools:    []ToolDef{{Name: "core-tool", Description: "core"}},
	}

	for _, a := range []*Artifact{a1, a2, a3} {
		if err := reg.Register(a); err != nil {
			t.Fatal(err)
		}
	}

	// All active by default
	active := reg.Active()
	if len(active) != 3 {
		t.Errorf("expected 3 active by default, got %d", len(active))
	}

	// Deactivate one
	a2.Active = false
	active = reg.Active()
	if len(active) != 2 {
		t.Errorf("expected 2 active after deactivation, got %d", len(active))
	}

	// Verify ordering (model < plugin < builtin by priority)
	if active[0].Metadata.Name != "a1" || active[1].Metadata.Name != "a3" {
		t.Errorf("unexpected ordering: %s, %s", active[0].Metadata.Name, active[1].Metadata.Name)
	}
}

func TestComposeWith_CombinedFilters(t *testing.T) {
	reg := NewRegistry()

	p1 := &Artifact{
		Metadata: Metadata{
			Name:        "gov-plugin",
			Type:        TypePlugin,
			Description: "governance",
			Tags:        []string{"governance"},
		},
		Tools: []ToolDef{{Name: "audit", Description: "audit"}},
	}
	p2 := &Artifact{
		Metadata: Metadata{
			Name:        "util-plugin",
			Type:        TypePlugin,
			Description: "utility",
			Tags:        []string{"utility"},
		},
		Tools: []ToolDef{{Name: "format", Description: "format"}},
	}
	b1 := &Artifact{
		Metadata: Metadata{
			Name:    "core-builtin",
			Type:    TypeBuiltin,
			Version: "1.0.0",
			Tags:    []string{"governance"},
		},
		Tools: []ToolDef{{Name: "validate", Description: "validate"}},
	}

	for _, a := range []*Artifact{p1, p2, b1} {
		if err := reg.Register(a); err != nil {
			t.Fatal(err)
		}
	}

	composer := NewComposer(reg)

	// TypeFilter(Plugin) + TagFilter(governance) = only gov-plugin
	result, err := composer.ComposeWith(
		WithTypeFilter(TypePlugin),
		WithTagFilter("governance"),
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.ActiveArtifacts) != 1 {
		t.Errorf("expected 1 (type+tag intersection), got %d", len(result.ActiveArtifacts))
	}
	if result.ActiveArtifacts[0].Name != "gov-plugin" {
		t.Errorf("expected gov-plugin, got %q", result.ActiveArtifacts[0].Name)
	}
}

func TestComposeWith_EndToEnd_TurnCycle(t *testing.T) {
	// Full lifecycle: register → evaluate conditions → compose respects result
	reg := NewRegistry()

	always := &Artifact{
		Metadata: Metadata{Name: "core", Type: TypeBuiltin, Version: "1.0.0"},
		Tools:    []ToolDef{{Name: "exec", Description: "execute commands"}},
	}
	turnGated := &Artifact{
		Metadata:  Metadata{Name: "advanced", Type: TypePlugin, Description: "advanced tools"},
		Condition: `ctx.get("turn", 0) > 3`,
		Tools:     []ToolDef{{Name: "refactor", Description: "refactor code"}},
	}

	for _, a := range []*Artifact{always, turnGated} {
		if err := reg.Register(a); err != nil {
			t.Fatal(err)
		}
	}

	composer := NewComposer(reg)
	ctx := scripting.WithTurnState(context.Background())

	// Turn 1: advanced should be inactive
	scripting.SetTurnState(ctx, "turn", 1)
	if err := composer.EvaluateConditions(ctx); err != nil {
		t.Fatalf("eval turn 1: %v", err)
	}

	result, err := composer.Compose(nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Tools) != 1 {
		t.Errorf("turn 1: expected 1 tool, got %d", len(result.Tools))
	}
	if result.Tools[0].Name != "exec" {
		t.Errorf("turn 1: expected 'exec', got %q", result.Tools[0].Name)
	}

	// Turn 5: advanced should be active
	scripting.SetTurnState(ctx, "turn", 5)
	if err := composer.EvaluateConditions(ctx); err != nil {
		t.Fatalf("eval turn 5: %v", err)
	}

	result, err = composer.Compose(nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Tools) != 2 {
		t.Errorf("turn 5: expected 2 tools, got %d", len(result.Tools))
	}
}
