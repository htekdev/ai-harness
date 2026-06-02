package governance_test

import (
	"testing"

	"github.com/htekdev/ai-harness/governance"
)

// TestTraceLogAppendAndLen verifies basic append/len operations.
func TestTraceLogAppendAndLen(t *testing.T) {
	tl := governance.NewTraceLog()
	if tl.Len() != 0 {
		t.Errorf("Len = %d, want 0", tl.Len())
	}
	tl.Append(governance.TraceEvent{Type: "tool.call", Tool: "Read"})
	tl.Append(governance.TraceEvent{Type: "tool.call", Tool: "Read"})
	tl.Append(governance.TraceEvent{Type: "turn.end"})
	if tl.Len() != 3 {
		t.Errorf("Len = %d, want 3", tl.Len())
	}
}

// TestTraceLogLast verifies the Last() window.
func TestTraceLogLast(t *testing.T) {
	tl := governance.NewTraceLog()
	for _, tool := range []string{"Read", "Grep", "Edit", "Write"} {
		tl.Append(governance.TraceEvent{Type: "tool.call", Tool: tool})
	}
	last2 := tl.Last(2)
	if len(last2) != 2 {
		t.Fatalf("Last(2) len = %d, want 2", len(last2))
	}
	if last2[0].Tool != "Edit" || last2[1].Tool != "Write" {
		t.Errorf("Last(2) tools = [%s, %s], want [Edit, Write]", last2[0].Tool, last2[1].Tool)
	}
}

// TestMatchesPatternLast verifies "LAST n CALLS tool" pattern.
func TestMatchesPatternLast(t *testing.T) {
	tl := governance.NewTraceLog()
	for range 5 {
		tl.Append(governance.TraceEvent{Type: "tool.call", Tool: "Read"})
	}

	if !tl.MatchesPattern("LAST 3 CALLS Read") {
		t.Error("LAST 3 CALLS Read should match 5x Read")
	}
	if tl.MatchesPattern("LAST 3 CALLS Edit") {
		t.Error("LAST 3 CALLS Edit should not match 5x Read")
	}
	if !tl.MatchesPattern("LAST 5 CALLS Read") {
		t.Error("LAST 5 CALLS Read should match exactly 5 reads")
	}
	if tl.MatchesPattern("LAST 6 CALLS Read") {
		t.Error("LAST 6 CALLS Read should not match (only 5 events)")
	}

	// Mixed sequence: last 2 are Edit, first 3 are Read.
	tl.Append(governance.TraceEvent{Type: "tool.call", Tool: "Edit"})
	tl.Append(governance.TraceEvent{Type: "tool.call", Tool: "Edit"})
	if tl.MatchesPattern("LAST 3 CALLS Read") {
		t.Error("LAST 3 CALLS Read should not match after Edit events")
	}
	if !tl.MatchesPattern("LAST 2 CALLS Edit") {
		t.Error("LAST 2 CALLS Edit should match after 2 Edit events")
	}
}

// TestMatchesPatternNo verifies "NO tool WITHIN n TURNS" pattern.
func TestMatchesPatternNo(t *testing.T) {
	tl := governance.NewTraceLog()
	for range 5 {
		tl.Append(governance.TraceEvent{Type: "tool.call", Tool: "Read"})
	}

	if !tl.MatchesPattern("NO Edit WITHIN 5 TURNS") {
		t.Error("NO Edit WITHIN 5 TURNS should match (no edits)")
	}

	tl.Append(governance.TraceEvent{Type: "tool.call", Tool: "Edit"})
	if tl.MatchesPattern("NO Edit WITHIN 3 TURNS") {
		t.Error("NO Edit WITHIN 3 TURNS should not match (edit is in last 3)")
	}
	// Add two more Reads so the Edit is pushed outside a 2-turn window.
	tl.Append(governance.TraceEvent{Type: "tool.call", Tool: "Read"})
	tl.Append(governance.TraceEvent{Type: "tool.call", Tool: "Read"})
	if !tl.MatchesPattern("NO Edit WITHIN 2 TURNS") {
		t.Error("NO Edit WITHIN 2 TURNS should match (edit is outside the 2-turn window)")
	}
}

