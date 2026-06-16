package agent

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel/trace"

	"github.com/htekdev/ai-harness/completion"
	"github.com/htekdev/ai-harness/hooks"
	"github.com/htekdev/ai-harness/tools"
)

// StopDelegateHandler executes a hook-requested delegation emitted from the
// agent.stop lifecycle event.
type StopDelegateHandler func(ctx context.Context, request any) (*TurnResult, error)

// StopPayload is the structured payload dispatched to agent.stop hooks before
// the agent accepts a final no-tool-call response and exits the loop.
type StopPayload struct {
	ID           string           `json:"id,omitempty"`
	ParentID     string           `json:"parent_id,omitempty"`
	FinishReason string           `json:"finish_reason,omitempty"`
	Iteration    int              `json:"iteration,omitempty"`
	Response     string           `json:"response,omitempty"`
	ToolCalls    []tools.Call     `json:"tool_calls,omitempty"`
	ToolResults  []tools.Result   `json:"tool_results,omitempty"`
	Usage        completion.Usage `json:"usage,omitempty"`
}

func (a *Agent) handleAgentStop(ctx context.Context, payload StopPayload, result *TurnResult) error {
	hookResult := a.hooks.Dispatch(ctx, hooks.EventAgentStop, &payload)
	if hookResult.Action == hooks.ActionBlock {
		return fmt.Errorf("agent stop blocked: %s", hookResult.Reason)
	}
	if hookResult.Action == hooks.ActionModify {
		if err := applyHookPayload(hookResult.Payload, &payload); err != nil {
			return fmt.Errorf("agent.stop modify payload: %w", err)
		}
	}
	if hookResult.Action == hooks.ActionDelegate {
		if a.stopDelegate == nil {
			return fmt.Errorf("agent.stop requested delegation but no stop delegate handler is configured")
		}
		delegated, err := a.stopDelegate(ctx, hookResult.Delegate)
		if err != nil {
			return err
		}
		if delegated != nil {
			result.Response = delegated.Response
			result.ToolCalls = append(result.ToolCalls, delegated.ToolCalls...)
			result.ToolResults = append(result.ToolResults, delegated.ToolResults...)
			result.Usage.PromptTokens += delegated.Usage.PromptTokens
			result.Usage.CompletionTokens += delegated.Usage.CompletionTokens
			result.Usage.TotalTokens += delegated.Usage.TotalTokens
		}
		return nil
	}

	result.Response = payload.Response
	result.ToolCalls = payload.ToolCalls
	result.ToolResults = payload.ToolResults
	result.Usage = payload.Usage
	return nil
}

func newStopPayload(ctx context.Context, finishReason string, iteration int, result *TurnResult) StopPayload {
	payload := StopPayload{
		FinishReason: finishReason,
		Iteration:    iteration,
		Response:     result.Response,
		ToolCalls:    append([]tools.Call(nil), result.ToolCalls...),
		ToolResults:  append([]tools.Result(nil), result.ToolResults...),
		Usage:        result.Usage,
	}
	spanCtx := trace.SpanContextFromContext(ctx)
	if spanCtx.HasSpanID() {
		payload.ID = spanCtx.SpanID().String()
	} else {
		payload.ID = "agent-stop-" + uuid.NewString()
	}
	return payload
}
