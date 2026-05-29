package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestCLIHelp(t *testing.T) {
	cmd := exec.Command("go", "run", ".", "help")
	cmd.Dir = "."
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("help command failed: %v\n%s", err, out)
	}
	output := string(out)
	if !strings.Contains(output, "Harness as Code CLI") {
		t.Errorf("help output missing header, got: %s", output)
	}
	if !strings.Contains(output, "harness init") {
		t.Errorf("help output missing init example, got: %s", output)
	}
}

func TestCLIVersion(t *testing.T) {
	cmd := exec.Command("go", "run", ".", "version")
	cmd.Dir = "."
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("version command failed: %v\n%s", err, out)
	}
	output := string(out)
	if !strings.Contains(output, "harness") {
		t.Errorf("version output unexpected: %s", output)
	}
}

func TestCLIValidate(t *testing.T) {
	// Set a dummy token so validation passes
	os.Setenv("GH_TOKEN", "test-token")
	defer os.Unsetenv("GH_TOKEN")

	cmd := exec.Command("go", "run", ".", "validate", "-c", "../../harness.md")
	cmd.Dir = "."
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("validate command failed: %v\n%s", err, out)
	}
	output := string(out)
	if !strings.Contains(output, "valid") {
		t.Errorf("validate output unexpected: %s", output)
	}
}

func TestCLIUnknownCommand(t *testing.T) {
	cmd := exec.Command("go", "run", ".", "nonexistent")
	cmd.Dir = "."
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatal("expected error for unknown command")
	}
	output := string(out)
	if !strings.Contains(output, "unknown command") {
		t.Errorf("expected 'unknown command' in output, got: %s", output)
	}
}

func TestCLINoArgs(t *testing.T) {
	cmd := exec.Command("go", "run", ".")
	cmd.Dir = "."
	_, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatal("expected error when no args provided")
	}
}

func TestCLIHooksVerboseShowsSource(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "harness.md")
	harnessContent := `---
model:
  name: gpt-4o
  provider: copilot
  api_key_env: GH_TOKEN
  max_tokens: 4096
  temperature: 0.7
hooks:
  - event: tool.pre
    handler: inline_hook
    script: |
      def handle(event, payload):
          return allow()
---

# Test Harness
`
	if err := os.WriteFile(configPath, []byte(harnessContent), 0o644); err != nil {
		t.Fatal(err)
	}

	extDir := filepath.Join(dir, ".harness", "extensions")
	if err := os.MkdirAll(extDir, 0o755); err != nil {
		t.Fatal(err)
	}
	extContent := `---
name: corp-extension
type: extension
description: corporate extension
hooks:
  - event: tool.pre
    handler: ext_hook
    script: |
      def handle(event, payload):
          return allow()
---

Extension context.
`
	if err := os.WriteFile(filepath.Join(extDir, "corp-extension.md"), []byte(extContent), 0o644); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command("go", "run", ".", "hooks", "-c", configPath, "--verbose")
	cmd.Dir = "."
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("hooks command failed: %v\n%s", err, out)
	}
	output := string(out)
	if !strings.Contains(output, "Source:") {
		t.Fatalf("expected verbose hooks output to include source, got:\n%s", output)
	}
	if !strings.Contains(output, "corp-extension.md") {
		t.Fatalf("expected extension hook source in output, got:\n%s", output)
	}
}
