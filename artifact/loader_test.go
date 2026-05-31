package artifact

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParse(t *testing.T) {
	input := []byte(`---
name: my-plugin
type: plugin
version: "1.0.0"
description: A test plugin
author: htekdev
tags: [security, auth]
depends_on: [core-tools]
condition: "ctx.get('env') == 'production'"
tools:
  - name: scan
    description: Scans for vulnerabilities
    parameters:
      target:
        type: string
        required: true
        description: Target to scan
    timeout_ms: 30000
hooks:
  - event: onPreToolUse
    handler: deny
    when: "tool.name == 'rm'"
    reason: "Dangerous operation"
---

# Security Plugin

This plugin provides security scanning capabilities.
`)

	a, err := Parse(input, "test/my-plugin.md")
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	if a.Metadata.Name != "my-plugin" {
		t.Errorf("name = %q, want 'my-plugin'", a.Metadata.Name)
	}
	if a.Metadata.Type != TypePlugin {
		t.Errorf("type = %q, want 'plugin'", a.Metadata.Type)
	}
	if a.Metadata.Version != "1.0.0" {
		t.Errorf("version = %q, want '1.0.0'", a.Metadata.Version)
	}
	if a.Metadata.Description != "A test plugin" {
		t.Errorf("description = %q", a.Metadata.Description)
	}
	if a.Metadata.Author != "htekdev" {
		t.Errorf("author = %q", a.Metadata.Author)
	}
	if len(a.Metadata.Tags) != 2 {
		t.Errorf("expected 2 tags, got %d", len(a.Metadata.Tags))
	}
	if len(a.Metadata.DependsOn) != 1 || a.Metadata.DependsOn[0] != "core-tools" {
		t.Errorf("depends_on = %v", a.Metadata.DependsOn)
	}
	if a.Condition != "ctx.get('env') == 'production'" {
		t.Errorf("condition = %q", a.Condition)
	}
	if len(a.Tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(a.Tools))
	}
	if a.Tools[0].Name != "scan" {
		t.Errorf("tool name = %q", a.Tools[0].Name)
	}
	if a.Tools[0].TimeoutMS != 30000 {
		t.Errorf("tool timeout = %d", a.Tools[0].TimeoutMS)
	}
	if len(a.Hooks) != 1 {
		t.Fatalf("expected 1 hook, got %d", len(a.Hooks))
	}
	if a.Hooks[0].Event != "onPreToolUse" {
		t.Errorf("hook event = %q", a.Hooks[0].Event)
	}
	if a.Source != "test/my-plugin.md" {
		t.Errorf("source = %q", a.Source)
	}
	if a.Context == "" {
		t.Error("expected non-empty context (markdown body)")
	}
}

func TestParseModelArtifactCapabilities(t *testing.T) {
	input := []byte(`---
name: openai-models
type: model
description: Declarative model catalog
models:
  - name: gpt-4o
    provider: openai
    max_tokens: 16384
    temperature: 0.2
    api_key_env: OPENAI_API_KEY
    base_url: https://api.openai.com/v1
    capabilities:
      streaming: true
      tool_calling: true
      vision: true
      json_mode: true
---

# OpenAI catalog

Onboard models here instead of changing Go code.
`)

	a, err := Parse(input, "test/openai-models.md")
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	if a.Metadata.Type != TypeModel {
		t.Fatalf("type = %q, want %q", a.Metadata.Type, TypeModel)
	}
	if len(a.Models) != 1 {
		t.Fatalf("expected 1 model, got %d", len(a.Models))
	}
	model := a.Models[0]
	if !model.Capabilities.Streaming || !model.Capabilities.ToolCalling || !model.Capabilities.Vision || !model.Capabilities.JSONMode {
		t.Fatalf("unexpected capabilities: %+v", model.Capabilities)
	}
}

func TestParseInvalidType(t *testing.T) {
	input := []byte(`---
name: bad
type: unknown
---

Content
`)
	_, err := Parse(input, "bad.md")
	if err == nil {
		t.Error("expected error for unknown type")
	}
}

func TestParseNoFrontmatter(t *testing.T) {
	input := []byte(`No frontmatter here.`)
	_, err := Parse(input, "no-fm.md")
	if err == nil {
		t.Error("expected error for missing frontmatter")
	}
}

