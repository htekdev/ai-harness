package main

import (
	"os"
	"os/exec"
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
