package artifact

import (
	"context"
	"errors"
	"testing"

	"github.com/htekdev/ai-harness/scripting"
)

func TestCompose_BasicMerge(t *testing.T) {
	reg := NewRegistry()

	harness := &Artifact{
		Metadata: Metadata{Name: "my-harness", Type: TypeHarness},
		Context:  "You are a helpful coding assistant.",
	}
	builtin := &Artifact{
		Metadata: Metadata{Name: "core-tools", Type: TypeBuiltin, Version: "1.0.0"},
		Tools: []ToolDef{
			{Name: "exec", Description: "Execute commands"},
			{Name: "read", Description: "Read files"},
		},
		Context: "Core tools documentation.",
	}
	plugin := &Artifact{
		Metadata: Metadata{Name: "git-plugin", Type: TypePlugin, Description: "Git operations"},
		Tools: []ToolDef{
			{Name: "git-status", Description: "Show git status"},
		},
		Context: "Git plugin context.",
	}

	for _, a := range []*Artifact{harness, builtin, plugin} {
		if err := reg.Register(a); err != nil {
			t.Fatal(err)
		}
	}

	composer := NewComposer(reg)
	result, err := composer.Compose(nil) // no condition eval
	if err != nil {
		t.Fatalf("Compose failed: %v", err)
	}

	if result.Identity != "You are a helpful coding assistant." {
		t.Errorf("identity = %q", result.Identity)
	}
	if len(result.Tools) != 3 {
		t.Errorf("expected 3 tools, got %d", len(result.Tools))
	}
	if len(result.ContextBlocks) != 2 { // builtin + plugin (harness context is Identity)
		t.Errorf("expected 2 context blocks, got %d", len(result.ContextBlocks))
	}
	if len(result.ActiveArtifacts) != 3 {
		t.Errorf("expected 3 active artifacts, got %d", len(result.ActiveArtifacts))
	}
}

func TestCompose_ToolOverride(t *testing.T) {
	reg := NewRegistry()

	builtin := &Artifact{
		Metadata: Metadata{Name: "core-tools", Type: TypeBuiltin, Version: "1.0.0"},
		Tools: []ToolDef{
			{Name: "exec", Description: "Original exec", TimeoutMS: 5000},
		},
	}
	override := &Artifact{
		Metadata: Metadata{Name: "exec-override", Type: TypeOverride},
		Tools: []ToolDef{
			{Name: "exec", Description: "Overridden exec", TimeoutMS: 30000},
		},
	}

	for _, a := range []*Artifact{builtin, override} {
		if err := reg.Register(a); err != nil {
			t.Fatal(err)
		}
	}

	composer := NewComposer(reg)
	result, err := composer.Compose(nil)
	if err != nil {
		t.Fatal(err)
	}

	if len(result.Tools) != 1 {
		t.Fatalf("expected 1 tool (override replaces), got %d", len(result.Tools))
	}
	if result.Tools[0].Description != "Overridden exec" {
		t.Errorf("expected overridden description, got %q", result.Tools[0].Description)
	}
	if result.Tools[0].TimeoutMS != 30000 {
		t.Errorf("expected timeout 30000, got %d", result.Tools[0].TimeoutMS)
	}
}

