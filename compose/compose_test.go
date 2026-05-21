package compose

import "testing"

func TestComposeIntegration_FromTestdata(t *testing.T) {
	resolved, err := ResolveHarness("testdata", "coder", ConditionContext{Values: map[string]interface{}{"mode": "pull_request"}})
	if err != nil {
		t.Fatalf("ResolveHarness error: %v", err)
	}

	if resolved.Policy.Model.Provider != "copilot" {
		t.Fatalf("unexpected provider: %q", resolved.Policy.Model.Provider)
	}
	if resolved.Policy.Meta.MaxTools != 10 {
		t.Fatalf("expected agent override for meta.max_tools, got %d", resolved.Policy.Meta.MaxTools)
	}
	if got := resolved.Tools[0].Name; got != "run_tests" {
		t.Fatalf("unexpected first tool: %q", got)
	}
	if got := resolved.Hooks[1].Handler; got != "ensure_ci" {
		t.Fatalf("unexpected second hook: %q", got)
	}
}
