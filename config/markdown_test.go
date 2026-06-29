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

func TestLoadMarkdown_DelegationVerifyFrontmatter(t *testing.T) {
	input := []byte(`---
model:
  name: gpt-4o
  provider: copilot
delegation:
  max_depth: 3
  verify: |
    def run(result):
      return json.encode({"verified": True})
  max_verify_retries: 2
  verify_policy:
    on_exhausted: error
---
`)

	doc, err := ParseMarkdown(input)
	if err != nil {
		t.Fatalf("ParseMarkdown error: %v", err)
	}
	cfg, err := Parse(doc.Frontmatter)
	if err != nil {
		t.Fatalf("Parse frontmatter error: %v", err)
	}
	if !contains(cfg.Delegation.Verify, `"verified": True`) {
		t.Fatalf("expected delegation.verify to be parsed, got %q", cfg.Delegation.Verify)
	}
	if cfg.Delegation.MaxVerifyRetries != 2 {
		t.Fatalf("expected max_verify_retries=2, got %d", cfg.Delegation.MaxVerifyRetries)
	}
	if cfg.Delegation.VerifyPolicy == nil {
		t.Fatal("expected delegation.verify_policy to be parsed")
	}
	if cfg.Delegation.VerifyPolicy.OnExhausted != "error" {
		t.Fatalf("expected delegation.verify_policy.on_exhausted=error, got %q", cfg.Delegation.VerifyPolicy.OnExhausted)
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

func TestParseToolMarkdown_WithVerify(t *testing.T) {
	input := []byte(`---
parameters:
  owner: { type: string, required: true }
  name: { type: string, required: true }
script: |
  def run(args):
      return "ok"
verify: |
  def run(result):
      return json.encode({"verified": True})
verify_policy:
  max_retries: 3
  on_exhausted: error
  timeout_per_attempt: 30s
---
`)

	tool, err := ParseToolMarkdown(input, "create_repo")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !contains(tool.Verify, `"verified": True`) {
		t.Fatalf("expected verify script to be parsed, got %q", tool.Verify)
	}
	if tool.VerifyPolicy == nil {
		t.Fatal("expected verify_policy to be parsed")
	}
	if tool.VerifyPolicy.MaxRetries != 3 {
		t.Fatalf("expected max_retries=3, got %d", tool.VerifyPolicy.MaxRetries)
	}
	if tool.VerifyPolicy.OnExhausted != "error" {
		t.Fatalf("expected on_exhausted=error, got %q", tool.VerifyPolicy.OnExhausted)
	}
	if tool.VerifyPolicy.TimeoutPerAttempt != "30s" {
		t.Fatalf("expected timeout_per_attempt=30s, got %q", tool.VerifyPolicy.TimeoutPerAttempt)
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

func TestParseHookMarkdown_WithVerify(t *testing.T) {
	input := []byte(`---
event: delegation.post_verify
script: |
  def handle(event, payload):
      return allow()
verify: |
  def run(result):
      return json.encode({"verified": True})
verify_policy:
  max_retries: 2
---
`)

	hook, err := ParseHookMarkdown(input, "verify_claims")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !contains(hook.Verify, `"verified": True`) {
		t.Fatalf("expected verify script to be parsed, got %q", hook.Verify)
	}
	if hook.VerifyPolicy == nil {
		t.Fatal("expected verify_policy to be parsed")
	}
	if hook.VerifyPolicy.MaxRetries != 2 {
		t.Fatalf("expected max_retries=2, got %d", hook.VerifyPolicy.MaxRetries)
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
