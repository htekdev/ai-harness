package governance_test

import (
	"encoding/json"
	"testing"

	"github.com/htekdev/ai-harness/governance"
)

// bugfixWorkflow is a direct port of the Statewright canonical example.
// It validates schema-level compatibility.
var bugfixWorkflowJSON = `{
  "id": "bugfix",
  "initial": "planning",
  "states": {
    "planning": {
      "allowed_tools": ["Read", "Grep", "Glob"],
      "max_iterations": 8,
      "on": { "READY": "implementing" }
    },
    "implementing": {
      "allowed_tools": ["Read", "Edit", "Write"],
      "max_edit_lines": 20,
      "max_files_per_state": 3,
      "on": { "DONE": "testing" }
    },
    "testing": {
      "allowed_tools": ["Read", "Bash"],
      "allowed_commands": ["pytest", "cargo test", "npm test"],
      "on": {
        "PASS": { "target": "completed", "guard": "tests_passed" },
        "FAIL_TEST": "implementing"
      }
    },
    "completed": { "type": "final" }
  },
  "guards": {
    "tests_passed": { "field": "test_result", "op": "eq", "value": "pass" }
  }
}`

func parseBugfixWorkflow(t *testing.T) *governance.Workflow {
	t.Helper()
	var w governance.Workflow
	if err := json.Unmarshal([]byte(bugfixWorkflowJSON), &w); err != nil {
		t.Fatalf("unmarshal bugfix workflow: %v", err)
	}
	return &w
}

// TestSchemaCompatibility verifies that the Statewright canonical bugfix
// workflow JSON is parsed correctly into AI Harness types.
func TestSchemaCompatibility(t *testing.T) {
	w := parseBugfixWorkflow(t)

	if w.ID != "bugfix" {
		t.Errorf("ID = %q, want %q", w.ID, "bugfix")
	}
	if w.Initial != "planning" {
		t.Errorf("Initial = %q, want %q", w.Initial, "planning")
	}
	if len(w.States) != 4 {
		t.Errorf("len(States) = %d, want 4", len(w.States))
	}

	planning := w.States["planning"]
	if planning == nil {
		t.Fatal("planning state not found")
	}
	if len(planning.AllowedTools) != 3 {
		t.Errorf("planning.AllowedTools len = %d, want 3", len(planning.AllowedTools))
	}
	if planning.MaxIterations != 8 {
		t.Errorf("planning.MaxIterations = %d, want 8", planning.MaxIterations)
	}

	completed := w.States["completed"]
	if completed == nil {
		t.Fatal("completed state not found")
	}
	if completed.Type != governance.StateTypeFinal {
		t.Errorf("completed.Type = %q, want %q", completed.Type, governance.StateTypeFinal)
	}

	if len(w.Guards) != 1 {
		t.Errorf("len(Guards) = %d, want 1", len(w.Guards))
	}
	g := w.Guards["tests_passed"]
	if g == nil {
		t.Fatal("tests_passed guard not found")
	}
	if g.Op != governance.GuardOpEq {
		t.Errorf("guard op = %q, want %q", g.Op, governance.GuardOpEq)
	}
}

// TestValidate checks that Validate catches common schema errors.
func TestValidate(t *testing.T) {
	t.Run("valid workflow", func(t *testing.T) {
		w := parseBugfixWorkflow(t)
		if err := w.Validate(); err != nil {
			t.Errorf("unexpected validation error: %v", err)
		}
	})

	t.Run("missing id", func(t *testing.T) {
		w := parseBugfixWorkflow(t)
		w.ID = ""
		if err := w.Validate(); err == nil {
			t.Error("expected error for missing id")
		}
	})

	t.Run("unknown initial state", func(t *testing.T) {
		w := parseBugfixWorkflow(t)
		w.Initial = "nonexistent"
		if err := w.Validate(); err == nil {
			t.Error("expected error for unknown initial state")
		}
	})

	t.Run("transition to unknown state", func(t *testing.T) {
		w := parseBugfixWorkflow(t)
		w.States["planning"].On["READY"] = governance.Transition{Target: "ghost"}
		if err := w.Validate(); err == nil {
			t.Error("expected error for transition to undefined state")
		}
	})

	t.Run("transition with unknown guard", func(t *testing.T) {
		w := parseBugfixWorkflow(t)
		w.States["testing"].On["PASS"] = governance.Transition{Target: "completed", Guard: "undefined_guard"}
		if err := w.Validate(); err == nil {
			t.Error("expected error for undefined guard reference")
		}
	})
}

// TestTransitionSerialization verifies that simple (string) and guarded
// (object) transitions round-trip through JSON correctly.
func TestTransitionSerialization(t *testing.T) {
	t.Run("simple transition marshals as string", func(t *testing.T) {
		tr := governance.Transition{Target: "implementing"}
		b, err := json.Marshal(tr)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		if string(b) != `"implementing"` {
			t.Errorf("got %s, want %q", b, "implementing")
		}
	})

	t.Run("guarded transition marshals as object", func(t *testing.T) {
		tr := governance.Transition{Target: "completed", Guard: "tests_passed"}
		b, err := json.Marshal(tr)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		var out map[string]string
		if err := json.Unmarshal(b, &out); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if out["target"] != "completed" {
			t.Errorf("target = %q, want %q", out["target"], "completed")
		}
		if out["guard"] != "tests_passed" {
			t.Errorf("guard = %q, want %q", out["guard"], "tests_passed")
		}
	})

	t.Run("string transition unmarshals correctly", func(t *testing.T) {
		var tr governance.Transition
		if err := json.Unmarshal([]byte(`"implementing"`), &tr); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if tr.Target != "implementing" {
			t.Errorf("Target = %q, want %q", tr.Target, "implementing")
		}
		if tr.Guard != "" {
			t.Errorf("Guard = %q, want empty", tr.Guard)
		}
	})
}
