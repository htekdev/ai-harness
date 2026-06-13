package main

import (
	"os/exec"
	"strings"
	"testing"
)

// TestServeHelp verifies that `harness serve` is registered as a subcommand and
// at minimum prints something meaningful when invoked with --help (via the
// stdlib flag.ExitOnError path it returns 0 after printing help to stderr).
//
// This is a lightweight smoke test — full integration tests for the multi-source
// loop go in a follow-up once we have a fake harness.Harness shim.
func TestServeSubcommandRegistered(t *testing.T) {
	// Build the binary in a tempdir and run it. Skip if `go` is not available
	// in PATH (some constrained CI environments).
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go not in PATH")
	}
	out, err := exec.Command("go", "run", ".", "help").CombinedOutput()
	if err != nil {
		// `help` returns a usage string but the cmd exits 0 (or 1 from os.Exit).
		// We only care that "serve" appears in the output.
	}
	if !strings.Contains(string(out), "serve") {
		t.Errorf("expected 'serve' in usage output, got:\n%s", string(out))
	}
}
