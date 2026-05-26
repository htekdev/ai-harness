package evals_test

import (
	"testing"

	"github.com/htekdev/ai-harness/evals"
)

func TestLoadAllCases(t *testing.T) {
	cases, err := evals.LoadCases("testdata")
	if err != nil {
		t.Fatalf("failed to load cases: %v", err)
	}
	t.Logf("Successfully loaded %d eval cases", len(cases))
	for _, c := range cases {
		if c.Name == "" {
			t.Errorf("case from %s has empty name", c.Category)
		}
		if len(c.Turns) == 0 {
			t.Errorf("case %q has no turns", c.Name)
		}
		if len(c.Grade) == 0 {
			t.Errorf("case %q has no grading criteria", c.Name)
		}
	}
}
