package evals

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/htekdev/ai-harness/tools"
)

// GradeResult is the outcome of a single grading criterion.
type GradeResult struct {
	Criterion GradeCriterion
	Passed    bool
	Reason    string
}

// Transcript captures the full execution trace of an eval case.
type Transcript struct {
	Turns         []TranscriptTurn
	TotalTokens   int
	TotalDuration time.Duration
	Errors        []string
	// DelegationEvents tracks delegation calls.
	DelegationEvents []DelegationEvent
	// HookEvents tracks hook firings.
	HookEvents []HookEvent
}

// TranscriptTurn is one turn of the conversation.
type TranscriptTurn struct {
	UserMessage string
	Response    string
	ToolCalls   []tools.Call
	ToolResults []tools.Result
	Tokens      int
	Duration    time.Duration
}

// DelegationEvent records that delegation occurred.
type DelegationEvent struct {
	Task   string
	Depth  int
	Result string
}

// HookEvent records a hook firing.
type HookEvent struct {
	Name   string
	Event  string
	Action string // "allow", "block", "modify"
	Reason string
}

// Grade runs all grading criteria against a transcript and returns results.
func Grade(criteria []GradeCriterion, transcript *Transcript) []GradeResult {
	results := make([]GradeResult, 0, len(criteria))
	for _, c := range criteria {
		result := gradeOne(c, transcript)
		results = append(results, result)
	}
	return results
}

// AllPassed returns true if every grade result passed.
func AllPassed(results []GradeResult) bool {
	for _, r := range results {
		if !r.Passed {
			return false
		}
	}
	return true
}

func gradeOne(c GradeCriterion, t *Transcript) GradeResult {
	switch c.Type {
	case "response_contains":
		return gradeResponseContains(c, t)
	case "response_matches":
		return gradeResponseMatches(c, t)
	case "response_not_contains":
		return gradeResponseNotContains(c, t)
	case "tool_called":
		return gradeToolCalled(c, t)
	case "tool_not_called":
		return gradeToolNotCalled(c, t)
	case "tool_call_count":
		return gradeToolCallCount(c, t)
	case "max_tool_iterations":
		return gradeMaxToolIterations(c, t)
	case "delegation_occurred":
		return gradeDelegationOccurred(c, t)
	case "delegation_depth":
		return gradeDelegationDepth(c, t)
	case "hook_fired":
		return gradeHookFired(c, t)
	case "hook_blocked":
		return gradeHookBlocked(c, t)
	case "no_errors":
		return gradeNoErrors(c, t)
	case "completed_within":
		return gradeCompletedWithin(c, t)
	case "tokens_under":
		return gradeTokensUnder(c, t)
	default:
		return GradeResult{Criterion: c, Passed: false, Reason: fmt.Sprintf("unknown grader type: %s", c.Type)}
	}
}

func lastResponse(t *Transcript) string {
	if len(t.Turns) == 0 {
		return ""
	}
	return t.Turns[len(t.Turns)-1].Response
}

func allToolCalls(t *Transcript) []tools.Call {
	var calls []tools.Call
	for _, turn := range t.Turns {
		calls = append(calls, turn.ToolCalls...)
	}
	return calls
}

func gradeResponseContains(c GradeCriterion, t *Transcript) GradeResult {
	resp := lastResponse(t)
	if strings.Contains(resp, c.Value) {
		return GradeResult{Criterion: c, Passed: true}
	}
	return GradeResult{
		Criterion: c,
		Passed:    false,
		Reason:    fmt.Sprintf("response does not contain %q (got: %s)", c.Value, truncate(resp, 100)),
	}
}

func gradeResponseMatches(c GradeCriterion, t *Transcript) GradeResult {
	resp := lastResponse(t)
	re, err := regexp.Compile(c.Value)
	if err != nil {
		return GradeResult{Criterion: c, Passed: false, Reason: fmt.Sprintf("invalid regex: %v", err)}
	}
	if re.MatchString(resp) {
		return GradeResult{Criterion: c, Passed: true}
	}
	return GradeResult{
		Criterion: c,
		Passed:    false,
		Reason:    fmt.Sprintf("response does not match /%s/ (got: %s)", c.Value, truncate(resp, 100)),
	}
}

