package evals

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/htekdev/ai-harness/tools"
)

func TestLoadCase(t *testing.T) {
	dir := t.TempDir()
	yaml := `
name: "test-case"
description: "A test eval case"
category: "basic"
model: "gpt-4o-mini"
max_tokens: 300
timeout: "15s"
setup:
  system_prompt: "You are a helpful assistant."
  tools:
    - name: add
      description: "Add two numbers"
      parameters:
        a: { type: number, required: true }
        b: { type: number, required: true }
      script: |
        def handle(args):
          return str(int(args["a"]) + int(args["b"]))
turns:
  - role: user
    content: "What is 2 + 3?"
grade:
  - type: response_contains
    value: "5"
  - type: tool_called
    tool: add
`
	path := filepath.Join(dir, "test.yaml")
	if err := os.WriteFile(path, []byte(yaml), 0644); err != nil {
		t.Fatal(err)
	}

	c, err := LoadCase(path)
	if err != nil {
		t.Fatal(err)
	}

	if c.Name != "test-case" {
		t.Errorf("name = %q, want %q", c.Name, "test-case")
	}
	if c.Category != "basic" {
		t.Errorf("category = %q, want %q", c.Category, "basic")
	}
	if c.Timeout != 15*time.Second {
		t.Errorf("timeout = %v, want 15s", c.Timeout)
	}
	if c.MaxTokens != 300 {
		t.Errorf("max_tokens = %d, want 300", c.MaxTokens)
	}
	if len(c.Setup.Tools) != 1 {
		t.Fatalf("tools count = %d, want 1", len(c.Setup.Tools))
	}
	if c.Setup.Tools[0].Name != "add" {
		t.Errorf("tool name = %q, want %q", c.Setup.Tools[0].Name, "add")
	}
	if len(c.Turns) != 1 {
		t.Fatalf("turns count = %d, want 1", len(c.Turns))
	}
	if len(c.Grade) != 2 {
		t.Fatalf("grade count = %d, want 2", len(c.Grade))
	}
	if c.Grade[0].Type != "response_contains" {
		t.Errorf("grade[0].type = %q, want %q", c.Grade[0].Type, "response_contains")
	}
	if c.Grade[0].Value != "5" {
		t.Errorf("grade[0].value = %q, want %q", c.Grade[0].Value, "5")
	}
}

func TestLoadCases(t *testing.T) {
	dir := t.TempDir()

	for i, name := range []string{"01_test.yaml", "02_test.yaml", "not_yaml.txt"} {
		content := ""
		if filepath.Ext(name) == ".yaml" {
			content = `
name: "case-` + string(rune('a'+i)) + `"
description: "Test"
category: "basic"
turns:
  - role: user
    content: "hello"
grade:
  - type: no_errors
`
		} else {
			content = "not yaml"
		}
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}

	cases, err := LoadCases(dir)
	if err != nil {
		t.Fatal(err)
	}

	if len(cases) != 2 {
		t.Fatalf("loaded %d cases, want 2", len(cases))
	}
}

func TestLoadCaseDefaults(t *testing.T) {
	dir := t.TempDir()
	yaml := `
name: "minimal"
description: "Minimal case"
category: "basic"
turns:
  - role: user
    content: "hi"
grade:
  - type: no_errors
`
	path := filepath.Join(dir, "minimal.yaml")
	if err := os.WriteFile(path, []byte(yaml), 0644); err != nil {
		t.Fatal(err)
	}

	c, err := LoadCase(path)
	if err != nil {
		t.Fatal(err)
	}

	if c.Model != "gpt-4o-mini" {
		t.Errorf("default model = %q, want %q", c.Model, "gpt-4o-mini")
	}
	if c.MaxTokens != 500 {
		t.Errorf("default max_tokens = %d, want 500", c.MaxTokens)
	}
	if c.Timeout != 30*time.Second {
		t.Errorf("default timeout = %v, want 30s", c.Timeout)
	}
}

func TestGradeResponseContains(t *testing.T) {
	transcript := &Transcript{
		Turns: []TranscriptTurn{
			{Response: "The answer is 42."},
		},
	}

	tests := []struct {
		value  string
		expect bool
	}{
		{"42", true},
		{"answer", true},
		{"99", false},
	}

	for _, tt := range tests {
		criteria := []GradeCriterion{{Type: "response_contains", Value: tt.value}}
		results := Grade(criteria, transcript)
		if results[0].Passed != tt.expect {
			t.Errorf("response_contains(%q) = %v, want %v", tt.value, results[0].Passed, tt.expect)
		}
	}
}

