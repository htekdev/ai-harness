package governance_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/htekdev/ai-harness/governance"
)

// TestExampleWorkflows loads every JSON file in the examples directory and
// verifies that it parses and validates without errors.
func TestExampleWorkflows(t *testing.T) {
	entries, err := os.ReadDir("examples")
	if err != nil {
		t.Fatalf("reading examples dir: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("no example workflow files found")
	}

	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
			continue
		}
		name := e.Name()
		t.Run(name, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join("examples", name))
			if err != nil {
				t.Fatalf("read %s: %v", name, err)
			}
			var w governance.Workflow
			if err := json.Unmarshal(data, &w); err != nil {
				t.Fatalf("unmarshal %s: %v", name, err)
			}
			if err := w.Validate(); err != nil {
				t.Errorf("validate %s: %v", name, err)
			}

			// Verify an evaluator can be created for each valid workflow.
			ev, err := governance.NewEvaluator(&w)
			if err != nil {
				t.Fatalf("NewEvaluator for %s: %v", name, err)
			}
			if ev.CurrentState() != w.Initial {
				t.Errorf("%s: initial state = %q, want %q", name, ev.CurrentState(), w.Initial)
			}
		})
	}
}
