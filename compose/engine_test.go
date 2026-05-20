package compose

import (
	"path/filepath"
	"testing"
)

func TestResolveHarness_BaseComposition(t *testing.T) {
	baseDir := filepath.Join("testdata")
	resolved, err := ResolveHarness(baseDir, "", ConditionContext{Values: map[string]interface{}{"mode": "chat"}})
	if err != nil {
		t.Fatalf("ResolveHarness error: %v", err)
	}

	if resolved.Identity != "# Base Identity\n\nYou are the base harness identity." {
		t.Fatalf("unexpected base identity: %q", resolved.Identity)
	}
	if len(resolved.Tools) != 1 || resolved.Tools[0].Name != "run_tests" {
		t.Fatalf("unexpected tools: %#v", resolved.Tools)
	}
	if len(resolved.Hooks) != 1 || resolved.Hooks[0].Handler != "guard_git" {
		t.Fatalf("unexpected hooks: %#v", resolved.Hooks)
	}
	if len(resolved.ContextBlocks) != 1 || resolved.ContextBlocks[0].Name != "git-workflow" {
		t.Fatalf("unexpected context blocks: %#v", resolved.ContextBlocks)
	}
}

func TestResolveHarness_AgentInheritanceAndAdditiveMerging(t *testing.T) {
	engine := NewEngine(filepath.Join("testdata"))
	resolved, err := engine.Resolve("coder", ConditionContext{Values: map[string]interface{}{"mode": "pull_request"}})
	if err != nil {
		t.Fatalf("Resolve error: %v", err)
	}

	if resolved.Identity != "# Coder Identity\n\nYou are the coding specialist agent." {
		t.Fatalf("unexpected agent identity: %q", resolved.Identity)
	}
	if resolved.Policy.Model.Name != "gpt-5-mini" {
		t.Fatalf("expected overridden model name, got %q", resolved.Policy.Model.Name)
	}
	if resolved.Policy.Context.MaxHistory != 15 {
		t.Fatalf("expected overridden context max_history, got %d", resolved.Policy.Context.MaxHistory)
	}
	if len(resolved.Tools) != 3 {
		t.Fatalf("expected 3 additive tools, got %d", len(resolved.Tools))
	}
	if len(resolved.Hooks) != 2 {
		t.Fatalf("expected 2 additive hooks, got %d", len(resolved.Hooks))
	}
	if len(resolved.ContextBlocks) != 3 {
		t.Fatalf("expected 3 active context blocks, got %d", len(resolved.ContextBlocks))
	}
	if resolved.ContextBlocks[1].Name != "pr-management" || resolved.ContextBlocks[2].Name != "testing" {
		t.Fatalf("unexpected context block order: %#v", resolved.ContextBlocks)
	}
}
