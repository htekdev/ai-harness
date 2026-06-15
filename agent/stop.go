package agent

import (
	"context"
	"strings"

	"github.com/htekdev/ai-harness/hooks"
	"github.com/htekdev/ai-harness/scripting"
)

const (
	// StopDecisionAllow allows the current turn to exit.
	StopDecisionAllow = "allow"
	// StopDecisionBlock blocks exit and continues the loop with a follow-up prompt.
	StopDecisionBlock = "block"
)

const (
	exitPolicyNatural  = "natural"
	exitPolicyDoneTool = "done_tool"
	exitPolicyHook     = "hook"
	exitPolicyHybrid   = "hybrid"
)

// StopDecision captures whether the agent loop is allowed to exit after a
// natural model stop (no tool calls).
type StopDecision struct {
	Decision string `json:"decision"`
	Reason   string `json:"reason,omitempty"`
	Trace    string `json:"trace,omitempty"`
}

func (a *Agent) evaluateStopDecision(ctx context.Context, result *TurnResult) StopDecision {
	mode := strings.TrimSpace(a.exitPolicyMode)
	if mode == "" {
		mode = exitPolicyNatural
	}

	doneRequired := mode == exitPolicyDoneTool || mode == exitPolicyHybrid
	hookEnabled := mode == exitPolicyHook || mode == exitPolicyHybrid

	if doneRequired && !doneFlag(ctx) {
		return StopDecision{
			Decision: StopDecisionBlock,
			Reason:   "Call the `done` tool when you have completed the task.",
			Trace:    "exit_policy.done_tool",
		}
	}

	if hookEnabled {
		hookResult := a.hooks.Dispatch(ctx, hooks.EventAgentStop, result)
		if hookResult.Action == hooks.ActionBlock {
			return StopDecision{
				Decision: StopDecisionBlock,
				Reason:   hookResult.Reason,
				Trace:    "hook:agent.stop",
			}
		}
	}

	return StopDecision{Decision: StopDecisionAllow}
}

func doneFlag(ctx context.Context) bool {
	values, ok := scripting.TurnStateValues(ctx)
	if !ok {
		return false
	}
	v, ok := values[scripting.TurnStateAgentDoneFlagKey]
	if !ok {
		return false
	}
	flag, ok := v.(bool)
	return ok && flag
}
