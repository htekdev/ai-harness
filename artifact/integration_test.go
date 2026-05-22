package artifact_test

import (
	"path/filepath"
	"runtime"
	"testing"

	"github.com/htekdev/ai-harness/artifact"
)

// TestIntegrationLoadTree verifies the full artifact loading pipeline
// against the testdata directory.
func TestIntegrationLoadTree(t *testing.T) {
	_, thisFile, _, _ := runtime.Caller(0)
	testdataDir := filepath.Join(filepath.Dir(thisFile), "testdata")

	reg, err := artifact.LoadAndRegister(testdataDir)
	if err != nil {
		t.Fatalf("LoadAndRegister testdata: %v", err)
	}

	// Should find: identity.md (harness), core-tools (builtin), git-ops (plugin),
	// openai-models (model), strict-mode (override)
	if reg.Count() != 5 {
		t.Errorf("expected 5 artifacts, got %d", reg.Count())
	}

	// Verify ordering: model(20) < plugin(40) < builtin(60) < harness(80) < override(100)
	all := reg.All()
	expectedOrder := []artifact.Type{
		artifact.TypeModel,
		artifact.TypePlugin,
		artifact.TypeBuiltin,
		artifact.TypeHarness,
		artifact.TypeOverride,
	}
	for i, a := range all {
		if a.Metadata.Type != expectedOrder[i] {
			t.Errorf("position %d: expected type %s, got %s (%s)",
				i, expectedOrder[i], a.Metadata.Type, a.Metadata.Name)
		}
	}

	// Verify dependency validation passes (git-ops depends on core-tools)
	if err := reg.ValidateDependencies(); err != nil {
		t.Errorf("dependencies should resolve: %v", err)
	}

	// Verify composition
	composer := artifact.NewComposer(reg)
	result, err := composer.Compose(nil)
	if err != nil {
		t.Fatalf("Compose: %v", err)
	}

	if result.Identity == "" {
		t.Error("expected non-empty identity from harness artifact")
	}
	if len(result.Tools) < 3 { // exec, read-file, git-status, git-diff (exec overridden by strict-mode)
		t.Errorf("expected at least 3 tools, got %d", len(result.Tools))
	}
	if len(result.Models) != 2 {
		t.Errorf("expected 2 models, got %d", len(result.Models))
	}
	if len(result.ActiveArtifacts) != 5 {
		t.Errorf("expected 5 active artifacts, got %d", len(result.ActiveArtifacts))
	}
}
