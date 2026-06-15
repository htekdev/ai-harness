// Package agent provides the core agent loop for the AI harness.
// It orchestrates the turn-based conversation between user, model, and tools.
package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"reflect"
	"sync"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/htekdev/ai-harness/artifact"
	"github.com/htekdev/ai-harness/completion"
	agentctx "github.com/htekdev/ai-harness/context"
	"github.com/htekdev/ai-harness/harness/errs"
	"github.com/htekdev/ai-harness/hooks"
	"github.com/htekdev/ai-harness/scripting"
	"github.com/htekdev/ai-harness/tools"
)

// tracerName is the OTel instrumentation library name shared across packages.
const tracerName = "github.com/htekdev/ai-harness"

// DefaultMaxToolIterations is the default maximum number of tool-call loops before forcing a stop.
const DefaultMaxToolIterations = 20

// Agent is the core agent loop that orchestrates conversation turns.
type Agent struct {
	client            *completion.Client
	tools             *tools.Registry
	hooks             *hooks.System
	context           *agentctx.Manager
	logger            *slog.Logger
	maxToolIterations int
	composer          *artifact.Composer
	turnNumber        int
}

// Options configures the agent.
type Options struct {
	Client  *completion.Client
	Tools   *tools.Registry
	Hooks   *hooks.System
	Context *agentctx.Manager
	Logger  *slog.Logger
	// MaxToolIterations overrides the default cap on tool-call loops per turn.
	// 0 means use DefaultMaxToolIterations.
	MaxToolIterations int
	// Composer, if provided, will have EvaluateConditions called at the start of each turn.
	Composer *artifact.Composer
}

// New creates a new Agent with the given options.
func New(opts Options) *Agent {
	if opts.Hooks == nil {
		opts.Hooks = hooks.NewSystem()
	}
	if opts.Tools == nil {
		opts.Tools = tools.NewRegistry()
	}
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	maxIter := opts.MaxToolIterations
	if maxIter <= 0 {
		maxIter = DefaultMaxToolIterations
	}

	return &Agent{
		client:            opts.Client,
		tools:             opts.Tools,
		hooks:             opts.Hooks,
		context:           opts.Context,
		logger:            opts.Logger,
		maxToolIterations: maxIter,
		composer:          opts.Composer,
	}
}

// TurnResult represents the outcome of a single agent turn.
type TurnResult struct {
	// Response is the final assistant message content.
	Response string
	// ToolCalls contains all tool calls made during this turn.
	ToolCalls []tools.Call
	// ToolResults contains all tool execution results.
	ToolResults []tools.Result
	// Usage contains the aggregated token usage across all completions in this turn.
	Usage completion.Usage
}

func applyHookPayload(payload any, target any) error {
	rv := reflect.ValueOf(target)
	if rv.Kind() != reflect.Pointer || rv.IsNil() {
		return fmt.Errorf("target must be a non-nil pointer")
	}

	// If payload is a map (from Starlark scripts), normalize and merge onto target.
	// This preserves fields not specified in the hook payload (like CallID).
	if m, isMap := payload.(map[string]interface{}); isMap {
		normalized := normalizeHookPayload(m, target)
		data, err := json.Marshal(normalized)
		if err != nil {
			return err
		}
		return json.Unmarshal(data, target)
	}

	// If payload is a Go struct (from Go hooks), replace the target entirely.
	// This supports hooks that return modified copies of the full struct.
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	rv.Elem().Set(reflect.Zero(rv.Elem().Type()))
	return json.Unmarshal(data, target)
}

// normalizeHookPayload translates user-facing hook payload field names
// to the internal JSON struct field names expected by the target type.
// This allows hook scripts to use intuitive names like "result" instead
// of implementation details like "content".
func normalizeHookPayload(m map[string]interface{}, target any) any {
	// For tools.Result targets: translate "result" → "content"
	if _, isToolResult := target.(*tools.Result); isToolResult {
		if result, has := m["result"]; has {
			if _, hasContent := m["content"]; !hasContent {
				m["content"] = result
				delete(m, "result")
			}
		}
	}

	// For tools.Call targets: translate "arguments" from map to raw JSON
	if _, isToolCall := target.(*tools.Call); isToolCall {
		if args, has := m["arguments"]; has {
			switch v := args.(type) {
			case map[string]interface{}:
				data, err := json.Marshal(v)
				if err == nil {
					m["arguments"] = json.RawMessage(data)
				}
			}
		}
	}

	return m
}

