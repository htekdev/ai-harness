package evals

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"
)

// Reporter formats eval results for display and file output.
type Reporter struct{}

// PrintSummary outputs a human-readable summary table to stdout.
func (r *Reporter) PrintSummary(suite *SuiteResult) {
	fmt.Printf("\nEVAL RESULTS (%d cases, model: gpt-4o-mini)\n", suite.TotalCases)
	fmt.Println(strings.Repeat("━", 60))

	for _, res := range suite.Results {
		icon := "✅"
		if !res.Passed {
			icon = "❌"
		}
		name := res.Case.Name
		if len(name) > 30 {
			name = name[:30]
		}

		fmt.Printf("%s %-30s [%d tokens, %s]", icon, name, res.Tokens, res.Duration.Round(time.Millisecond))
		if res.Retries > 0 {
			fmt.Printf(" (retry %d)", res.Retries)
		}
		fmt.Println()

		if !res.Passed {
			if res.Error != "" {
				fmt.Printf("   └─ ERROR: %s\n", truncate(res.Error, 80))
			}
			for _, g := range res.Grades {
				if !g.Passed {
					fmt.Printf("   └─ FAIL [%s]: %s\n", g.Criterion.Type, g.Reason)
				}
			}
		}
	}

	fmt.Println(strings.Repeat("━", 60))
	fmt.Printf("PASSED: %d/%d | FAILED: %d | TOKENS: %d | COST: ~$%.4f | TIME: %s\n",
		suite.Passed, suite.TotalCases, suite.Failed,
		suite.Tokens, suite.Cost, suite.Duration.Round(time.Millisecond))

	if suite.Aborted {
		fmt.Println("⚠️  SUITE ABORTED: Budget cap exceeded")
	}
}

// JSONReport is the structured output format for CI/artifacts.
type JSONReport struct {
	Timestamp  string             `json:"timestamp"`
	Model      string             `json:"model"`
	TotalCases int                `json:"total_cases"`
	Passed     int                `json:"passed"`
	Failed     int                `json:"failed"`
	Tokens     int                `json:"tokens"`
	CostUSD    float64            `json:"cost_usd"`
	DurationMS int64              `json:"duration_ms"`
	Aborted    bool               `json:"aborted"`
	Cases      []JSONCaseResult   `json:"cases"`
}

// JSONCaseResult is a single case in the JSON report.
type JSONCaseResult struct {
	Name       string           `json:"name"`
	Category   string           `json:"category"`
	Passed     bool             `json:"passed"`
	Tokens     int              `json:"tokens"`
	DurationMS int64            `json:"duration_ms"`
	Retries    int              `json:"retries"`
	Error      string           `json:"error,omitempty"`
	Failures   []JSONGradeFail  `json:"failures,omitempty"`
}

// JSONGradeFail records a specific grading failure.
type JSONGradeFail struct {
	Type   string `json:"type"`
	Reason string `json:"reason"`
}

// WriteJSON writes the suite results to a JSON file.
func (r *Reporter) WriteJSON(suite *SuiteResult, path string) error {
	report := JSONReport{
		Timestamp:  time.Now().UTC().Format(time.RFC3339),
		Model:      "gpt-4o-mini",
		TotalCases: suite.TotalCases,
		Passed:     suite.Passed,
		Failed:     suite.Failed,
		Tokens:     suite.Tokens,
		CostUSD:    suite.Cost,
		DurationMS: suite.Duration.Milliseconds(),
		Aborted:    suite.Aborted,
	}

	for _, res := range suite.Results {
		cr := JSONCaseResult{
			Name:       res.Case.Name,
			Category:   res.Case.Category,
			Passed:     res.Passed,
			Tokens:     res.Tokens,
			DurationMS: res.Duration.Milliseconds(),
			Retries:    res.Retries,
			Error:      res.Error,
		}
		for _, g := range res.Grades {
			if !g.Passed {
				cr.Failures = append(cr.Failures, JSONGradeFail{
					Type:   g.Criterion.Type,
					Reason: g.Reason,
				})
			}
		}
		report.Cases = append(report.Cases, cr)
	}

	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(path, data, 0644)
}