func TestCompose_ConditionalExclusion(t *testing.T) {
	reg := NewRegistry()

	always := &Artifact{
		Metadata: Metadata{Name: "always-plugin", Type: TypePlugin, Description: "always active"},
		Tools:    []ToolDef{{Name: "t1", Description: "always"}},
	}
	conditional := &Artifact{
		Metadata:  Metadata{Name: "conditional-plugin", Type: TypePlugin, Description: "sometimes"},
		Condition: "is_prod",
		Tools:     []ToolDef{{Name: "t2", Description: "conditional"}},
	}

	for _, a := range []*Artifact{always, conditional} {
		if err := reg.Register(a); err != nil {
			t.Fatal(err)
		}
	}

	composer := NewComposer(reg)

	// Exclude conditional
	result, err := composer.Compose(func(cond string) (bool, error) {
		return false, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Tools) != 1 {
		t.Errorf("expected 1 tool (conditional excluded), got %d", len(result.Tools))
	}
}

func TestCompose_ModelArtifacts(t *testing.T) {
	reg := NewRegistry()

	models := &Artifact{
		Metadata: Metadata{Name: "openai-models", Type: TypeModel},
		Models: []ModelDef{
			{Name: "gpt-4o", Provider: "openai", MaxTokens: 128000, Temperature: 0.7, APIKeyEnv: "OPENAI_KEY"},
			{Name: "gpt-4o-mini", Provider: "openai", MaxTokens: 16000, Temperature: 0.5, APIKeyEnv: "OPENAI_KEY"},
		},
	}

	if err := reg.Register(models); err != nil {
		t.Fatal(err)
	}

	composer := NewComposer(reg)
	result, err := composer.Compose(nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Models) != 2 {
		t.Errorf("expected 2 models, got %d", len(result.Models))
	}
}

func TestComposerSummary(t *testing.T) {
	reg := NewRegistry()

	artifacts := []*Artifact{
		{Metadata: Metadata{Name: "my-harness", Type: TypeHarness}, Context: "identity"},
		{Metadata: Metadata{Name: "core-tools", Type: TypeBuiltin, Version: "1.0.0"}, Tools: []ToolDef{{Name: "t1", Description: "test"}}},
		{Metadata: Metadata{Name: "my-plugin", Type: TypePlugin, Description: "test"}, Tools: []ToolDef{{Name: "t2", Description: "test"}}},
	}

	for _, a := range artifacts {
		if err := reg.Register(a); err != nil {
			t.Fatal(err)
		}
	}

	composer := NewComposer(reg)
	summary := composer.Summary()
	if summary == "" {
		t.Error("summary should not be empty")
	}
	if len(summary) < 50 {
		t.Errorf("summary seems too short: %q", summary)
	}
}

func TestEvaluateConditions_TurnNumber(t *testing.T) {
	reg := NewRegistry()
	a := &Artifact{
		Metadata:  Metadata{Name: "turn-context", Type: TypePlugin, Description: "turn-aware plugin"},
		Condition: `ctx.get("turn", 0) > 3`,
	}
	if err := reg.Register(a); err != nil {
		t.Fatal(err)
	}
	composer := NewComposer(reg)

	ctx := scripting.WithTurnState(context.Background())

	// Turn 1: condition is false → inactive
	scripting.SetTurnState(ctx, "turn", 1)
	if err := composer.EvaluateConditions(ctx); err != nil {
		t.Fatalf("turn 1 eval failed: %v", err)
	}
	if a.Active {
		t.Error("expected artifact inactive at turn 1")
	}

	// Turn 4: condition is true → active
	scripting.SetTurnState(ctx, "turn", 4)
	if err := composer.EvaluateConditions(ctx); err != nil {
		t.Fatalf("turn 4 eval failed: %v", err)
	}
	if !a.Active {
		t.Error("expected artifact active at turn 4")
	}
}

func TestEvaluateConditions_NoWhenExpr(t *testing.T) {
	reg := NewRegistry()
	a := &Artifact{
		Metadata: Metadata{Name: "always-on", Type: TypePlugin, Description: "always active plugin"},
	}
	if err := reg.Register(a); err != nil {
		t.Fatal(err)
	}
	composer := NewComposer(reg)

	ctx := scripting.WithTurnState(context.Background())
	scripting.SetTurnState(ctx, "turn", 1)

	if err := composer.EvaluateConditions(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !a.Active {
		t.Error("artifact with no condition should always be active")
	}
}

func TestEvaluateConditions_EvalError(t *testing.T) {
	reg := NewRegistry()
	badArtifact := &Artifact{
		Metadata:  Metadata{Name: "bad-cond", Type: TypePlugin, Description: "bad condition plugin"},
		Condition: `1 // 0`, // integer floor-divide-by-zero at runtime
	}
	if err := reg.Register(badArtifact); err != nil {
		t.Fatal(err)
	}
	// Manually set prior Active state to false to verify it's retained
	badArtifact.Active = false

	composer := NewComposer(reg)
	ctx := scripting.WithTurnState(context.Background())
	err := composer.EvaluateConditions(ctx)
	if err == nil {
		t.Fatal("expected error from bad condition")
	}
	// prior Active state (false) must be retained on error
	if badArtifact.Active {
		t.Error("Active state should be retained on eval error (was false, must stay false)")
	}
}

func TestEvaluateConditions_NilContext(t *testing.T) {
	reg := NewRegistry()
	if err := reg.Register(&Artifact{Metadata: Metadata{Name: "aa", Type: TypePlugin, Description: "nil context plugin"}, Condition: `True`}); err != nil {
		t.Fatal(err)
	}
	composer := NewComposer(reg)

	// context.Background() has no turn state
	err := composer.EvaluateConditions(context.Background())
	if err == nil {
		t.Fatal("expected ErrNilTurnContext")
	}
	if !errors.Is(err, ErrNilTurnContext) {
		t.Errorf("expected ErrNilTurnContext, got %v", err)
	}
}

func TestEvaluateConditions_ScratchpadKey(t *testing.T) {
	reg := NewRegistry()
	a := &Artifact{
		Metadata:  Metadata{Name: "mode-context", Type: TypePlugin, Description: "mode-aware plugin"},
		Condition: `ctx.get("mode", "") == "review"`,
	}
	if err := reg.Register(a); err != nil {
		t.Fatal(err)
	}
	composer := NewComposer(reg)

	ctx := scripting.WithTurnState(context.Background())

	// mode not set: inactive
	if err := composer.EvaluateConditions(ctx); err != nil {
		t.Fatalf("eval failed: %v", err)
	}
	if a.Active {
		t.Error("expected inactive when mode not set")
	}

	// mode = "review": active
	scripting.SetTurnState(ctx, "mode", "review")
	if err := composer.EvaluateConditions(ctx); err != nil {
		t.Fatalf("eval failed: %v", err)
	}
	if !a.Active {
		t.Error("expected active when mode=review")
	}
}

func TestEvaluateConditions_TimeExpr(t *testing.T) {
	reg := NewRegistry()
	a := &Artifact{
		Metadata:  Metadata{Name: "time-context", Type: TypePlugin, Description: "time-aware plugin"},
		Condition: `len(time.now()) > 0`,
	}
	if err := reg.Register(a); err != nil {
		t.Fatal(err)
	}
	composer := NewComposer(reg)

	ctx := scripting.WithTurnState(context.Background())
	if err := composer.EvaluateConditions(ctx); err != nil {
		t.Fatalf("time expr eval failed: %v", err)
	}
	if !a.Active {
		t.Error("expected active with time.now() expr")
	}
}

func TestCompose_BackwardCompat_WithActiveField(t *testing.T) {
	reg := NewRegistry()
	a := &Artifact{
		Metadata: Metadata{Name: "compat", Type: TypePlugin, Description: "compat plugin"},
		Tools:    []ToolDef{{Name: "t1", Description: "test"}},
	}
	if err := reg.Register(a); err != nil {
		t.Fatal(err)
	}
	composer := NewComposer(reg)

	// Compose(nil) must still work as before — all artifacts active
	result, err := composer.Compose(nil)
	if err != nil {
		t.Fatalf("Compose(nil) failed: %v", err)
	}
	if len(result.Tools) != 1 {
		t.Errorf("expected 1 tool, got %d", len(result.Tools))
	}
	// Active field defaults to true
	if !a.Active {
		t.Error("Active should default to true after registration")
	}
}