func TestLoadDir(t *testing.T) {
	dir := t.TempDir()

	// Write a valid artifact file
	content := []byte(`---
name: test-plugin
type: plugin
description: A test plugin for LoadDir
tools:
  - name: greet
    description: Says hello
---

Hello from the test plugin.
`)
	if err := os.WriteFile(filepath.Join(dir, "test-plugin.md"), content, 0644); err != nil {
		t.Fatal(err)
	}

	// Write a non-.md file (should be skipped)
	if err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("skip me"), 0644); err != nil {
		t.Fatal(err)
	}

	artifacts, err := LoadDir(dir)
	if err != nil {
		t.Fatalf("LoadDir failed: %v", err)
	}
	if len(artifacts) != 1 {
		t.Errorf("expected 1 artifact, got %d", len(artifacts))
	}
	if artifacts[0].Metadata.Name != "test-plugin" {
		t.Errorf("name = %q", artifacts[0].Metadata.Name)
	}
}

func TestLoadDirNonexistent(t *testing.T) {
	artifacts, err := LoadDir("/nonexistent/path")
	if err != nil {
		t.Errorf("expected nil error for nonexistent dir, got: %v", err)
	}
	if artifacts != nil {
		t.Errorf("expected nil artifacts, got %d", len(artifacts))
	}
}

func TestLoadTree(t *testing.T) {
	dir := t.TempDir()

	// Create subdirectories
	for _, sub := range []string{"builtins", "plugins", "models"} {
		if err := os.MkdirAll(filepath.Join(dir, sub), 0755); err != nil {
			t.Fatal(err)
		}
	}

	// Write a harness artifact
	harness := []byte(`---
name: my-harness
type: harness
---

You are a helpful assistant.
`)
	if err := os.WriteFile(filepath.Join(dir, "identity.md"), harness, 0644); err != nil {
		t.Fatal(err)
	}

	// Write a builtin
	builtin := []byte(`---
name: core-tools
type: builtin
version: "1.0.0"
tools:
  - name: exec
    description: Execute commands
---

Core execution tools.
`)
	if err := os.WriteFile(filepath.Join(dir, "builtins", "core-tools.md"), builtin, 0644); err != nil {
		t.Fatal(err)
	}

	// Write a plugin
	plugin := []byte(`---
name: my-plugin
type: plugin
description: A custom plugin
tools:
  - name: custom
    description: Custom tool
---

Plugin context.
`)
	if err := os.WriteFile(filepath.Join(dir, "plugins", "my-plugin.md"), plugin, 0644); err != nil {
		t.Fatal(err)
	}

	artifacts, err := LoadTree(dir)
	if err != nil {
		t.Fatalf("LoadTree failed: %v", err)
	}
	if len(artifacts) != 3 {
		t.Errorf("expected 3 artifacts, got %d", len(artifacts))
	}
}

func TestLoadAndRegister(t *testing.T) {
	dir := t.TempDir()

	os.MkdirAll(filepath.Join(dir, "plugins"), 0755)

	harness := []byte(`---
name: test-harness
type: harness
---

Identity prompt.
`)
	if err := os.WriteFile(filepath.Join(dir, "identity.md"), harness, 0644); err != nil {
		t.Fatal(err)
	}

	plugin := []byte(`---
name: my-plugin
type: plugin
description: Test
tools:
  - name: do-thing
    description: Does a thing
---

Plugin body.
`)
	if err := os.WriteFile(filepath.Join(dir, "plugins", "my-plugin.md"), plugin, 0644); err != nil {
		t.Fatal(err)
	}

	reg, err := LoadAndRegister(dir)
	if err != nil {
		t.Fatalf("LoadAndRegister failed: %v", err)
	}
	if reg.Count() != 2 {
		t.Errorf("expected 2 artifacts, got %d", reg.Count())
	}

	// Verify ordering
	all := reg.All()
	if all[0].Metadata.Name != "my-plugin" { // plugin=40 < harness=80
		t.Errorf("expected my-plugin first, got %q", all[0].Metadata.Name)
	}
}
