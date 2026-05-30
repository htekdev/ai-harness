package main

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
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
	}()
	defer func() {
		os.Stdout = orig
	}()
	defer func() {
		_ = w.Close()
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
