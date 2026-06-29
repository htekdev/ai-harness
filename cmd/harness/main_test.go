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
	// Golden path journey must be visible in help
	for _, want := range []string{"scaffold", "validate", "deploy", "inspect"} {
		if !strings.Contains(output, want) {
			t.Errorf("help output missing %q command, got: %s", want, output)
		}
	}
	if !strings.Contains(output, "Golden path") {
		t.Errorf("help output missing golden path section, got: %s", output)
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

func TestCLIScaffold(t *testing.T) {
	tmp := t.TempDir()
	projectName := "test-harness-project"
	projectDir := filepath.Join(tmp, projectName)

	cmd := exec.Command("go", "run", ".", "scaffold", projectDir)
	cmd.Dir = "."
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("scaffold command failed: %v\n%s", err, out)
	}
	output := string(out)
	if !strings.Contains(output, "Scaffolded") {
		t.Errorf("scaffold output missing confirmation, got: %s", output)
	}

	// Check that the expected files were created
	for _, path := range []string{
		"harness.md",
		".harness/tools/read_file.md",
		".harness/tools/write_file.md",
		".harness/hooks/safety.md",
	} {
		if _, err := os.Stat(filepath.Join(projectDir, path)); err != nil {
			t.Errorf("scaffold missing expected file %s: %v", path, err)
		}
	}
}

func TestCLIScaffoldExistingDir(t *testing.T) {
	tmp := t.TempDir()

	cmd := exec.Command("go", "run", ".", "scaffold", tmp)
	cmd.Dir = "."
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatal("expected error when scaffolding into an existing directory")
	}
	output := string(out)
	if !strings.Contains(output, "already exists") {
		t.Errorf("expected 'already exists' in output, got: %s", output)
	}
}

func TestCLIScaffoldNoName(t *testing.T) {
	cmd := exec.Command("go", "run", ".", "scaffold")
	cmd.Dir = "."
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatal("expected error when scaffold has no name argument")
	}
	output := string(out)
	if !strings.Contains(output, "name is required") {
		t.Errorf("expected 'name is required' in output, got: %s", output)
	}
}

func TestCLIDeployDryRun(t *testing.T) {
	os.Setenv("GH_TOKEN", "test-token")
	defer os.Unsetenv("GH_TOKEN")

	cmd := exec.Command("go", "run", ".", "deploy", "--dry-run", "-c", "../../harness.md")
	cmd.Dir = "."
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("deploy --dry-run failed: %v\n%s", err, out)
	}
	output := string(out)
	if !strings.Contains(output, "Dry-run") {
		t.Errorf("deploy --dry-run output missing 'Dry-run', got: %s", output)
	}
	if !strings.Contains(output, "valid") {
		t.Errorf("deploy --dry-run output missing 'valid', got: %s", output)
	}
}

func TestCLIDeployNoInput(t *testing.T) {
	cmd := exec.Command("go", "run", ".", "deploy", "-c", "../../harness.md")
	cmd.Dir = "."
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatal("expected error when deploy has no input")
	}
	output := string(out)
	if !strings.Contains(output, "no input") {
		t.Errorf("expected 'no input' in output, got: %s", output)
	}
}

func TestCLIInspect(t *testing.T) {
	os.Setenv("GH_TOKEN", "test-token")
	defer os.Unsetenv("GH_TOKEN")

	cmd := exec.Command("go", "run", ".", "inspect", "-c", "../../harness.md", "--dir", "../..")
	cmd.Dir = "."
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("inspect command failed: %v\n%s", err, out)
	}
	output := string(out)
	if !strings.Contains(output, "Harness Inspect") {
		t.Errorf("inspect output missing header, got: %s", output)
	}
	if !strings.Contains(output, "Tools") {
		t.Errorf("inspect output missing Tools section, got: %s", output)
	}
	if !strings.Contains(output, "Hooks") {
		t.Errorf("inspect output missing Hooks section, got: %s", output)
	}
	if !strings.Contains(output, "core_identity") {
		t.Errorf("inspect output missing core identity token accounting, got: %s", output)
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

func TestToTitleCase(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"my-project", "My Project"},
		{"my_project", "My Project"},
		{"hello", "Hello"},
		{"multi-word-name", "Multi Word Name"},
	}
	for _, c := range cases {
		got := toTitleCase(c.input)
		if got != c.want {
			t.Errorf("toTitleCase(%q) = %q, want %q", c.input, got, c.want)
		}
	}
}
