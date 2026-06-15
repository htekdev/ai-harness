package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/htekdev/ai-harness/completion"
	"github.com/htekdev/ai-harness/harness/errs"
	"github.com/htekdev/ai-harness/hooks"
	"github.com/htekdev/ai-harness/scripting"
	"github.com/htekdev/ai-harness/tools"
)

// StreamCallback is invoked for every text delta emitted by the model during
// a streaming turn. It runs synchronously on the agent goroutine, so callers
// must not block for long; for CLI use, write to stdout and flush.
//
// Tool-call deltas are intentionally NOT exposed through this callback: they
// arrive piecewise and are useless until assembled. Callers that want to
// surface tool activity should instead inspect the returned TurnResult or
// register a tool.pre / tool.post hook.
type StreamCallback func(delta string)

// RunStream is the streaming counterpart to Run. It uses CompleteStream +
// AssembleStream to surface text deltas to onDelta as they arrive, while
// preserving the full tool-call loop semantics of Run.
//
// Trade-offs vs Run:
//   - Token usage is not reported by most providers on streaming responses.
//     The returned TurnResult.Usage will be zero. Operators who need
//     accurate accounting should fall back to Run.
//   - completion.post hooks still fire, but with a synthesized Response
//     whose Usage is zero. Hooks that depend on usage values must handle
//     this case (or be disabled in stream mode).
//
// Everything else — tool execution, parallel calls, hooks, OTel spans,
// max-iteration cap, condition re-evaluation — matches Run exactly.
func (a *Agent) RunStream(ctx context.Context, userMessage string, onDelta StreamCallback) (result *TurnResult, err error) {
	turnCtx := hooks.WithDispatcher(scripting.WithTurnState(ctx), a.hooks)

	a.turnNumber++
	scripting.SetTurnState(turnCtx, "turn", a.turnNumber)

	turnCtx, span := otel.Tracer(tracerName).Start(turnCtx, "agent.turn",
		trace.WithAttributes(
			attribute.Int("turn.index", a.turnNumber),
			attribute.Int("turn.user_message_len", len(userMessage)),
			attribute.Bool("turn.streaming", true),
		),
	)
	var iterationsRun int
	defer func() {
		if result != nil {
			span.SetAttributes(
				attribute.Int("turn.iterations", iterationsRun),
				attribute.Int("turn.tool_calls", len(result.ToolCalls)),
			)
		}
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
		}
		span.End()
	}()

	if a.composer != nil {
		if cerr := a.composer.EvaluateConditions(turnCtx); cerr != nil {
			a.logger.Warn("artifact condition re-evaluation failed",
				"turn", a.turnNumber, "error", cerr)
		}
	}

	hookResult := a.hooks.Dispatch(turnCtx, hooks.EventTurnStart, userMessage)
	if hookResult.Action == hooks.ActionBlock {
		return nil, fmt.Errorf("turn blocked: %s", hookResult.Reason)
	}
	if hookResult.Action == hooks.ActionModify {
		if modified, ok := hookResult.Payload.(string); ok {
			userMessage = modified
		}
	}

	a.context.AddMessage(completion.Message{
		Role:    completion.RoleUser,
		Content: userMessage,
	})

	result = &TurnResult{}
	completed := false

	// See agent.go Run() — same auto-continuation budget on max_tokens
	// truncation. Mirrors copilot-agent-runtime MAX_CONTINUATION_ATTEMPTS=3.
	const maxTokenExhaustionContinuations = 3
	consecutiveTokenExhaustions := 0
	consecutiveEmptyToolCalls := 0

	for iteration := 0; iteration < a.maxToolIterations; iteration++ {
		iterationsRun = iteration + 1

		req := completion.Request{
			Messages: a.context.Messages(),
			Tools:    a.tools.ToOpenAIFormat(),
		}

		hookResult = a.hooks.Dispatch(turnCtx, hooks.EventCompletionPre, &req)
		if hookResult.Action == hooks.ActionBlock {
			return nil, fmt.Errorf("completion blocked: %s", hookResult.Reason)
		}
		if hookResult.Action == hooks.ActionModify {
			if err := applyHookPayload(hookResult.Payload, &req); err != nil {
				return nil, fmt.Errorf("completion.pre modify payload: %w", err)
			}
		}

		stream, err := a.client.CompleteStream(turnCtx, req)
		if err != nil {
			return nil, errs.Retriable(errs.KindCompletion, "agent.completion.stream", err, "stream open failed")
		}

		assembled, err := completion.AssembleStream(turnCtx, stream, completion.DeltaCallback(onDelta))
		if err != nil {
			return nil, errs.Retriable(errs.KindCompletion, "agent.completion.stream", err, "stream assembly failed")
		}

		// Synthesize a Response so completion.post hooks keep working.
		// Usage is intentionally zero — see method doc-comment.
		resp := &completion.Response{
			Choices: []completion.Choice{{
				Index:        0,
				Message:      assembled.Message,
				FinishReason: assembled.FinishReason,
			}},
		}

		hookResult = a.hooks.Dispatch(turnCtx, hooks.EventCompletionPost, resp)
		if hookResult.Action == hooks.ActionBlock {
			return nil, fmt.Errorf("completion blocked: %s", hookResult.Reason)
		}
		if hookResult.Action == hooks.ActionModify {
			if err := applyHookPayload(hookResult.Payload, resp); err != nil {
				return nil, fmt.Errorf("completion.post modify payload: %w", err)
			}
		}

		if len(resp.Choices) == 0 {
			return nil, errs.Newf(errs.KindCompletion, "agent.completion.stream", "no choices in response")
		}

		choice := resp.Choices[0]

		// Auto-continuation on truncation. See agent.go Run() for the full
		// rationale and copilot-agent-runtime parity reference.
		if choice.FinishReason == "length" && len(choice.Message.ToolCalls) == 0 {
			if consecutiveTokenExhaustions+1 < maxTokenExhaustionContinuations {
				consecutiveTokenExhaustions++
				a.logger.Warn("streaming completion truncated, injecting continuation prompt",
					"turn", a.turnNumber, "iteration", iteration,
					"attempt", consecutiveTokenExhaustions,
					"max_attempts", maxTokenExhaustionContinuations)
				a.context.AddMessage(choice.Message)
				a.context.AddMessage(completion.Message{
					Role:    completion.RoleUser,
					Content: "Please continue from where you left off.",
				})
				continue
			}
			a.logger.Error("streaming completion truncated; continuation budget exhausted",
				"turn", a.turnNumber, "iteration", iteration,
				"attempts", consecutiveTokenExhaustions)
			return nil, errs.Retriable(errs.KindCompletion, "agent.completion.stream",
				fmt.Errorf("response truncated repeatedly after %d continuations; raise model.max_tokens", consecutiveTokenExhaustions),
				"completion truncated by max_tokens")
		}
		consecutiveTokenExhaustions = 0

		if len(choice.Message.ToolCalls) == 0 {
			if choice.FinishReason == "stop" &&
				strings.TrimSpace(choice.Message.Content) != "" &&
				a.maxEmptyToolCallContinuations > 0 &&
				consecutiveEmptyToolCalls < a.maxEmptyToolCallContinuations {
				consecutiveEmptyToolCalls++
				a.logger.Warn("streaming text-only response with no tool calls; nudging agent",
					"turn", a.turnNumber, "iteration", iteration,
					"attempt", consecutiveEmptyToolCalls,
					"max_attempts", a.maxEmptyToolCallContinuations,
					"finish_reason", choice.FinishReason,
					"content_len", len(choice.Message.Content),
					"content_preview", truncateForLog(choice.Message.Content, 120))
				a.context.AddMessage(choice.Message)
				a.context.AddMessage(completion.Message{
					Role: completion.RoleUser,
					Content: "Either call the appropriate tool now to make progress, or " +
						"provide your complete final answer with no further planning preamble. " +
						"Do not narrate intent without acting.",
				})
				continue
			}
			a.logger.Info("turn complete: streaming text-only response",
				"turn", a.turnNumber, "iteration", iteration,
				"finish_reason", choice.FinishReason,
				"content_len", len(choice.Message.Content),
				"empty_continuations_used", consecutiveEmptyToolCalls)
			a.context.AddMessage(choice.Message)
			result.Response = choice.Message.Content
			completed = true
			break
		}
		consecutiveEmptyToolCalls = 0

		a.context.AddMessage(choice.Message)

		// Tool execution mirrors Run exactly (parallel, with pre/post hooks).
		type toolExecResult struct {
			index      int
			call       tools.Call
			toolResult tools.Result
			blocked    bool
			err        error
		}

		toolCalls := choice.Message.ToolCalls
		execResults := make([]toolExecResult, len(toolCalls))
		var wg sync.WaitGroup

		for i, tc := range toolCalls {
			call := tools.Call{
				ID:        tc.ID,
				Name:      tc.Function.Name,
				Arguments: json.RawMessage(tc.Function.Arguments),
			}
			execResults[i].index = i
			execResults[i].call = call

			wg.Add(1)
			go func(idx int, c tools.Call) {
				defer wg.Done()

				preResult := a.hooks.Dispatch(turnCtx, hooks.EventToolPre, &c)
				if preResult.Action == hooks.ActionBlock {
					execResults[idx].blocked = true
					execResults[idx].toolResult = tools.Result{
						CallID:  c.ID,
						Content: fmt.Sprintf("tool blocked: %s", preResult.Reason),
						IsError: true,
					}
					return
				}
				if preResult.Action == hooks.ActionModify {
					if err := applyHookPayload(preResult.Payload, &c); err != nil {
						execResults[idx].err = err
						return
					}
					execResults[idx].call = c
				}

				a.logger.Debug("executing tool", "tool", c.Name, "call_id", c.ID)
				toolResult := a.tools.Execute(turnCtx, c)

				postResult := a.hooks.Dispatch(turnCtx, hooks.EventToolPost, &toolResult)
				if postResult.Action == hooks.ActionBlock {
					toolResult.Content = fmt.Sprintf("tool result blocked: %s", postResult.Reason)
					toolResult.IsError = true
				}
				if postResult.Action == hooks.ActionModify {
					if err := applyHookPayload(postResult.Payload, &toolResult); err != nil {
						execResults[idx].err = err
						return
					}
				}

				execResults[idx].toolResult = toolResult
			}(i, call)
		}

		wg.Wait()

		for _, er := range execResults {
			if er.err != nil {
				return nil, er.err
			}
			result.ToolCalls = append(result.ToolCalls, er.call)
			result.ToolResults = append(result.ToolResults, er.toolResult)
			a.context.AddMessage(completion.Message{
				Role:       completion.RoleTool,
				Content:    er.toolResult.Content,
				ToolCallID: er.call.ID,
			})
		}
	}

	if !completed {
		return nil, errs.Newf(errs.KindCompletion, "agent.runstream", "max tool iterations reached (%d)", a.maxToolIterations)
	}

	hookResult = a.hooks.Dispatch(turnCtx, hooks.EventTurnEnd, result)
	if hookResult.Action == hooks.ActionBlock {
		return nil, fmt.Errorf("turn blocked: %s", hookResult.Reason)
	}
	if hookResult.Action == hooks.ActionModify {
		if err := applyHookPayload(hookResult.Payload, result); err != nil {
			return nil, fmt.Errorf("turn.end modify payload: %w", err)
		}
	}

	return result, nil
}