func TestGradeResponseMatches(t *testing.T) {
	transcript := &Transcript{
		Turns: []TranscriptTurn{
			{Response: "The result is 986 exactly."},
		},
	}

	tests := []struct {
		pattern string
		expect  bool
	}{
		{`\b986\b`, true},
		{`\d{3}`, true},
		{`\b999\b`, false},
	}

	for _, tt := range tests {
		criteria := []GradeCriterion{{Type: "response_matches", Value: tt.pattern}}
		results := Grade(criteria, transcript)
		if results[0].Passed != tt.expect {
			t.Errorf("response_matches(%q) = %v, want %v", tt.pattern, results[0].Passed, tt.expect)
		}
	}
}

func TestGradeResponseNotContains(t *testing.T) {
	transcript := &Transcript{
		Turns: []TranscriptTurn{
			{Response: "Here is the answer: 42"},
		},
	}

	criteria := []GradeCriterion{
		{Type: "response_not_contains", Value: "I can't"},
		{Type: "response_not_contains", Value: "42"},
	}
	results := Grade(criteria, transcript)

	if !results[0].Passed {
		t.Error("should pass: response doesn't contain 'I can't'")
	}
	if results[1].Passed {
		t.Error("should fail: response does contain '42'")
	}
}

func TestGradeToolCalled(t *testing.T) {
	transcript := &Transcript{
		Turns: []TranscriptTurn{
			{
				ToolCalls: []tools.Call{
					{Name: "add", Arguments: []byte(`{"a": 5, "b": 3}`)},
				},
			},
		},
	}

	// Tool called with any args
	criteria := []GradeCriterion{{Type: "tool_called", Tool: "add"}}
	results := Grade(criteria, transcript)
	if !results[0].Passed {
		t.Error("should pass: 'add' was called")
	}

	// Tool called with specific args
	criteria = []GradeCriterion{{
		Type:        "tool_called",
		Tool:        "add",
		ArgsContain: map[string]interface{}{"a": 5},
	}}
	results = Grade(criteria, transcript)
	if !results[0].Passed {
		t.Error("should pass: 'add' called with a=5")
	}

	// Tool not called
	criteria = []GradeCriterion{{Type: "tool_called", Tool: "subtract"}}
	results = Grade(criteria, transcript)
	if results[0].Passed {
		t.Error("should fail: 'subtract' was never called")
	}
}

func TestGradeToolNotCalled(t *testing.T) {
	transcript := &Transcript{
		Turns: []TranscriptTurn{
			{ToolCalls: []tools.Call{{Name: "add"}}},
		},
	}

	criteria := []GradeCriterion{
		{Type: "tool_not_called", Tool: "delete"},
		{Type: "tool_not_called", Tool: "add"},
	}
	results := Grade(criteria, transcript)

	if !results[0].Passed {
		t.Error("should pass: 'delete' was not called")
	}
	if results[1].Passed {
		t.Error("should fail: 'add' was called")
	}
}

func TestGradeToolCallCount(t *testing.T) {
	transcript := &Transcript{
		Turns: []TranscriptTurn{
			{ToolCalls: []tools.Call{{Name: "add"}, {Name: "add"}, {Name: "multiply"}}},
		},
	}

	// Count specific tool
	criteria := []GradeCriterion{{Type: "tool_call_count", Tool: "add", Count: 2}}
	results := Grade(criteria, transcript)
	if !results[0].Passed {
		t.Error("should pass: 'add' called exactly 2 times")
	}

	// Count all tools
	criteria = []GradeCriterion{{Type: "tool_call_count", Count: 3}}
	results = Grade(criteria, transcript)
	if !results[0].Passed {
		t.Error("should pass: 3 total tool calls")
	}

	// Wrong count
	criteria = []GradeCriterion{{Type: "tool_call_count", Tool: "add", Count: 5}}
	results = Grade(criteria, transcript)
	if results[0].Passed {
		t.Error("should fail: 'add' called 2 times, not 5")
	}
}

func TestGradeMaxToolIterations(t *testing.T) {
	transcript := &Transcript{
		Turns: []TranscriptTurn{
			{ToolCalls: []tools.Call{{Name: "a"}, {Name: "b"}, {Name: "c"}}},
		},
	}

	criteria := []GradeCriterion{{Type: "max_tool_iterations", MaxValue: 5}}
	results := Grade(criteria, transcript)
	if !results[0].Passed {
		t.Error("should pass: 3 calls <= 5 max")
	}

	criteria = []GradeCriterion{{Type: "max_tool_iterations", MaxValue: 2}}
	results = Grade(criteria, transcript)
	if results[0].Passed {
		t.Error("should fail: 3 calls > 2 max")
	}
}

