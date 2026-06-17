package config

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadDirectory_NoHarnessDir(t *testing.T) {
	dir := t.TempDir()
	result, err := LoadDirectory(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Config.Tools) != 0 {
		t.Errorf("expected no tools, got %d", len(result.Config.Tools))
	}
	if len(result.Agents) != 0 {
		t.Errorf("expected no agents, got %d", len(result.Agents))
	}
}

func TestLoadDirectory_WithTools(t *testing.T) {
	dir := t.TempDir()
	toolsDir := filepath.Join(dir, ".harness", "tools")
	if err := os.MkdirAll(toolsDir, 0o755); err != nil {
		t.Fatal(err)
	}

	toolContent := `---
parameters:
  path: { type: string, required: true }
script: |
  def run(args):
      return fs.read(args["path"])
---

Read a file from the workspace.
`
	if err := os.WriteFile(filepath.Join(toolsDir, "read_file.md"), []byte(toolContent), 0o644); err != nil {
		t.Fatal(err)
	}

	result, err := LoadDirectory(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.Config.Tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(result.Config.Tools))
	}

	tool := result.Config.Tools[0]
	if tool.Name != "read_file" {
		t.Errorf("expected tool name 'read_file', got %q", tool.Name)
	}
	if tool.Parameters["path"].Type != "string" {
		t.Errorf("expected path param type string")
	}
}

func TestLoadDirectory_WithHooks(t *testing.T) {
	dir := t.TempDir()
	hooksDir := filepath.Join(dir, ".harness", "hooks")
	if err := os.MkdirAll(hooksDir, 0o755); err != nil {
		t.Fatal(err)
	}

	hookContent := `---
event: tool.pre
priority: 1
script: |
  def handle(event, payload):
      return allow()
---

Guards against bad paths.
`
	if err := os.WriteFile(filepath.Join(hooksDir, "path_guard.md"), []byte(hookContent), 0o644); err != nil {
		t.Fatal(err)
	}

	result, err := LoadDirectory(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.Config.Hooks) != 1 {
		t.Fatalf("expected 1 hook, got %d", len(result.Config.Hooks))
	}

	hook := result.Config.Hooks[0]
	if hook.Handler != "path_guard" {
		t.Errorf("expected handler 'path_guard', got %q", hook.Handler)
	}
	if hook.Event != "tool.pre" {
		t.Errorf("expected event 'tool.pre', got %q", hook.Event)
	}
}

func TestLoadDirectory_WithAgents(t *testing.T) {
	dir := t.TempDir()
	agentsDir := filepath.Join(dir, ".harness", "agents")
	if err := os.MkdirAll(agentsDir, 0o755); err != nil {
		t.Fatal(err)
	}

	agentContent := `---
model: gpt-4o
description: Writes code

tools:
  - read_file
  - name: run_tests
    description: Run tests
    parameters: {}
    script: |
      def run(args):
          return "ok"
---

# Code Writer

You write great code.
`
	if err := os.WriteFile(filepath.Join(agentsDir, "code-writer.md"), []byte(agentContent), 0o644); err != nil {
		t.Fatal(err)
	}

	result, err := LoadDirectory(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.Agents) != 1 {
		t.Fatalf("expected 1 agent, got %d", len(result.Agents))
	}

	agent, ok := result.Agents["code-writer"]
	if !ok {
		t.Fatal("expected agent 'code-writer' in registry")
	}

	if agent.Model != "gpt-4o" {
		t.Errorf("expected model 'gpt-4o', got %q", agent.Model)
	}

	if agent.SystemPrompt != "# Code Writer\n\nYou write great code." {
		t.Errorf("unexpected system prompt: %q", agent.SystemPrompt)
	}

	if len(agent.Tools) != 2 {
		t.Fatalf("expected 2 tools, got %d", len(agent.Tools))
	}
}

func TestMergeDirectoryResult_Additive(t *testing.T) {
	cfg := &Config{
		Tools: []ToolConfig{
			{Name: "inline_tool", Description: "inline", Script: "def run(args): return 'inline'"},
		},
		Hooks: []HookConfig{
			{Event: "tool.pre", Handler: "inline_hook", Script: "def handle(e,p): return allow()"},
		},
	}

	dirResult := &LoadResult{
		Config: &Config{
			Tools: []ToolConfig{
				{Name: "file_tool", Description: "from file", Script: "def run(args): return 'file'"},
			},
			Hooks: []HookConfig{
				{Event: "tool.post", Handler: "file_hook", Script: "def handle(e,p): return allow()"},
			},
		},
	}

	MergeDirectoryResult(cfg, dirResult)

	if len(cfg.Tools) != 2 {
		t.Fatalf("expected 2 tools after merge, got %d", len(cfg.Tools))
	}
	if len(cfg.Hooks) != 2 {
		t.Fatalf("expected 2 hooks after merge, got %d", len(cfg.Hooks))
	}
}

