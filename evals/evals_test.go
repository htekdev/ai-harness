//go:build eval

package evals_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/htekdev/ai-harness/evals"
)

// TestEvals is the main entry point for the eval suite.
// Run with: go test -tags=eval -v -timeout=5m ./evals/
func TestEvals(t *testing.T) {
	apiKey := os.Getenv("GH_TOKEN")
	if apiKey == "" {
		t.Skip("GH_TOKEN not set — skipping evals (requires real LLM API)")
	}

	cfg := evals.DefaultConfig()
	cfg.CasesDir = "testdata"

	runner := evals.NewRunner(cfg)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	suite, err := runner.Run(ctx)
	if err != nil {
		t.Fatalf("eval runner error: %v", err)
	}

	// Print results
	reporter := &evals.Reporter{}
	reporter.PrintSummary(suite)

	// Write JSON report
	if err := reporter.WriteJSON(suite, "testdata/results.json"); err != nil {
		t.Logf("warning: could not write JSON report: %v", err)
	}

	// Assert
	if suite.Aborted {
		t.Error("eval suite was aborted due to budget cap")
	}

	for _, res := range suite.Results {
		if !res.Passed {
			t.Errorf("EVAL FAILED: %s", res.Case.Name)
			if res.Error != "" {
				t.Logf("  Error: %s", res.Error)
			}
			for _, g := range res.Grades {
				if !g.Passed {
					t.Logf("  Grade [%s]: %s", g.Criterion.Type, g.Reason)
				}
			}
		}
	}

	t.Logf("Suite: %d/%d passed, %d tokens, ~$%.4f",
		suite.Passed, suite.TotalCases, suite.Tokens, suite.Cost)
}
