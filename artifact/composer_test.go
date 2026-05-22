package artifact

import (
	"testing"
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