func TestMergeDirectoryResult_FileOverridesInline(t *testing.T) {
	cfg := &Config{
		Tools: []ToolConfig{
			{Name: "my_tool", Description: "inline version", Script: "inline"},
		},
	}

	dirResult := &LoadResult{
		Config: &Config{
			Tools: []ToolConfig{
				{Name: "my_tool", Description: "file version", Script: "file"},
			},
		},
	}

	MergeDirectoryResult(cfg, dirResult)

	if len(cfg.Tools) != 1 {
		t.Fatalf("expected 1 tool (override), got %d", len(cfg.Tools))
	}
	if cfg.Tools[0].Description != "file version" {
		t.Errorf("expected file version to override, got %q", cfg.Tools[0].Description)
	}
}

func TestLoadFull_Integration(t *testing.T) {
	dir := t.TempDir()

	// Create harness.md
	harnessContent := `---
model:
  name: gpt-4o
  provider: copilot
  api_key_env: GH_TOKEN
  max_tokens: 4096
  temperature: 0.7

tools:
  - name: inline_tool
    description: An inline tool
    parameters: {}
    script: |
      def run(args):
          return "inline"
---

# Test Harness

You are a test agent.
`
	if err := os.WriteFile(filepath.Join(dir, "harness.md"), []byte(harnessContent), 0o644); err != nil {
		t.Fatal(err)
	}

	// Create .harness/tools/
	toolsDir := filepath.Join(dir, ".harness", "tools")
	if err := os.MkdirAll(toolsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	toolContent := `---
parameters:
  path: { type: string, required: true }
script: |
  def run(args):
      return fs.read(args["path"])
---

Read a file.
`
	if err := os.WriteFile(filepath.Join(toolsDir, "read_file.md"), []byte(toolContent), 0o644); err != nil {
		t.Fatal(err)
	}

	// Create .harness/agents/
	agentsDir := filepath.Join(dir, ".harness", "agents")
	if err := os.MkdirAll(agentsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	agentContent := `---
model: gpt-4o-mini
description: Research agent
tools: []
hooks: []
---

# Researcher

You research things.
`
	if err := os.WriteFile(filepath.Join(agentsDir, "researcher.md"), []byte(agentContent), 0o644); err != nil {
		t.Fatal(err)
	}

	// Load full
	cfg, agents, err := LoadFull(filepath.Join(dir, "harness.md"))
	if err != nil {
		t.Fatalf("LoadFull error: %v", err)
	}

	// Verify inline + file tools merged
	if len(cfg.Tools) != 2 {
		t.Fatalf("expected 2 tools (inline + file), got %d", len(cfg.Tools))
	}

	// Verify system prompt from markdown body
	if cfg.Context.SystemPrompt != "# Test Harness\n\nYou are a test agent." {
		t.Errorf("unexpected system prompt: %q", cfg.Context.SystemPrompt)
	}

	// Verify agent loaded
	if len(agents) != 1 {
		t.Fatalf("expected 1 agent, got %d", len(agents))
	}
	if _, ok := agents["researcher"]; !ok {
		t.Error("expected 'researcher' agent")
	}
}

func TestLoadFull_WithGitArtifactSource(t *testing.T) {
	projectDir := t.TempDir()
	repoDir := filepath.Join(t.TempDir(), "plugin-repo")
	if err := os.MkdirAll(filepath.Join(repoDir, "plugins"), 0o755); err != nil {
		t.Fatal(err)
	}
	plugin := `---
name: production
type: plugin
tools:
  - name: prod_guard
    description: guard
    parameters: {}
    script: |
      def run(args):
          return "ok"
---

plugin body
`
	if err := os.WriteFile(filepath.Join(repoDir, "plugins", "production.md"), []byte(plugin), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"init"},
		{"config", "user.email", "test@example.com"},
		{"config", "user.name", "Test"},
		{"add", "."},
		{"commit", "-m", "init"},
		{"tag", "v1.0.0"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = repoDir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v (%s)", args, err, strings.TrimSpace(string(out)))
		}
	}

	harness := `---
model:
  name: gpt-4o
  max_tokens: 1024
  temperature: 0.1
trusted_sources:
  - ` + repoDir + `
artifact_sources:
  - type: git
    url: ` + repoDir + `
    ref: v1.0.0
    path: plugins
---

# Test
`
	configPath := filepath.Join(projectDir, "harness.md")
	if err := os.WriteFile(configPath, []byte(harness), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, _, err := LoadFull(configPath)
	if err != nil {
		t.Fatalf("LoadFull error: %v", err)
	}
	found := false
	for _, tool := range cfg.Tools {
		if tool.Name == "prod_guard" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected tool from git artifact source; tools=%v", cfg.Tools)
	}
}
