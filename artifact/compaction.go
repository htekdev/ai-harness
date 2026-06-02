package artifact

import (
	"fmt"
	"strings"
	"time"
)

// CompactionDef defines compaction triggers, strategies, and retention policy.
type CompactionDef struct {
	Triggers   []CompactionTrigger           `yaml:"triggers,omitempty"`
	Retention  CompactionRetention           `yaml:"retention,omitempty"`
	Strategies map[string]CompactionStrategy `yaml:"strategies,omitempty"`
}

// CompactionTrigger selects a strategy when threshold conditions are met.
type CompactionTrigger struct {
	TokenThreshold float64 `yaml:"token_threshold,omitempty"`
	TurnThreshold  int     `yaml:"turn_threshold,omitempty"`
	Event          string  `yaml:"event,omitempty"`
	Strategy       string  `yaml:"strategy"`
}

// CompactionRetention defines what to preserve, summarize, or drop.
type CompactionRetention struct {
	AlwaysKeep []string `yaml:"always_keep,omitempty"`
	Summarize  []string `yaml:"summarize,omitempty"`
	Drop       []string `yaml:"drop,omitempty"`
}

// CompactionStrategy describes a strategy implementation.
type CompactionStrategy struct {
	Description string `yaml:"description,omitempty"`
	Prompt      string `yaml:"prompt,omitempty"`
}

// CompactionState is runtime input used to evaluate compaction triggers.
type CompactionState struct {
	TokenPercent float64
	TurnCount    int
	Event        string
}

// CompactionOutcome describes how compaction was applied.
type CompactionOutcome struct {
	Strategies []string
	AppliedAt  time.Time
	AlwaysKept []string
	Summarized []string
	Dropped    []string
}

// SelectStrategies returns triggered strategies in declared order.
func (c CompactionDef) SelectStrategies(state CompactionState) []string {
	out := make([]string, 0)
	seen := make(map[string]bool)
	for _, tr := range c.Triggers {
		if tr.Strategy == "" || seen[tr.Strategy] {
			continue
		}
		if tr.TokenThreshold > 0 && state.TokenPercent < tr.TokenThreshold {
			continue
		}
		if tr.TurnThreshold > 0 && state.TurnCount < tr.TurnThreshold {
			continue
		}
		if tr.Event != "" && tr.Event != state.Event {
			continue
		}
		out = append(out, tr.Strategy)
		seen[tr.Strategy] = true
	}
	return out
}

// Execute applies built-in strategies and returns a compaction outcome.
func (c CompactionDef) Execute(state CompactionState) CompactionOutcome {
	strategies := c.SelectStrategies(state)
	out := CompactionOutcome{
		Strategies: append([]string(nil), strategies...),
		AppliedAt:  time.Now().UTC(),
		AlwaysKept: append([]string(nil), c.Retention.AlwaysKeep...),
		Summarized: append([]string(nil), c.Retention.Summarize...),
		Dropped:    append([]string(nil), c.Retention.Drop...),
	}
	for _, s := range strategies {
		switch strings.ToLower(s) {
		case "truncate":
			// truncate is represented by retention.drop
		case "summarize":
			// summarize is represented by retention.summarize
		case "checkpoint":
			if !contains(out.AlwaysKept, "checkpoint_summary") {
				out.AlwaysKept = append(out.AlwaysKept, "checkpoint_summary")
			}
		}
	}
	return out
}

// ValidateCompaction performs type-specific validation for compaction artifacts.
func ValidateCompaction(a *Artifact) []string {
	issues := make([]string, 0)
	if len(a.Compaction.Triggers) == 0 {
		issues = append(issues, "compaction artifact must define at least one trigger")
	}
	if len(a.Compaction.Strategies) == 0 {
		issues = append(issues, "compaction artifact must define at least one strategy")
	}
	for i, tr := range a.Compaction.Triggers {
		if tr.Strategy == "" {
			issues = append(issues, fmt.Sprintf("triggers[%d].strategy is required", i))
		}
		if tr.TokenThreshold < 0 || tr.TokenThreshold > 1 {
			issues = append(issues, fmt.Sprintf("triggers[%d].token_threshold must be in [0,1]", i))
		}
		if tr.TokenThreshold == 0 && tr.TurnThreshold == 0 && tr.Event == "" {
			issues = append(issues, fmt.Sprintf("triggers[%d] must define token_threshold, turn_threshold, or event", i))
		}
	}
	if len(a.Models) > 0 {
		issues = append(issues, "compaction artifact should not define models")
	}
	if len(a.Tools) > 0 {
		issues = append(issues, "compaction artifact should not define tools")
	}
	if len(a.Hooks) > 0 {
		issues = append(issues, "compaction artifact should not define hooks")
	}
	return issues
}

func mergeCompaction(base, overlay CompactionDef) CompactionDef {
	out := base
	if len(overlay.Triggers) > 0 {
		out.Triggers = append(out.Triggers, overlay.Triggers...)
	}
	if len(overlay.Retention.AlwaysKeep) > 0 {
		out.Retention.AlwaysKeep = appendUnique(out.Retention.AlwaysKeep, overlay.Retention.AlwaysKeep...)
	}
	if len(overlay.Retention.Summarize) > 0 {
		out.Retention.Summarize = appendUnique(out.Retention.Summarize, overlay.Retention.Summarize...)
	}
	if len(overlay.Retention.Drop) > 0 {
		out.Retention.Drop = appendUnique(out.Retention.Drop, overlay.Retention.Drop...)
	}
	if out.Strategies == nil {
		out.Strategies = map[string]CompactionStrategy{}
	}
	for name, def := range overlay.Strategies {
		out.Strategies[name] = def
	}
	return out
}

func appendUnique(base []string, values ...string) []string {
	seen := make(map[string]bool, len(base))
	for _, v := range base {
		seen[v] = true
	}
	for _, v := range values {
		if v == "" || seen[v] {
			continue
		}
		base = append(base, v)
		seen[v] = true
	}
	return base
}

func contains(values []string, v string) bool {
	for _, cur := range values {
		if cur == v {
			return true
		}
	}
	return false
}