// Run executes a single turn: takes user input, runs the agent loop until
// the model produces a final response (no more tool calls).
//
// Phase 5.2 (PR-B): each turn opens an `agent.turn` OTel span with attrs
//
//	turn.index             = monotonic per-agent turn counter (1-based)
//	turn.user_message_len  = len(userMessage) (in bytes; cheap)
//	turn.tool_calls        = number of tool calls executed across iterations
//	turn.iterations        = number of model completions consumed
//	turn.prompt_tokens
//	turn.completion_tokens
//	turn.total_tokens
//
// On error the span is marked with codes.Error and the underlying error is
// recorded via span.RecordError. Child spans (`tool.call`, `delegation.execute`)
// nest under this turn span automatically because turnCtx carries the span
// context through hooks/tools/delegation.
func (a *Agent) Run(ctx context.Context, userMessage string) (result *TurnResult, err error) {
	turnCtx := hooks.WithDispatcher(scripting.WithTurnState(ctx), a.hooks)

	// Increment turn counter and populate turn state
	a.turnNumber++
	scripting.SetTurnState(turnCtx, "turn", a.turnNumber)

	// Open the agent.turn span. Attributes that aren't known until completion
	// (token counts, iterations) are added in the deferred finalizer.
	turnCtx, span := otel.Tracer(tracerName).Start(turnCtx, "agent.turn",
		trace.WithAttributes(
			attribute.Int("turn.index", a.turnNumber),
			attribute.Int("turn.user_message_len", len(userMessage)),
		),
	)
	var iterationsRun int
	defer func() {
		if result != nil {
			span.SetAttributes(
				attribute.Int("turn.iterations", iterationsRun),
				attribute.Int("turn.tool_calls", len(result.ToolCalls)),
				attribute.Int("turn.prompt_tokens", result.Usage.PromptTokens),
				attribute.Int("turn.completion_tokens", result.Usage.CompletionTokens),
				attribute.Int("turn.total_tokens", result.Usage.TotalTokens),
			)
		}
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
		}
		span.End()
	}()

	// Re-evaluate artifact conditions against fresh turn state
	if a.composer != nil {
		if cerr := a.composer.EvaluateConditions(turnCtx); cerr != nil {
			a.logger.Warn("artifact condition re-evaluation failed",
				"turn", a.turnNumber, "error", cerr)
		}
	}

	// Fire turn.start hook
	hookResult := a.hooks.Dispatch(turnCtx, hooks.EventTurnStart, userMessage)
	if hookResult.Action == hooks.ActionBlock {
		return nil, fmt.Errorf("turn blocked: %s", hookResult.Reason)
	}
	if hookResult.Action == hooks.ActionModify {
		if modified, ok := hookResult.Payload.(string); ok {
			userMessage = modified
		}
	}

	// Add user message to context
	a.context.AddMessage(completion.Message{
		Role:    completion.RoleUser,
		Content: userMessage,
	})

	result = &TurnResult{}
	completed := false

	for iteration := 0; iteration < a.maxToolIterations; iteration++ {
		iterationsRun = iteration + 1
		// Build completion request
		req := completion.Request{
			Messages: a.context.Messages(),
			Tools:    a.tools.ToOpenAIFormat(),
		}

		// Fire completion.pre hook
		hookResult = a.hooks.Dispatch(turnCtx, hooks.EventCompletionPre, &req)
		if hookResult.Action == hooks.ActionBlock {
			return nil, fmt.Errorf("completion blocked: %s", hookResult.Reason)
		}
		if hookResult.Action == hooks.ActionModify {
			if err := applyHookPayload(hookResult.Payload, &req); err != nil {
				return nil, fmt.Errorf("completion.pre modify payload: %w", err)
			}
		}

		// Call the model
		resp, err := a.client.Complete(turnCtx, req)
		if err != nil {
			// Provider/network errors: classify as KindCompletion and mark
			// retriable so hooks/operators can implement backoff strategies.
			return nil, errs.Retriable(errs.KindCompletion, "agent.completion", err, "completion call failed")
		}

		// Fire completion.post hook
		hookResult = a.hooks.Dispatch(turnCtx, hooks.EventCompletionPost, resp)
		if hookResult.Action == hooks.ActionBlock {
			return nil, fmt.Errorf("completion blocked: %s", hookResult.Reason)
		}
		if hookResult.Action == hooks.ActionModify {
			if err := applyHookPayload(hookResult.Payload, resp); err != nil {
				return nil, fmt.Errorf("completion.post modify payload: %w", err)
			}
		}

		// Aggregate usage
		result.Usage.PromptTokens += resp.Usage.PromptTokens
		result.Usage.CompletionTokens += resp.Usage.CompletionTokens
		result.Usage.TotalTokens += resp.Usage.TotalTokens

		if len(resp.Choices) == 0 {
			return nil, errs.Newf(errs.KindCompletion, "agent.completion", "no choices in response")
		}

		choice := resp.Choices[0]

		// Detect degenerate / truncated completions BEFORE the standard
		// "text-only response = final answer" exit. Without this, a turn
		// truncated by max_tokens (finish_reason="length") looks identical
		// to a deliberate final reply: the model emitted some text, no
		// tool_calls, loop exits "complete" — and the user sees a partial
		// "let me try X" reply that never actually tries X.
		//
		// Finish-reason cases handled here:
		//   "length"     -> response truncated mid-tool-call by max_tokens.
		//                   Surface as a typed error so the caller knows to
		//                   raise max_tokens, not as a "successful" turn.
		//   "tool_calls" -> provider claimed tool calls but parsing produced
		//                   an empty slice. Almost always a streaming /
		//                   provider format mismatch worth retrying.
		// Only "stop" (and "" for non-conformant providers) plus empty
		// tool_calls is a legitimate final answer. Anything else with empty
		// tool_calls is degenerate and must NOT silently exit the loop —
		// see #104 for the agent.stop hook primitive that will eventually
		// let governance hooks decide.
		switch choice.FinishReason {
		case "length":
			a.logger.Warn("completion truncated by max_tokens",
				"turn", a.turnNumber, "iteration", iteration,
				"finish_reason", choice.FinishReason,
				"completion_tokens", resp.Usage.CompletionTokens)
			return nil, errs.Retriable(errs.KindCompletion, "agent.completion",
				fmt.Errorf("response truncated (finish_reason=length); raise model.max_tokens"),
				"completion truncated by max_tokens")
		case "tool_calls":
			if len(choice.Message.ToolCalls) == 0 {
				a.logger.Warn("provider reported tool_calls but parsed none",
					"turn", a.turnNumber, "iteration", iteration)
				return nil, errs.Retriable(errs.KindCompletion, "agent.completion",
					fmt.Errorf("finish_reason=tool_calls but no tool_calls parsed"),
					"degenerate tool_calls response")
			}
		case "stop", "end_turn", "":
			// fall through to final-response handling
		case "content_filter":
			a.logger.Warn("completion stopped by content filter",
				"turn", a.turnNumber, "iteration", iteration,
				"finish_reason", choice.FinishReason)
			return nil, errs.Newf(errs.KindCompletion, "agent.completion",
				"completion stopped by content filter")
		default:
			a.logger.Warn("unknown finish_reason; not treating as final answer",
				"turn", a.turnNumber, "iteration", iteration,
				"finish_reason", choice.FinishReason,
				"tool_calls", len(choice.Message.ToolCalls))
			if len(choice.Message.ToolCalls) == 0 {
				return nil, errs.Retriable(errs.KindCompletion, "agent.completion",
					fmt.Errorf("unrecognized finish_reason=%q with no tool_calls", choice.FinishReason),
					"unrecognized finish_reason with empty tool_calls")
			}
		}

		// Final response: emitted by the model with finish_reason=stop and
		// zero tool_calls. Anything else has been handled (or errored) above.
		if len(choice.Message.ToolCalls) == 0 {
			a.context.AddMessage(choice.Message)
			result.Response = choice.Message.Content
			completed = true
			break
		}

		// Add assistant message with tool calls to context
		a.context.AddMessage(choice.Message)

		// Execute tool calls in parallel
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

				// Fire tool.pre hook
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

				// Execute the tool
				a.logger.Debug("executing tool", "tool", c.Name, "call_id", c.ID)
				toolResult := a.tools.Execute(turnCtx, c)

				// Fire tool.post hook
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

		// Collect results in order and add to context
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
		return nil, errs.Newf(errs.KindCompletion, "agent.run", "max tool iterations reached (%d)", a.maxToolIterations)
	}

	// Fire turn.end hook
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

// RunSession starts a session, executing the session.start hook.
func (a *Agent) RunSession(ctx context.Context) error {
	result := a.hooks.Dispatch(ctx, hooks.EventSessionStart, nil)
	if result.Action == hooks.ActionBlock {
		return fmt.Errorf("session blocked: %s", result.Reason)
	}
	return nil
}

// EndSession fires the session.end hook.
func (a *Agent) EndSession(ctx context.Context) {
	a.hooks.Dispatch(ctx, hooks.EventSessionEnd, nil)
}

// Context returns the agent's context manager for inspection.
func (a *Agent) Context() *agentctx.Manager {
	return a.context
}

// Tools returns the agent's tool registry for inspection or modification.
func (a *Agent) Tools() *tools.Registry {
	return a.tools
}

// Hooks returns the agent's hook system for inspection or modification.
func (a *Agent) Hooks() *hooks.System {
	return a.hooks
}