func gradeResponseNotContains(c GradeCriterion, t *Transcript) GradeResult {
	resp := lastResponse(t)
	if !strings.Contains(resp, c.Value) {
		return GradeResult{Criterion: c, Passed: true}
	}
	return GradeResult{
		Criterion: c,
		Passed:    false,
		Reason:    fmt.Sprintf("response should NOT contain %q but does", c.Value),
	}
}

func gradeToolCalled(c GradeCriterion, t *Transcript) GradeResult {
	calls := allToolCalls(t)
	for _, call := range calls {
		if call.Name == c.Tool {
			// Check args_contain if specified
			if len(c.ArgsContain) > 0 {
				var args map[string]interface{}
				if err := json.Unmarshal(call.Arguments, &args); err != nil {
					continue
				}
				if argsMatch(args, c.ArgsContain) {
					return GradeResult{Criterion: c, Passed: true}
				}
				continue
			}
			return GradeResult{Criterion: c, Passed: true}
		}
	}
	reason := fmt.Sprintf("tool %q was never called", c.Tool)
	if len(c.ArgsContain) > 0 {
		reason = fmt.Sprintf("tool %q was not called with expected args", c.Tool)
	}
	return GradeResult{Criterion: c, Passed: false, Reason: reason}
}

func gradeToolNotCalled(c GradeCriterion, t *Transcript) GradeResult {
	calls := allToolCalls(t)
	for _, call := range calls {
		if call.Name == c.Tool {
			return GradeResult{Criterion: c, Passed: false, Reason: fmt.Sprintf("tool %q was called but should not have been", c.Tool)}
		}
	}
	return GradeResult{Criterion: c, Passed: true}
}

func gradeToolCallCount(c GradeCriterion, t *Transcript) GradeResult {
	calls := allToolCalls(t)
	count := 0
	if c.Tool != "" {
		for _, call := range calls {
			if call.Name == c.Tool {
				count++
			}
		}
	} else {
		count = len(calls)
	}

	// Support min_value (at least N calls)
	if c.MinValue > 0 {
		if count >= c.MinValue {
			return GradeResult{Criterion: c, Passed: true}
		}
		return GradeResult{
			Criterion: c,
			Passed:    false,
			Reason:    fmt.Sprintf("expected at least %d tool calls (tool=%q), got %d", c.MinValue, c.Tool, count),
		}
	}

	// Support max_value (at most N calls)
	if c.MaxValue > 0 {
		if count <= c.MaxValue {
			return GradeResult{Criterion: c, Passed: true}
		}
		return GradeResult{
			Criterion: c,
			Passed:    false,
			Reason:    fmt.Sprintf("expected at most %d tool calls (tool=%q), got %d", c.MaxValue, c.Tool, count),
		}
	}

	// Exact match
	if count == c.Count {
		return GradeResult{Criterion: c, Passed: true}
	}
	return GradeResult{
		Criterion: c,
		Passed:    false,
		Reason:    fmt.Sprintf("expected %d tool calls (tool=%q), got %d", c.Count, c.Tool, count),
	}
}

func gradeMaxToolIterations(c GradeCriterion, t *Transcript) GradeResult {
	totalCalls := len(allToolCalls(t))
	max := c.MaxValue
	if max == 0 {
		// Try parsing from Value field
		fmt.Sscanf(c.Value, "%d", &max)
	}
	if max == 0 {
		max = 5 // default
	}
	if totalCalls <= max {
		return GradeResult{Criterion: c, Passed: true}
	}
	return GradeResult{
		Criterion: c,
		Passed:    false,
		Reason:    fmt.Sprintf("too many tool iterations: %d > %d", totalCalls, max),
	}
}

func gradeDelegationOccurred(c GradeCriterion, t *Transcript) GradeResult {
	if len(t.DelegationEvents) > 0 {
		if c.Value != "" {
			// Check that at least one delegation task contains the value
			for _, de := range t.DelegationEvents {
				if strings.Contains(de.Task, c.Value) || strings.Contains(de.Result, c.Value) {
					return GradeResult{Criterion: c, Passed: true}
				}
			}
			return GradeResult{
				Criterion: c,
				Passed:    false,
				Reason:    fmt.Sprintf("delegation occurred but none matched %q", c.Value),
			}
		}
		return GradeResult{Criterion: c, Passed: true}
	}
	// Also check if "delegate" tool was called
	for _, call := range allToolCalls(t) {
		if call.Name == "delegate" || call.Name == "delegate_async" {
			return GradeResult{Criterion: c, Passed: true}
		}
	}
	return GradeResult{Criterion: c, Passed: false, Reason: "no delegation detected"}
}

