package evals

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/htekdev/ai-harness/harness/errs"
)

// Phase 5.3 PR-C: evals must surface KindConfig for cases I/O and parse
// failures so retry policies and dashboards can distinguish "user gave us
// a bad cases dir" from a runtime / completion failure.

func TestEvalsErrorKind_LoadCaseMissingFile(t *testing.T) {
	t.Parallel()
	_, err := LoadCase(filepath.Join("testdata", "does-not-exist.yaml"))
	if err == nil {
		t.Fatal("expected error for missing file")
	}
	if errs.KindOf(err) != errs.KindConfig {
		t.Fatalf("expected KindConfig, got %s (err=%v)", errs.KindOf(err), err)
	}
}

func TestEvalsErrorKind_LoadCaseBadYAML(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	bad := filepath.Join(tmp, "bad.yaml")
	if err := os.WriteFile(bad, []byte("not: [valid: yaml"), 0o600); err != nil {
		t.Fatalf("write tmp: %v", err)
	}
	_, err := LoadCase(bad)
	if err == nil {
		t.Fatal("expected parse error")
	}
	if errs.KindOf(err) != errs.KindConfig {
		t.Fatalf("expected KindConfig, got %s (err=%v)", errs.KindOf(err), err)
	}
}

func TestEvalsErrorKind_LoadCasesMissingDir(t *testing.T) {
	t.Parallel()
	_, err := LoadCases(filepath.Join("testdata", "does-not-exist-dir"))
	if err == nil {
		t.Fatal("expected error for missing dir")
	}
	if errs.KindOf(err) != errs.KindConfig {
		t.Fatalf("expected KindConfig, got %s (err=%v)", errs.KindOf(err), err)
	}
}
