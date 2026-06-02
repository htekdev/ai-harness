package governance_test

import (
	"testing"

	"github.com/htekdev/ai-harness/governance"
)

// newBugfixEvaluator creates a ready-to-use evaluator from the bugfix workflow.
func newBugfixEvaluator(t *testing.T) *governance.Evaluator {
	t.Helper()
	w := parseBugfixWorkflow(t)
	ev, err := governance.NewEvaluator(w)
	if err != nil {
		t.Fatalf("NewEvaluator: %v", err)
	}
	return ev
}

// TestInitialState verifies the evaluator starts in the correct state.
func TestInitialState(t *testing.T) {
	ev := newBugfixEvaluator(t)
	if ev.CurrentState() != "planning" {
		t.Errorf("CurrentState = %q, want %q", ev.CurrentState(), "planning")
	}
	if ev.IsFinal() {
		t.Error("expected non-final initial state")
	}
}

// TestAllowedTools verifies per-state tool filtering.
func TestAllowedTools(t *testing.T) {
	tests := []struct {
		state   string // state to reach via transitions
		events  []string
		allowed []string
		denied  []string
	}{
		{
			state:   "planning",
			allowed: []string{"Read", "Grep", "Glob"},
			denied:  []string{"Edit", "Write", "Bash", "Delete"},
		},
		{
			state:   "implementing",
			events:  []string{"READY"},
			allowed: []string{"Read", "Edit", "Write"},
			denied:  []string{"Grep", "Glob", "Bash", "Delete"},
		},
		{
			state:   "testing",
			events:  []string{"READY", "DONE"},
			allowed: []string{"Read", "Bash"},
			denied:  []string{"Edit", "Write", "Grep"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.state, func(t *testing.T) {
			ev := newBugfixEvaluator(t)
			for _, event := range tc.events {
				if _, err := ev.Transition(event); err != nil {
					t.Fatalf("Transition(%q): %v", event, err)
				}
			}
			if ev.CurrentState() != tc.state {
				t.Fatalf("state = %q, want %q", ev.CurrentState(), tc.state)
			}
			for _, tool := range tc.allowed {
				if !ev.IsToolAllowed(tool) {
					t.Errorf("IsToolAllowed(%q) = false, want true", tool)
				}
			}
			for _, tool := range tc.denied {
				if ev.IsToolAllowed(tool) {
					t.Errorf("IsToolAllowed(%q) = true, want false", tool)
				}
			}
		})
	}
}

// TestCommandAllowList verifies that shell commands are filtered by prefix.
func TestCommandAllowList(t *testing.T) {
	ev := newBugfixEvaluator(t)
	// Advance to testing state.
	if _, err := ev.Transition("READY"); err != nil {
		t.Fatal(err)
	}
	if _, err := ev.Transition("DONE"); err != nil {
		t.Fatal(err)
	}

	allowed := []string{"pytest", "pytest -x", "cargo test", "npm test --watch"}
	for _, cmd := range allowed {
		if !ev.IsCommandAllowed(cmd) {
			t.Errorf("IsCommandAllowed(%q) = false, want true", cmd)
		}
	}

	denied := []string{"rm -rf /", "python exploit.py", "bash script.sh", "echo > file"}
	for _, cmd := range denied {
		if ev.IsCommandAllowed(cmd) {
			t.Errorf("IsCommandAllowed(%q) = true, want false", cmd)
		}
	}
}

// TestGuardedTransition verifies that a guarded transition only fires when
// the guard condition is satisfied.
func TestGuardedTransition(t *testing.T) {
	ev := newBugfixEvaluator(t)

	// Advance to testing state.
	if _, err := ev.Transition("READY"); err != nil {
		t.Fatal(err)
	}
	if _, err := ev.Transition("DONE"); err != nil {
		t.Fatal(err)
	}

	t.Run("guard not satisfied", func(t *testing.T) {
		// test_result is not set, so guard should fail.
		_, err := ev.Transition("PASS")
		if err == nil {
			t.Error("expected error when guard not satisfied")
		}
	})

	t.Run("guard satisfied", func(t *testing.T) {
		ev.SetContext("test_result", "pass")
		next, err := ev.Transition("PASS")
		if err != nil {
			t.Fatalf("Transition(PASS): %v", err)
		}
		if next != "completed" {
			t.Errorf("next state = %q, want %q", next, "completed")
		}
	})
}

// TestFinalState verifies that no transitions are allowed from a final state.
func TestFinalState(t *testing.T) {
	ev := newBugfixEvaluator(t)

	// Drive workflow to completion.
	if _, err := ev.Transition("READY"); err != nil {
		t.Fatal(err)
	}
	if _, err := ev.Transition("DONE"); err != nil {
		t.Fatal(err)
	}
	ev.SetContext("test_result", "pass")
	if _, err := ev.Transition("PASS"); err != nil {
		t.Fatal(err)
	}

	if !ev.IsFinal() {
		t.Error("expected IsFinal() = true")
	}
	if _, err := ev.Transition("PASS"); err == nil {
		t.Error("expected error transitioning from final state")
	}
}

// TestIterationLimit verifies RecordIteration detects limit violations.
func TestIterationLimit(t *testing.T) {
	ev := newBugfixEvaluator(t)
	// planning has max_iterations: 8
	for i := 0; i < 8; i++ {
		count, exceeded := ev.RecordIteration()
		if exceeded {
			t.Errorf("iteration %d unexpectedly exceeded limit", count)
		}
	}
	count, exceeded := ev.RecordIteration()
	if !exceeded {
		t.Errorf("iteration %d should have exceeded limit of 8", count)
	}
}

// TestFileEditLimit verifies RecordFileEdit detects max_files_per_state violations.
func TestFileEditLimit(t *testing.T) {
	ev := newBugfixEvaluator(t)
	if _, err := ev.Transition("READY"); err != nil {
		t.Fatal(err)
	}
	// implementing has max_files_per_state: 3

	_, exceeded := ev.RecordFileEdit("a.go")
	if exceeded {
		t.Error("first file should not exceed limit")
	}
	_, exceeded = ev.RecordFileEdit("b.go")
	if exceeded {
		t.Error("second file should not exceed limit")
	}
	_, exceeded = ev.RecordFileEdit("c.go")
	if exceeded {
		t.Error("third file should not exceed limit")
	}
	_, exceeded = ev.RecordFileEdit("d.go")
	if !exceeded {
		t.Error("fourth file should exceed limit of 3")
	}
}

// TestUnknownEvent verifies that emitting an unknown event returns an error.
func TestUnknownEvent(t *testing.T) {
	ev := newBugfixEvaluator(t)
	_, err := ev.Transition("NO_SUCH_EVENT")
	if err == nil {
		t.Error("expected error for unknown event")
	}
}

// TestLoopBack verifies that a FAIL_TEST event in testing loops back to implementing.
func TestLoopBack(t *testing.T) {
	ev := newBugfixEvaluator(t)
	if _, err := ev.Transition("READY"); err != nil {
		t.Fatal(err)
	}
	if _, err := ev.Transition("DONE"); err != nil {
		t.Fatal(err)
	}
	next, err := ev.Transition("FAIL_TEST")
	if err != nil {
		t.Fatalf("Transition(FAIL_TEST): %v", err)
	}
	if next != "implementing" {
		t.Errorf("next = %q, want %q", next, "implementing")
	}
}

// TestRequiredModel verifies per-state model routing.
func TestRequiredModel(t *testing.T) {
	w := parseBugfixWorkflow(t)
	w.States["implementing"].Model = "gpt-4o-mini"
	ev, err := governance.NewEvaluator(w)
	if err != nil {
		t.Fatal(err)
	}

	if m := ev.RequiredModel(); m != "" {
		t.Errorf("planning model = %q, want empty", m)
	}

	if _, err := ev.Transition("READY"); err != nil {
		t.Fatal(err)
	}
	if m := ev.RequiredModel(); m != "gpt-4o-mini" {
		t.Errorf("implementing model = %q, want %q", m, "gpt-4o-mini")
	}
}

// TestDefaultAllowedTools verifies that DefaultAllowedTools is used when a
// state does not specify its own allowed_tools.
func TestDefaultAllowedTools(t *testing.T) {
	w := &governance.Workflow{
		ID:                  "default-policy",
		Initial:             "start",
		DefaultAllowedTools: []string{"Read"},
		States: map[string]*governance.State{
			"start": {
				// No AllowedTools — should inherit DefaultAllowedTools.
				On: map[string]governance.Transition{"GO": {Target: "end"}},
			},
			"override": {
				// Explicit override.
				AllowedTools: []string{"Edit"},
			},
			"end": {Type: governance.StateTypeFinal},
		},
	}
	ev, err := governance.NewEvaluator(w)
	if err != nil {
		t.Fatal(err)
	}

	if !ev.IsToolAllowed("Read") {
		t.Error("Read should be allowed via DefaultAllowedTools")
	}
	if ev.IsToolAllowed("Edit") {
		t.Error("Edit should be denied in start state")
	}
}

// TestBlockedTools verifies the BlockedTools field (AI Harness extension).
func TestBlockedTools(t *testing.T) {
	w := &governance.Workflow{
		ID:      "blocked",
		Initial: "work",
		States: map[string]*governance.State{
			"work": {
				AllowedTools: []string{"Read", "Bash", "Edit"},
				BlockedTools: []string{"Bash"},
			},
		},
	}
	ev, err := governance.NewEvaluator(w)
	if err != nil {
		t.Fatal(err)
	}

	if !ev.IsToolAllowed("Read") {
		t.Error("Read should be allowed")
	}
	if ev.IsToolAllowed("Bash") {
		t.Error("Bash should be blocked")
	}
	if !ev.IsToolAllowed("Edit") {
		t.Error("Edit should be allowed")
	}
}
