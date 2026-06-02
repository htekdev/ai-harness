package artifact

import "testing"

func TestCompactionSelectStrategies(t *testing.T) {
	def := CompactionDef{
		Triggers: []CompactionTrigger{
			{TokenThreshold: 0.80, Strategy: "truncate"},
			{TokenThreshold: 0.90, Strategy: "summarize"},
			{TurnThreshold: 30, Strategy: "checkpoint"},
		},
	}

	got := def.SelectStrategies(CompactionState{TokenPercent: 0.95, TurnCount: 10})
	if len(got) != 2 || got[0] != "truncate" || got[1] != "summarize" {
		t.Fatalf("unexpected strategies: %+v", got)
	}
}

func TestCompactionExecuteRetention(t *testing.T) {
	def := CompactionDef{
		Triggers: []CompactionTrigger{
			{TokenThreshold: 0.90, Strategy: "summarize"},
			{TokenThreshold: 0.95, Strategy: "checkpoint"},
		},
		Retention: CompactionRetention{
			AlwaysKeep: []string{"system_prompt"},
			Summarize:  []string{"tool_results"},
			Drop:       []string{"exploratory_logs"},
		},
	}
	out := def.Execute(CompactionState{TokenPercent: 0.97})
	if len(out.Strategies) != 2 {
		t.Fatalf("expected 2 strategies, got %+v", out.Strategies)
	}
	if len(out.Dropped) != 1 || out.Dropped[0] != "exploratory_logs" {
		t.Fatalf("unexpected dropped retention: %+v", out.Dropped)
	}
	if !contains(out.AlwaysKept, "checkpoint_summary") {
		t.Fatalf("checkpoint should add checkpoint_summary retention: %+v", out.AlwaysKept)
	}
}