func gradeDelegationDepth(c GradeCriterion, t *Transcript) GradeResult {
	max := c.MaxValue
	if max == 0 {
		fmt.Sscanf(c.Value, "%d", &max)
	}
	maxSeen := 0
	for _, de := range t.DelegationEvents {
		if de.Depth > maxSeen {
			maxSeen = de.Depth
		}
	}
	if maxSeen <= max {
		return GradeResult{Criterion: c, Passed: true}
	}
	return GradeResult{
		Criterion: c,
		Passed:    false,
		Reason:    fmt.Sprintf("delegation depth %d exceeds max %d", maxSeen, max),
	}
}

func gradeHookFired(c GradeCriterion, t *Transcript) GradeResult {
	for _, he := range t.HookEvents {
		if he.Name == c.Value || he.Name == c.Tool {
			return GradeResult{Criterion: c, Passed: true}
		}
	}
	return GradeResult{
		Criterion: c,
		Passed:    false,
		Reason:    fmt.Sprintf("hook %q was not fired", c.Value),
	}
}

func gradeHookBlocked(c GradeCriterion, t *Transcript) GradeResult {
	for _, he := range t.HookEvents {
		if he.Action == "block" {
			if c.Value == "" || strings.Contains(he.Name, c.Value) || strings.Contains(he.Reason, c.Value) {
				return GradeResult{Criterion: c, Passed: true}
			}
		}
	}
	// Also check tool results for blocked indicators
	for _, turn := range t.Turns {
		for _, result := range turn.ToolResults {
			if result.IsError && strings.Contains(result.Content, "blocked") {
				return GradeResult{Criterion: c, Passed: true}
			}
		}
	}
	return GradeResult{Criterion: c, Passed: false, Reason: "no hook blocking detected"}
}

func gradeNoErrors(c GradeCriterion, t *Transcript) GradeResult {
	if len(t.Errors) == 0 {
		return GradeResult{Criterion: c, Passed: true}
	}
	return GradeResult{
		Criterion: c,
		Passed:    false,
		Reason:    fmt.Sprintf("%d errors occurred: %s", len(t.Errors), strings.Join(t.Errors, "; ")),
	}
}

func gradeCompletedWithin(c GradeCriterion, t *Transcript) GradeResult {
	limit, err := time.ParseDuration(c.Value)
	if err != nil {
		return GradeResult{Criterion: c, Passed: false, Reason: fmt.Sprintf("invalid duration: %v", err)}
	}
	if t.TotalDuration <= limit {
		return GradeResult{Criterion: c, Passed: true}
	}
	return GradeResult{
		Criterion: c,
		Passed:    false,
		Reason:    fmt.Sprintf("took %v, limit is %v", t.TotalDuration, limit),
	}
}

func gradeTokensUnder(c GradeCriterion, t *Transcript) GradeResult {
	var limit int
	fmt.Sscanf(c.Value, "%d", &limit)
	if limit == 0 {
		limit = 2000
	}
	if t.TotalTokens <= limit {
		return GradeResult{Criterion: c, Passed: true}
	}
	return GradeResult{
		Criterion: c,
		Passed:    false,
		Reason:    fmt.Sprintf("used %d tokens, limit is %d", t.TotalTokens, limit),
	}
}

// argsMatch checks if actual args contain all expected key-value pairs.
func argsMatch(actual map[string]interface{}, expected map[string]interface{}) bool {
	for key, expectedVal := range expected {
		actualVal, ok := actual[key]
		if !ok {
			return false
		}
		// Compare as lowercase strings for flexibility (handles minor model variations)
		actualStr := strings.ToLower(fmt.Sprintf("%v", actualVal))
		expectedStr := strings.ToLower(fmt.Sprintf("%v", expectedVal))
		if !strings.Contains(actualStr, expectedStr) && actualStr != expectedStr {
			return false
		}
	}
	return true
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