// TestMatchesPatternCount verifies "COUNT tool GT|LT n" pattern.
func TestMatchesPatternCount(t *testing.T) {
	tl := governance.NewTraceLog()
	for range 5 {
		tl.Append(governance.TraceEvent{Type: "tool.call", Tool: "Read"})
	}
	tl.Append(governance.TraceEvent{Type: "tool.call", Tool: "Edit"})

	if !tl.MatchesPattern("COUNT Read GT 3") {
		t.Error("COUNT Read GT 3 should match (5 reads)")
	}
	if tl.MatchesPattern("COUNT Read GT 5") {
		t.Error("COUNT Read GT 5 should not match (exactly 5 reads)")
	}
	if !tl.MatchesPattern("COUNT Read LT 6") {
		t.Error("COUNT Read LT 6 should match (5 reads)")
	}
	if tl.MatchesPattern("COUNT Edit GT 2") {
		t.Error("COUNT Edit GT 2 should not match (only 1 edit)")
	}
}

// TestEnforcementEngineToolAllow verifies that the enforcement engine
// correctly allows tools permitted by the state machine.
func TestEnforcementEngineToolAllow(t *testing.T) {
	ev := newBugfixEvaluator(t)
	tl := governance.NewTraceLog()
	eng := governance.NewEnforcementEngine(ev, tl)

	result := eng.EvaluateToolCall("Read", nil)
	if !result.Allowed {
		t.Errorf("Read should be allowed in planning state; reasons: %v", result.DenyReasons)
	}
}

// TestEnforcementEngineToolDeny verifies that the enforcement engine blocks
// tools not in the state's allowed set.
func TestEnforcementEngineToolDeny(t *testing.T) {
	ev := newBugfixEvaluator(t)
	tl := governance.NewTraceLog()
	eng := governance.NewEnforcementEngine(ev, tl)

	result := eng.EvaluateToolCall("Edit", nil)
	if result.Allowed {
		t.Error("Edit should be denied in planning state")
	}
	if len(result.DenyReasons) == 0 {
		t.Error("expected at least one deny reason")
	}
}

// TestEnforcementEngineCommandDeny verifies that the enforcement engine blocks
// shell commands not in the allowed_commands list.
func TestEnforcementEngineCommandDeny(t *testing.T) {
	ev := newBugfixEvaluator(t)
	// Advance to testing state where Bash is allowed but commands are filtered.
	if _, err := ev.Transition("READY"); err != nil {
		t.Fatal(err)
	}
	if _, err := ev.Transition("DONE"); err != nil {
		t.Fatal(err)
	}

	tl := governance.NewTraceLog()
	eng := governance.NewEnforcementEngine(ev, tl)

	allowed := eng.EvaluateToolCall("Bash", map[string]any{"command": "pytest -x"})
	if !allowed.Allowed {
		t.Errorf("pytest should be allowed; reasons: %v", allowed.DenyReasons)
	}

	denied := eng.EvaluateToolCall("Bash", map[string]any{"command": "rm -rf /"})
	if denied.Allowed {
		t.Error("rm should be denied")
	}
}

// TestEnforcementEngineTraceRule verifies that trace rules fire and produce
// the correct enforcement actions.
func TestEnforcementEngineTraceRule(t *testing.T) {
	w := parseBugfixWorkflow(t)
	w.TraceRules = []*governance.TraceRule{
		{
			ID:          "no-read-loops",
			Pattern:     "LAST 5 CALLS Read",
			Enforcement: governance.ActionInject,
			Payload:     "You have been reading the same files repeatedly. Consider moving to implementation.",
		},
		{
			ID:          "stale-search",
			Pattern:     "COUNT Grep GT 10",
			Enforcement: governance.ActionDeny,
			Payload:     "Excessive search calls detected. Transition to implementing state.",
		},
	}
	ev, err := governance.NewEvaluator(w)
	if err != nil {
		t.Fatal(err)
	}

	tl := governance.NewTraceLog()
	eng := governance.NewEnforcementEngine(ev, tl)

	// 5 consecutive Read calls → inject rule fires.
	for range 5 {
		tl.Append(governance.TraceEvent{Type: "tool.call", Tool: "Read"})
	}
	result := eng.EvaluateToolCall("Read", nil)
	if !result.Allowed {
		t.Error("tool should still be allowed (inject only, not deny)")
	}
	if result.InjectedContext == "" {
		t.Error("expected injected context from no-read-loops rule")
	}

	// 11 Grep calls → deny rule fires.
	for range 11 {
		tl.Append(governance.TraceEvent{Type: "tool.call", Tool: "Grep"})
	}
	result = eng.EvaluateToolCall("Grep", nil)
	if result.Allowed {
		t.Error("Grep should be denied after stale-search rule fires")
	}
}
