package config

import (
	"testing"
)

func TestParseMarkdown_Basic(t *testing.T) {
	input := []byte(`---
model:
  name: gpt-4o
  provider: copilot
---

# My Agent

You are helpful.
`)

	doc, err := ParseMarkdown(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !contains(string(doc.Frontmatter), "model:") {
		t.Errorf("frontmatter should contain model config, got: %s", doc.Frontmatter)
	}

	if !contains(doc.Body, "# My Agent") {
		t.Errorf("body should contain markdown heading, got: %s", doc.Body)
	}

	if !contains(doc.Body, "You are helpful.") {
		t.Errorf("body should contain prompt text, got: %s", doc.Body)
	}
}

func TestParseMarkdown_NoBody(t *testing.T) {
	input := []byte(`---
model:
  name: gpt-4o
---`)

	doc, err := ParseMarkdown(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if doc.Body != "" {
		t.Errorf("expected empty body, got: %q", doc.Body)
	}
}

func TestParseMarkdown_NoFrontmatter(t *testing.T) {
	input := []byte(`# Just markdown

No frontmatter here.
`)

	_, err := ParseMarkdown(input)
	if err == nil {
		t.Fatal("expected error for missing frontmatter")
	}
}

func TestParseMarkdown_NoClosingDelimiter(t *testing.T) {
	input := []byte(`---
model:
  name: gpt-4o

No closing delimiter.
`)

	_, err := ParseMarkdown(input)
	if err == nil {
		t.Fatal("expected error for missing closing delimiter")
	}
}

func TestLoadMarkdown_IntegrationConfig(t *testing.T) {
	input := []byte(`---
model:
  name: gpt-4o
  provider: copilot
  api_key_env: GH_TOKEN
  max_tokens: 4096
  temperature: 0.7

tools:
  - name: hello
    description: Say hello
    parameters: {}
    script: |
      def run(args):
          return "hello"
---

# Test Agent

You are a test agent. Be helpful.
`)

	doc, err := ParseMarkdown(input)
	if err != nil {
		t.Fatalf("ParseMarkdown error: %v", err)
	}

	cfg, err := Parse(doc.Frontmatter)
	if err != nil {
		t.Fatalf("Parse frontmatter error: %v", err)
	}

	if cfg.Model.Name != "gpt-4o" {
		t.Errorf("expected model gpt-4o, got %s", cfg.Model.Name)
	}

	if len(cfg.Tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(cfg.Tools))
	}

	if cfg.Tools[0].Name != "hello" {
		t.Errorf("expected tool name 'hello', got %q", cfg.Tools[0].Name)
	}

	if doc.Body != "# Test Agent\n\nYou are a test agent. Be helpful." {
		t.Errorf("unexpected body: %q", doc.Body)
	}
}

func TestParseToolMarkdown(t *testing.T) {
	input := []byte(`---
parameters:
  path: { type: string, required: true }
script: |
  def run(args):
      return fs.read(args["path"])
---

# read_file

Read a file from the workspace and return its contents.
`)

	tool, err := ParseToolMarkdown(input, "read_file")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if tool.Name != "read_file" {
		t.Errorf("expected name 'read_file', got %q", tool.Name)
	}

	if !contains(tool.Description, "Read a file") {
		t.Errorf("expected description from body, got %q", tool.Description)
	}

	if tool.Parameters["path"].Type != "string" {
		t.Errorf("expected path param type string, got %q", tool.Parameters["path"].Type)
	}

	if !contains(tool.Script, "fs.read") {
		t.Errorf("expected script with fs.read, got %q", tool.Script)
	}
}

func TestParseHookMarkdown(t *testing.T) {
	input := []byte(`---
event: tool.pre
priority: 1
when: payload["name"] == "read_file"
script: |
  def handle(event, payload):
      return allow()
---

# path_guard

Blocks path traversal attempts.
`)

	hook, err := ParseHookMarkdown(input, "path_guard")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if hook.Handler != "path_guard" {
		t.Errorf("expected handler 'path_guard', got %q", hook.Handler)
	}

	if hook.Event != "tool.pre" {
		t.Errorf("expected event 'tool.pre', got %q", hook.Event)
	}

	if hook.Priority != 1 {
		t.Errorf("expected priority 1, got %d", hook.Priority)
	}

	if hook.When != `payload["name"] == "read_file"` {
		t.Errorf("unexpected when: %q", hook.When)
	}
}

func TestParseHookMarkdown_MissingEvent(t *testing.T) {
	input := []byte(`---
script: |
  def handle(event, payload):
      return allow()
---

# broken_hook
`)

	_, err := ParseHookMarkdown(input, "broken_hook")
	if err == nil {
		t.Fatal("expected error for missing event")
	}
}

func TestParseAgentMarkdown(t *testing.T) {
	input := []byte(`---
model: gpt-4o
description: Writes Go code

tools:
  - read_file
  - name: run_tests
    description: Run tests
    parameters: {}
    script: |
      def run(args):
          return exec.run("go", ["test", "./..."])

hooks:
  - event: tool.pre
    handler: guard
    script: |
      def handle(event, payload):
          return allow()
---

# Code Writer

You are a senior Go developer.
`)

	agent, err := ParseAgentMarkdown(input, "code-writer")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if agent.Name != "code-writer" {
		t.Errorf("expected name 'code-writer', got %q", agent.Name)
	}

	if agent.Model != "gpt-4o" {
		t.Errorf("expected model 'gpt-4o', got %q", agent.Model)
	}

	if !contains(agent.SystemPrompt, "senior Go developer") {
		t.Errorf("expected system prompt from body, got %q", agent.SystemPrompt)
	}

	if len(agent.Tools) != 2 {
		t.Fatalf("expected 2 tools, got %d", len(agent.Tools))
	}

	// First tool is a reference
	if agent.Tools[0].Ref != "read_file" {
		t.Errorf("expected first tool ref 'read_file', got ref=%q", agent.Tools[0].Ref)
	}

	// Second tool is inline
	if agent.Tools[1].Inline == nil {
		t.Fatal("expected second tool to be inline")
	}
	if agent.Tools[1].Inline.Name != "run_tests" {
		t.Errorf("expected inline tool name 'run_tests', got %q", agent.Tools[1].Inline.Name)
	}

	if len(agent.Hooks) != 1 {
		t.Fatalf("expected 1 hook, got %d", len(agent.Hooks))
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsStr(s, substr))
}

func containsStr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
