package compose

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestLoadPolicy_Parsing(t *testing.T) {
	baseDir := t.TempDir()
	writeTestFile(t, filepath.Join(baseDir, ".harness", "harness.yaml"), `model:
  name: claude-sonnet-4.5
  provider: anthropic
  max_tokens: 8192
  temperature: 0.2
  api_key_env: ANTHROPIC_API_KEY
delegation:
  max_depth: 4
  max_concurrent: 7
  iterations_per_depth: [12, 6, 3]
context:
  max_history: 25
  max_tokens: 64000
meta:
  enabled: true
  max_tools: 12
  max_hooks: 8
  max_agents: 4
  max_call_depth: 3
`)

	policy, err := LoadPolicy(baseDir)
	if err != nil {
		t.Fatalf("LoadPolicy error: %v", err)
	}

	if policy.Model.Name != "claude-sonnet-4.5" || policy.Context.MaxHistory != 25 || policy.Delegation.MaxConcurrent != 7 {
		t.Fatalf("unexpected policy: %#v", policy)
	}
}

func TestLoadPolicy_Defaults(t *testing.T) {
	baseDir := t.TempDir()
	policy, err := LoadPolicy(baseDir)
	if err != nil {
		t.Fatalf("LoadPolicy error: %v", err)
	}

	defaults := DefaultPolicy()
	if !reflect.DeepEqual(policy, defaults) {
		t.Fatalf("expected defaults %#v, got %#v", defaults, policy)
	}
}

func TestLoadPolicy_Validation(t *testing.T) {
	baseDir := t.TempDir()
	writeTestFile(t, filepath.Join(baseDir, ".harness", "harness.yaml"), `model:
  name: bad
  provider: openai
  max_tokens: 0
  temperature: 4
  api_key_env: TOKEN
`)

	_, err := LoadPolicy(baseDir)
	if err == nil {
		t.Fatal("expected validation error")
	}
	if !strings.Contains(err.Error(), "invalid policy") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestResolvePolicy_AgentOverrides(t *testing.T) {
	baseDir := t.TempDir()
	writeTestFile(t, filepath.Join(baseDir, ".harness", "harness.yaml"), `model:
  name: gpt-4o
  provider: openai
  max_tokens: 4096
  temperature: 0.6
  api_key_env: BASE_TOKEN
context:
  max_history: 40
  max_tokens: 128000
meta:
  enabled: true
  max_tools: 20
  max_hooks: 10
  max_agents: 5
  max_call_depth: 4
`)
	writeTestFile(t, filepath.Join(baseDir, ".harness", "agents", "coder", "agent.yaml"), `model:
  name: gpt-5-mini
context:
  max_history: 10
meta:
  max_tools: 5
`)

	policy, err := ResolvePolicy(baseDir, "coder")
	if err != nil {
		t.Fatalf("ResolvePolicy error: %v", err)
	}

	if policy.Model.Name != "gpt-5-mini" {
		t.Fatalf("expected overridden model name, got %q", policy.Model.Name)
	}
	if policy.Model.APIKeyEnv != "BASE_TOKEN" {
		t.Fatalf("expected base api_key_env to be preserved, got %q", policy.Model.APIKeyEnv)
	}
	if policy.Context.MaxHistory != 10 || policy.Context.MaxTokens != 128000 {
		t.Fatalf("unexpected context policy: %#v", policy.Context)
	}
	if policy.Meta.MaxTools != 5 || policy.Meta.MaxHooks != 10 {
		t.Fatalf("unexpected meta policy: %#v", policy.Meta)
	}
}

func TestDefaultPolicy_IndependentSlices(t *testing.T) {
	first := DefaultPolicy()
	second := DefaultPolicy()
	first.Delegation.IterationsPerDepth[0] = 99
	if second.Delegation.IterationsPerDepth[0] == 99 {
		t.Fatal("expected default iterations slice to be copied")
	}
}

func TestLoadPolicy_DefaultsUsesExistingHarnessDir(t *testing.T) {
	baseDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(baseDir, ".harness"), 0o755); err != nil {
		t.Fatal(err)
	}
	policy, err := LoadPolicy(baseDir)
	if err != nil {
		t.Fatalf("LoadPolicy error: %v", err)
	}
	if policy.Model.Name != DefaultPolicy().Model.Name {
		t.Fatalf("unexpected default policy: %#v", policy)
	}
}
