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

	writeTestFile(t, filepath.Join(harnessDir, "identity.md"), "# Base Identity")
	writeTestBlock(t, filepath.Join(harnessDir, "alpha.md"), "alpha", "", "Alpha context", "", "")
	writeTestBlock(t, filepath.Join(harnessDir, "beta.md"), "beta", "", "Beta context", "", "")
	writeTestBlock(t, filepath.Join(harnessDir, "agents", "coder", "testing.md"), "testing", "", "Testing context", "", "")

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
		"---\n\n" +
		"Use the git workflow carefully.\n\n" +
		"## Tools\n" +
		"```yaml\n" +
		"- name: run_tests\n" +
		"  description: Run tests\n" +
		"  parameters:\n" +
		"    package:\n" +
		"      type: string\n" +
		"      required: true\n" +
		"  timeout_ms: 5000\n" +
		"```\n\n" +
		"## Hooks\n" +
		"```yaml\n" +
		"- event: tool.pre\n" +
		"  handler: guard\n" +
		"  priority: 1\n" +
		"```\n")

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
	if len(block.Hooks) != 1 || block.Hooks[0].Handler != "guard" {
		t.Fatalf("unexpected hooks: %#v", block.Hooks)
	}
}

func TestParseBlock_MalformedFileHandling(t *testing.T) {
	_, err := ParseBlock([]byte("---\nname: broken\n"), "broken.md")
	if err == nil {
		t.Fatal("expected missing closing frontmatter delimiter error")
	}

	_, err = ParseBlock([]byte(`---
name: broken
---

## Tools
not fenced
`), "broken.md")
	if err == nil {
		t.Fatal("expected malformed tools section error")
	}
}

func TestLoadIdentity(t *testing.T) {
	baseDir := t.TempDir()
	writeTestFile(t, filepath.Join(baseDir, ".harness", "identity.md"), "# Identity\n\nBase prompt")

	identity, err := LoadIdentity(baseDir)
	if err != nil {
		t.Fatalf("LoadIdentity error: %v", err)
	}
	if identity != "# Identity\n\nBase prompt" {
		t.Fatalf("unexpected identity: %q", identity)
	}
}

func writeTestBlock(t *testing.T, path, name, condition, context, toolsSection, hooksSection string) {
	t.Helper()
	content := "---\nname: " + name + "\ndescription: " + name + " description\n"
	if condition != "" {
		content += "condition: " + condition + "\n"
	}
	content += "---\n\n" + context + "\n"
	if toolsSection != "" {
		content += "\n## Tools\n```yaml\n" + toolsSection + "\n```\n"
	}
	if hooksSection != "" {
		content += "\n## Hooks\n```yaml\n" + hooksSection + "\n```\n"
	}
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
