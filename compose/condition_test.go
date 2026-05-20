package compose

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEvaluateCondition_TrueAndFalse(t *testing.T) {
	ctx := ConditionContext{Values: map[string]interface{}{"mode": "pull_request"}}

	ok, err := EvaluateCondition(`ctx.get("mode") == "pull_request"`, ctx)
	if err != nil {
		t.Fatalf("EvaluateCondition error: %v", err)
	}
	if !ok {
		t.Fatal("expected condition to be true")
	}

	ok, err = EvaluateCondition(`ctx.get("mode") == "chat"`, ctx)
	if err != nil {
		t.Fatalf("EvaluateCondition error: %v", err)
	}
	if ok {
		t.Fatal("expected condition to be false")
	}
}

func TestEvaluateCondition_CtxEnvTimeAndFS(t *testing.T) {
	baseDir := t.TempDir()
	flagPath := filepath.Join(baseDir, "flag.txt")
	if err := os.WriteFile(flagPath, []byte("ok"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("COMPOSE_TEST_ENV", "present")

	ctx := ConditionContext{
		Values:  map[string]interface{}{"count": 2},
		BaseDir: baseDir,
	}

	expr := `ctx.get("count") == 2 and ctx.get("missing", "fallback") == "fallback" and env("COMPOSE_TEST_ENV") == "present" and fs.exists("flag.txt") and time.now() != ""`
	ok, err := EvaluateCondition(expr, ctx)
	if err != nil {
		t.Fatalf("EvaluateCondition error: %v", err)
	}
	if !ok {
		t.Fatal("expected condition to be true")
	}
}

func TestEvaluateCondition_EmptyConditionIsTrue(t *testing.T) {
	ok, err := EvaluateCondition("", ConditionContext{})
	if err != nil {
		t.Fatalf("EvaluateCondition error: %v", err)
	}
	if !ok {
		t.Fatal("expected empty condition to be true")
	}
}

func TestEvaluateCondition_SyntaxErrors(t *testing.T) {
	_, err := EvaluateCondition(`ctx.get("mode") ==`, ConditionContext{})
	if err == nil {
		t.Fatal("expected syntax error")
	}
	if !strings.Contains(err.Error(), "evaluate condition") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestEvaluateCondition_NonBooleanResult(t *testing.T) {
	_, err := EvaluateCondition(`"nope"`, ConditionContext{})
	if err == nil {
		t.Fatal("expected non-boolean result error")
	}
}
