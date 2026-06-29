package main

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type contextSection struct {
	Name string `json:"name"`
}

func TestCmdContextAgentEvaluatesConditions(t *testing.T) {
	tmp := t.TempDir()
	writeContextFile(t, filepath.Join(tmp, ".harness", "base.md"), `---
name: base
type: harness
---
You are helpful.
`)
	writeContextFile(t, filepath.Join(tmp, ".harness", "plugins", "planner.md"), `---
name: planner-context
type: plugin
description: Planner-only context
condition: ctx.get("agent.name") == "planner"
---
Plan first.
`)

	raw := captureContextStdout(t, func() {
		if err := cmdContext([]string{"--dir", tmp, "--json"}); err != nil {
			t.Fatalf("cmdContext: %v", err)
		}
	})
	var withoutAgent struct {
		Sections []contextSection `json:"sections"`
	}
	if err := json.Unmarshal([]byte(raw), &withoutAgent); err != nil {
		t.Fatalf("unmarshal without agent: %v\n%s", err, raw)
	}
	if hasSection(withoutAgent.Sections, "planner-context") {
		t.Fatalf("planner-context section should be inactive without --agent:\n%s", raw)
	}

	raw = captureContextStdout(t, func() {
		if err := cmdContext([]string{"--dir", tmp, "--json", "--agent", "planner"}); err != nil {
			t.Fatalf("cmdContext --agent planner: %v", err)
		}
	})
	var withAgent struct {
		Sections []contextSection `json:"sections"`
	}
	if err := json.Unmarshal([]byte(raw), &withAgent); err != nil {
		t.Fatalf("unmarshal with agent: %v\n%s", err, raw)
	}
	if !hasSection(withAgent.Sections, "planner-context") {
		t.Fatalf("planner-context section should be active with --agent planner:\n%s", raw)
	}
}

func captureContextStdout(t *testing.T, fn func()) string {
	t.Helper()
	orig := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	defer func() {
		_ = r.Close()
		os.Stdout = orig
	}()
	os.Stdout = w

	fn()

	_ = w.Close()
	b, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("read captured stdout: %v", err)
	}
	return string(b)
}

func writeContextFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", path, err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func hasSection(sections []contextSection, name string) bool {
	for _, s := range sections {
		if s.Name == name {
			return true
		}
	}
	return false
}

// --- context list subcommand tests -----------------------------------

// minimalIdentity returns a minimal valid identity.md frontmatter body.
// The caller may append context.sources before the closing ---.
func minimalIdentityWithSources(sources string) string {
	return `---
model:
  name: gpt-4o
  provider: openai
  max_tokens: 4096
  temperature: 0.7
  api_key_env: OPENAI_API_KEY
context:
  max_history: 50
  max_tokens: 128000
` + sources + `delegation:
  max_depth: 3
  max_concurrent: 5
  iterations_per_depth: [20, 10, 5, 3]
meta:
  max_tools: 50
  max_hooks: 30
  max_agents: 10
  max_call_depth: 5
---
`
}

func TestCmdContextList_NoSources(t *testing.T) {
	tmp := t.TempDir()

	writeContextFile(t, filepath.Join(tmp, ".harness", "identity.md"),
		minimalIdentityWithSources(""))

	out := captureContextStdout(t, func() {
		if err := cmdContext([]string{"list", "--dir", tmp}); err != nil {
			t.Fatalf("cmdContext list: %v", err)
		}
	})
	if !strings.Contains(out, "No context sources configured") {
		t.Errorf("expected 'No context sources configured', got:\n%s", out)
	}
}

func TestCmdContextList_ShowsInactiveSource(t *testing.T) {
	tmp := t.TempDir()

	writeContextFile(t, filepath.Join(tmp, ".harness", "context", "pr.md"), "# PR Rules\nReview carefully.")
	writeContextFile(t, filepath.Join(tmp, ".harness", "identity.md"),
		minimalIdentityWithSources(`  sources:
    - name: pr-workflow
      type: file
      path: ".harness/context/pr.md"
      when: 'ctx.get("mode") == "pull_request"'
`))

	// Without --ctx mode=pull_request the source should be inactive.
	out := captureContextStdout(t, func() {
		if err := cmdContext([]string{"list", "--dir", tmp}); err != nil {
			t.Fatalf("cmdContext list: %v", err)
		}
	})
	if !strings.Contains(out, "pr-workflow") {
		t.Errorf("expected pr-workflow in output, got:\n%s", out)
	}
	if !strings.Contains(out, "INACTIVE") {
		t.Errorf("expected INACTIVE section, got:\n%s", out)
	}
}

func TestCmdContextList_ShowsActiveSource(t *testing.T) {
	tmp := t.TempDir()

	writeContextFile(t, filepath.Join(tmp, ".harness", "context", "pr.md"), "# PR Rules\nReview carefully.")
	writeContextFile(t, filepath.Join(tmp, ".harness", "identity.md"),
		minimalIdentityWithSources(`  sources:
    - name: pr-workflow
      type: file
      path: ".harness/context/pr.md"
      when: 'ctx.get("mode") == "pull_request"'
`))

	// With --ctx mode=pull_request the source should be active.
	out := captureContextStdout(t, func() {
		if err := cmdContext([]string{"list", "--dir", tmp, "--ctx", "mode=pull_request"}); err != nil {
			t.Fatalf("cmdContext list --ctx: %v", err)
		}
	})
	if !strings.Contains(out, "ACTIVE") {
		t.Errorf("expected ACTIVE section, got:\n%s", out)
	}
	if !strings.Contains(out, "pr-workflow") {
		t.Errorf("expected pr-workflow in output, got:\n%s", out)
	}
}

func TestCmdContextList_ActiveOnly(t *testing.T) {
	tmp := t.TempDir()

	writeContextFile(t, filepath.Join(tmp, ".harness", "context", "always.md"), "Always here.")
	writeContextFile(t, filepath.Join(tmp, ".harness", "context", "pr.md"), "PR rules.")
	writeContextFile(t, filepath.Join(tmp, ".harness", "identity.md"),
		minimalIdentityWithSources(`  sources:
    - name: always
      type: file
      path: ".harness/context/always.md"
    - name: pr-workflow
      type: file
      path: ".harness/context/pr.md"
      when: 'ctx.get("mode") == "pull_request"'
`))

	out := captureContextStdout(t, func() {
		if err := cmdContext([]string{"list", "--dir", tmp, "--active"}); err != nil {
			t.Fatalf("cmdContext list --active: %v", err)
		}
	})
	if strings.Contains(out, "INACTIVE") {
		t.Errorf("--active flag should suppress INACTIVE section, got:\n%s", out)
	}
	if !strings.Contains(out, "always") {
		t.Errorf("expected always-on source in output, got:\n%s", out)
	}
}