func TestGradeDelegationOccurred(t *testing.T) {
	// With delegation events
	transcript := &Transcript{
		DelegationEvents: []DelegationEvent{
			{Task: "research the topic", Depth: 1, Result: "found info"},
		},
	}
	criteria := []GradeCriterion{{Type: "delegation_occurred"}}
	results := Grade(criteria, transcript)
	if !results[0].Passed {
		t.Error("should pass: delegation occurred")
	}

	// With value match
	criteria = []GradeCriterion{{Type: "delegation_occurred", Value: "research"}}
	results = Grade(criteria, transcript)
	if !results[0].Passed {
		t.Error("should pass: delegation task contains 'research'")
	}

	// No delegation
	empty := &Transcript{}
	criteria = []GradeCriterion{{Type: "delegation_occurred"}}
	results = Grade(criteria, empty)
	if results[0].Passed {
		t.Error("should fail: no delegation")
	}
}

func TestGradeHookBlocked(t *testing.T) {
	transcript := &Transcript{
		HookEvents: []HookEvent{
			{Name: "path_guard", Event: "tool.pre", Action: "block", Reason: "path traversal"},
		},
	}

	criteria := []GradeCriterion{{Type: "hook_blocked"}}
	results := Grade(criteria, transcript)
	if !results[0].Passed {
		t.Error("should pass: a hook blocked")
	}

	criteria = []GradeCriterion{{Type: "hook_blocked", Value: "path_guard"}}
	results = Grade(criteria, transcript)
	if !results[0].Passed {
		t.Error("should pass: path_guard blocked")
	}
}

func TestGradeNoErrors(t *testing.T) {
	clean := &Transcript{}
	criteria := []GradeCriterion{{Type: "no_errors"}}
	results := Grade(criteria, clean)
	if !results[0].Passed {
		t.Error("should pass: no errors")
	}

	withErrors := &Transcript{Errors: []string{"something failed"}}
	results = Grade(criteria, withErrors)
	if results[0].Passed {
		t.Error("should fail: has errors")
	}
}

func TestGradeCompletedWithin(t *testing.T) {
	transcript := &Transcript{TotalDuration: 2 * time.Second}

	criteria := []GradeCriterion{{Type: "completed_within", Value: "5s"}}
	results := Grade(criteria, transcript)
	if !results[0].Passed {
		t.Error("should pass: 2s < 5s")
	}

	criteria = []GradeCriterion{{Type: "completed_within", Value: "1s"}}
	results = Grade(criteria, transcript)
	if results[0].Passed {
		t.Error("should fail: 2s > 1s")
	}
}

func TestGradeTokensUnder(t *testing.T) {
	transcript := &Transcript{TotalTokens: 500}

	criteria := []GradeCriterion{{Type: "tokens_under", Value: "1000"}}
	results := Grade(criteria, transcript)
	if !results[0].Passed {
		t.Error("should pass: 500 < 1000")
	}

	criteria = []GradeCriterion{{Type: "tokens_under", Value: "100"}}
	results = Grade(criteria, transcript)
	if results[0].Passed {
		t.Error("should fail: 500 > 100")
	}
}

func TestAllPassed(t *testing.T) {
	all := []GradeResult{{Passed: true}, {Passed: true}, {Passed: true}}
	if !AllPassed(all) {
		t.Error("should be true: all passed")
	}

	mixed := []GradeResult{{Passed: true}, {Passed: false}, {Passed: true}}
	if AllPassed(mixed) {
		t.Error("should be false: one failed")
	}
}

func TestCostTracker(t *testing.T) {
	ct := &CostTracker{}
	ct.Add(1000)
	ct.Add(500)

	if ct.TotalTokens() != 1500 {
		t.Errorf("total = %d, want 1500", ct.TotalTokens())
	}

	// Estimated cost: 1500 * 0.40 / 1M = $0.0000006
	cost := ct.EstimatedUSD()
	if cost <= 0 {
		t.Error("cost should be > 0")
	}
	if cost >= 0.01 {
		t.Errorf("cost = %f, seems too high for 1500 tokens", cost)
	}

	ct.Reset()
	if ct.TotalTokens() != 0 {
		t.Error("after reset, should be 0")
	}
}
