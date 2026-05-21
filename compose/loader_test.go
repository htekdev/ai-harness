package compose

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadBlocks_Discovery(t *testing.T) {
	baseDir := t.TempDir()
	harnessDir := filepath.Join(baseDir, ".harness")
	if err := os.MkdirAll(filepath.Join(harnessDir, "agents", "coder"), 0o755); err != nil {
		t.Fatal(err)
	}

	writeTestFile(t, filepath.Join(harnessDir, "identity.md"), "---\nmodel:\n  name: gpt-4o\n---\n\n# Base Identity")
	writeTestBlock(t, filepath.Join(harnessDir, "alpha.md"), "alpha", "", "Alpha context", nil, nil)
	writeTestBlock(t, filepath.Join(harnessDir, "beta.md"), "beta", "", "Beta context", nil, nil)
	writeTestBlock(t, filepath.Join(harnessDir, "agents", "coder", "testing.md"), "testing", "", "Testing context", nil, nil)

	blocks, err := LoadBlocks(baseDir)
	if err != nil {
		t.Fatalf("LoadBlocks error: %v", err)
	}
	if len(blocks) != 2 {
		t.Fatalf("expected 2 base blocks, got %d", len(blocks))
	}
	if blocks[0].Name != "alpha" || blocks[1].Name != "beta" {
		t.Fatalf("unexpected block order: %#v", []string{blocks[0].Name, blocks[1].Name})
	}

	agentBlocks, err := LoadAgentBlocks(baseDir, "coder")
	if err != nil {
		t.Fatalf("LoadAgentBlocks error: %v", err)
	}
	if len(agentBlocks) != 1 || agentBlocks[0].Name != "testing" {
		t.Fatalf("unexpected agent blocks: %#v", agentBlocks)
	}
}

func TestParseBlock_ParsesFrontmatterAndSections(t *testing.T) {
	input := []byte("---\n" +
		"name: git-workflow\n" +
		"description: Git workflow guidance\n" +
		"condition: ctx.get(\"mode\") == \"pr\"\n" +
		"tools:\n" +
		"  - name: run_tests\n" +
		"    description: Run tests\n" +
		"    parameters:\n" +
		"      package:\n" +
		"        type: string\n" +
		"        required: true\n" +
		"    timeout_ms: 5000\n" +
		"hooks:\n" +
		"  - event: tool.pre\n" +
		"    handler: guard\n" +
		"    priority: 1\n" +
		"---\n\n" +
		"Use the git workflow carefully.\n")

	block, err := ParseBlock(input, "git-workflow.md")
	if err != nil {
		t.Fatalf("ParseBlock error: %v", err)
	}

	if block.Name != "git-workflow" {
		t.Fatalf("unexpected block name: %q", block.Name)
	}
	if block.Description != "Git workflow guidance" {
		t.Fatalf("unexpected description: %q", block.Description)
	}
	if block.Condition != `ctx.get("mode") == "pr"` {
		t.Fatalf("unexpected condition: %q", block.Condition)
	}
	if block.Context != "Use the git workflow carefully." {
		t.Fatalf("unexpected context: %q", block.Context)
	}
	if len(block.Tools) != 1 || block.Tools[0].Name != "run_tests" {
		t.Fatalf("unexpected tools: %#v", block.Tools)
	}
	if block.Tools[0].TimeoutMS != 5000 {
		t.Fatalf("unexpected timeout: %d", block.Tools[0].TimeoutMS)
	}
	if len(block.Hooks) != 1 || block.Hooks[0].Handler != "guard" {
		t.Fatalf("unexpected hooks: %#v", block.Hooks)
	}
}

func TestParseBlock_MalformedFileHandling(t *testing.T) {
	// Missing closing frontmatter delimiter
	_, err := ParseBlock([]byte("---\nname: broken\n"), "broken.md")
	if err == nil {
		t.Fatal("expected missing closing frontmatter delimiter error")
	}

	// Missing name field
	_, err = ParseBlock([]byte("---\ndescription: no name\n---\n\nSome context\n"), "broken.md")
	if err == nil {
		t.Fatal("expected block name required error")
	}

	// No frontmatter at all
	_, err = ParseBlock([]byte("Just some text without frontmatter"), "broken.md")
	if err == nil {
		t.Fatal("expected no frontmatter error")
	}
}

func TestLoadIdentity(t *testing.T) {
	baseDir := t.TempDir()
	writeTestFile(t, filepath.Join(baseDir, ".harness", "identity.md"),
		"---\nmodel:\n  name: gpt-4o\n---\n\n# Identity\n\nBase prompt")

	identity, err := LoadIdentity(baseDir)
	if err != nil {
		t.Fatalf("LoadIdentity error: %v", err)
	}
	if identity != "# Identity\n\nBase prompt" {
		t.Fatalf("unexpected identity: %q", identity)
	}
}

func TestLoadIdentity_NoFrontmatter(t *testing.T) {
	baseDir := t.TempDir()
	writeTestFile(t, filepath.Join(baseDir, ".harness", "identity.md"), "# Identity\n\nPlain prompt")

	identity, err := LoadIdentity(baseDir)
	if err != nil {
		t.Fatalf("LoadIdentity error: %v", err)
	}
	if identity != "# Identity\n\nPlain prompt" {
		t.Fatalf("unexpected identity: %q", identity)
	}
}

func TestParseBlock_ToolsAndHooksInFrontmatter(t *testing.T) {
	input := []byte(`---
name: pr-guard
description: PR safety hooks
condition: ctx.get("has_pr") == True
hooks:
  - event: tool.pre
    handler: block_push_main
    tool: git_push
    action: deny
    reason: Use dev_push instead
---

Never push directly to main. Always use the dev-workflow tools.
`)

	block, err := ParseBlock(input, "pr-guard.md")
	if err != nil {
		t.Fatalf("ParseBlock error: %v", err)
	}
	if len(block.Hooks) != 1 {
		t.Fatalf("expected 1 hook, got %d", len(block.Hooks))
	}
	if block.Hooks[0].Tool != "git_push" {
		t.Fatalf("unexpected hook tool: %q", block.Hooks[0].Tool)
	}
	if block.Hooks[0].Action != "deny" {
		t.Fatalf("unexpected hook action: %q", block.Hooks[0].Action)
	}
	if block.Context != "Never push directly to main. Always use the dev-workflow tools." {
		t.Fatalf("unexpected context: %q", block.Context)
	}
}

func writeTestBlock(t *testing.T, path, name, condition, context string, tools []ToolDef, hooks []HookDef) {
	t.Helper()
	content := "---\nname: " + name + "\ndescription: " + name + " description\n"
	if condition != "" {
		content += "condition: " + condition + "\n"
	}
	if len(tools) > 0 {
		content += "tools:\n"
		for _, tool := range tools {
			content += "  - name: " + tool.Name + "\n    description: " + tool.Description + "\n"
		}
	}
	if len(hooks) > 0 {
		content += "hooks:\n"
		for _, hook := range hooks {
			content += "  - event: " + hook.Event + "\n    handler: " + hook.Handler + "\n"
		}
	}
	content += "---\n\n" + context + "\n"
	writeTestFile(t, path, content)
}

func writeTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
