package governance

import (
	"fmt"
	"strings"
)

// EnforcementAction is the governance action applied when a rule fires.
// The four progressive levels match the AI Harness enforcement model.
type EnforcementAction string

const (
	// ActionDeny blocks the current tool call or agent turn.
	ActionDeny EnforcementAction = "deny"
	// ActionRequire forces a specific tool call or parameter to be included.
	ActionRequire EnforcementAction = "require"
	// ActionInject injects additional context text into the model prompt.
	ActionInject EnforcementAction = "inject"
	// ActionRank reorders tool suggestions to bias without blocking.
	ActionRank EnforcementAction = "rank"
)

// EnforcementResult is the outcome of evaluating a set of rules against a
// tool call or agent turn. Multiple rules may fire and their results are
// merged in priority order (deny > require > inject > rank).
type EnforcementResult struct {
	// Allowed is false if any deny rule fired.
	Allowed bool
	// DenyReasons lists the explanations from all fired deny rules.
	DenyReasons []string
	// RequiredTools lists tool names that must be called (from require rules).
	RequiredTools []string
	// InjectedContext is additional context to prepend to the model prompt.
	// Multiple inject rules are concatenated with newlines.
	InjectedContext string
	// RankedTools is the preferred tool ordering from rank rules.
	// If multiple rank rules fire, only the first one is used.
	RankedTools []string
}

// EnforcementEngine evaluates workflow-level enforcement rules against a tool
// call context. It applies both state-based tool restrictions and trace rules.
type EnforcementEngine struct {
	evaluator *Evaluator
	trace     *TraceLog
}

// NewEnforcementEngine creates an enforcement engine backed by the given
// state machine evaluator and trace log.
func NewEnforcementEngine(ev *Evaluator, tr *TraceLog) *EnforcementEngine {
	return &EnforcementEngine{evaluator: ev, trace: tr}
}

// EvaluateToolCall checks whether the named tool may be called and applies any
// applicable trace rules. Returns an EnforcementResult summarising all active
// constraints.
func (e *EnforcementEngine) EvaluateToolCall(tool string, params map[string]any) EnforcementResult {
	result := EnforcementResult{Allowed: true}

	// 1. Per-state tool allow-list (Statewright-level enforcement).
	if !e.evaluator.IsToolAllowed(tool) {
		result.Allowed = false
		allowed := e.evaluator.AllowedTools()
		if allowed == nil {
			result.DenyReasons = append(result.DenyReasons,
				fmt.Sprintf("tool %q is blocked in state %q", tool, e.evaluator.CurrentState()))
		} else {
			result.DenyReasons = append(result.DenyReasons,
				fmt.Sprintf("tool %q is not in the allowed set for state %q; permitted tools: [%s]",
					tool, e.evaluator.CurrentState(), strings.Join(allowed, ", ")))
		}
	}

	// 2. Approval gate.
	if e.evaluator.RequiresApproval() {
		result.Allowed = false
		result.DenyReasons = append(result.DenyReasons,
			fmt.Sprintf("state %q requires human approval before any tool calls", e.evaluator.CurrentState()))
	}

	// 3. Shell command allow-list (when tool is a shell tool).
	if isShellTool(tool) {
		if cmd, ok := params["command"].(string); ok && cmd != "" {
			if !e.evaluator.IsCommandAllowed(cmd) {
				s := e.evaluator.CurrentStateConfig()
				result.Allowed = false
				result.DenyReasons = append(result.DenyReasons,
					fmt.Sprintf("command %q not in allowed_commands for state %q; permitted prefixes: [%s]",
						cmd, e.evaluator.CurrentState(), strings.Join(s.AllowedCommands, ", ")))
			}
		}
	}

	// 4. Trace rules (AI Harness temporal governance).
	if e.trace != nil {
		for _, rule := range e.evaluator.workflow.TraceRules {
			if rule.Scope != "" && rule.Scope != e.evaluator.CurrentState() {
				continue
			}
			if e.trace.MatchesPattern(rule.Pattern) {
				e.applyRule(rule, &result)
			}
		}
	}

	return result
}

// applyRule adds a fired rule's effect to the enforcement result.
func (e *EnforcementEngine) applyRule(rule *TraceRule, result *EnforcementResult) {
	switch rule.Enforcement {
	case ActionDeny:
		result.Allowed = false
		msg := rule.Payload
		if msg == "" {
			msg = fmt.Sprintf("trace rule %q denied this action", rule.ID)
		}
		result.DenyReasons = append(result.DenyReasons, msg)
	case ActionRequire:
		if rule.Payload != "" {
			result.RequiredTools = append(result.RequiredTools, rule.Payload)
		}
	case ActionInject:
		if rule.Payload != "" {
			if result.InjectedContext != "" {
				result.InjectedContext += "\n"
			}
			result.InjectedContext += rule.Payload
		}
	case ActionRank:
		if len(result.RankedTools) == 0 && rule.Payload != "" {
			for _, t := range strings.Split(rule.Payload, ",") {
				if trimmed := strings.TrimSpace(t); trimmed != "" {
					result.RankedTools = append(result.RankedTools, trimmed)
				}
			}
		}
	}
}

// isShellTool returns true for tool names that execute shell commands and
// therefore need command-level filtering.
func isShellTool(name string) bool {
	switch strings.ToLower(name) {
	case "bash", "shell", "run", "exec":
		return true
	}
	return false
}
