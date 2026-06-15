// Package agent provides the core agent loop for the AI harness.
// It orchestrates the turn-based conversation between user, model, and tools.
package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"reflect"
	"strings"
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
	client                        *completion.Client
	tools                         *tools.Registry
	hooks                         *hooks.System
	context                       *agentctx.Manager
	logger                        *slog.Logger
	maxToolIterations             int
	maxEmptyToolCallContinuations int
	composer                      *artifact.Composer
	turnNumber                    int
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
	// MaxEmptyToolCallContinuations is the recovery budget for the
	// "smarter-model planning turn" failure mode: a model emits text
	// content with finish_reason="stop" and zero tool_calls — looking
	// like a final reply but actually being a mid-thought planning
	// utterance. When > 0, the loop nudges the model with a system
	// reminder ("act or finalize") up to this many consecutive times
	// before giving up and treating the response as terminal.
	//
	// Default 0 = disabled, which matches strict OpenAI/Claude spec
	// (text-only + finish_reason=stop = final answer). Set 1-3 for
	// reasoning-heavy models that pre-narrate before acting.
	MaxEmptyToolCallContinuations int
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
		client:                        opts.Client,
		tools:                         opts.Tools,
		hooks:                         opts.Hooks,
		context:                       opts.Context,
		logger:                        opts.Logger,
		maxToolIterations:             maxIter,
		maxEmptyToolCallContinuations: opts.MaxEmptyToolCallContinuations,
		composer:                      opts.Composer,
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

// truncateForLog clamps long strings for structured-log fields so we
// don't blow up log lines on very long assistant content. Used by the
// loop-exit diagnostic logging.
func truncateForLog(s string, max int) string {
	s = strings.ReplaceAll(s, "\n", "\\n")
	if len(s) <= max {
		return s
	}
	return s[:max] + "…"
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

	// Auto-continuation budget for finish_reason=length (truncation).
	// Mirrors copilot-agent-runtime's MAX_CONTINUATION_ATTEMPTS=3 in
	// chat-completion-client.ts:858. When the model is cut off mid-thought,
	// we inject a "please continue" user message and re-prompt instead of
	// silently shipping the truncated reply. Reset on any non-truncation
	// response so transient truncations don't accumulate across a turn.
	const maxTokenExhaustionContinuations = 3
	consecutiveTokenExhaustions := 0

	// Recovery budget for "model emitted planning text with no tool calls
	// and finish_reason=stop" — the smarter-model failure mode where a
	// reasoning-heavy model narrates intent ("I'll start by reading the
	// README, then check the package.json") and waits for confirmation
	// instead of acting. Strict OpenAI spec says this is a final reply,
	// but for reasoning models it's a mid-thought hand-off. Bounded;
	// disabled (0) by default. See Options.MaxEmptyToolCallContinuations.
	consecutiveEmptyToolCalls := 0
	consecutiveDegenerateToolCalls := 0

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

		// Truncation auto-continuation. When the model is cut off by
		// max_tokens (finish_reason="length") AND emits no tool calls, the
		// naive "no tool_calls = final reply" branch below would ship a
		// half-sentence ("hm let me try X") as a "successful" turn — which
		// is exactly the bug Hector reported testing @htekdevaiharness.
		//
		// Mirrors copilot-agent-runtime/chat-completion-client.ts:1908-1923:
		// inject a synthetic user message asking the model to continue, add
		// the partial assistant message to history first so the model has
		// context, and re-enter the loop. Bounded by
		// maxTokenExhaustionContinuations to prevent runaway loops on a
		// pathologically small max_tokens.
		if choice.FinishReason == "length" && len(choice.Message.ToolCalls) == 0 {
			if consecutiveTokenExhaustions+1 < maxTokenExhaustionContinuations {
				consecutiveTokenExhaustions++
				a.logger.Warn("completion truncated, injecting continuation prompt",
					"turn", a.turnNumber, "iteration", iteration,
					"attempt", consecutiveTokenExhaustions,
					"max_attempts", maxTokenExhaustionContinuations)
				// Preserve the partial assistant turn in history so the
				// model can resume coherently, then nudge it to continue.
				a.context.AddMessage(choice.Message)
				a.context.AddMessage(completion.Message{
					Role:    completion.RoleUser,
					Content: "Please continue from where you left off.",
				})
				continue
			}
			// Budget exhausted: fall through to typed error rather than
			// silently emit a half-truncated reply.
			a.logger.Error("completion truncated; continuation budget exhausted",
				"turn", a.turnNumber, "iteration", iteration,
				"attempts", consecutiveTokenExhaustions)
			return nil, errs.Retriable(errs.KindCompletion, "agent.completion",
				fmt.Errorf("response truncated repeatedly after %d continuations; raise model.max_tokens", consecutiveTokenExhaustions),
				"completion truncated by max_tokens")
		}
		// Any non-truncation response resets the truncation counter. Tool-
		// call rounds reset too, mirroring chat-completion-client.ts:1725.
		consecutiveTokenExhaustions = 0

		// Hector's "look deeper into finish_reason" mandate: do NOT collapse
		// every "no tool_calls" case into a single exit branch. Different
		// finish_reason values mean different things and demand different
		// recovery strategies. Mapping (assistant message has no parsed
		// tool_calls):
		//
		//   "tool_calls"     -> DEGENERATE. Provider asserted tool calls
		//                       but the parser surfaced none (streaming
		//                       merge bug, malformed args JSON, format
		//                       drift on a new model). ALWAYS retry with a
		//                       dedicated "emit your tool call" nudge —
		//                       independent of the planning-turn budget,
		//                       because this is a parser/wire-level
		//                       failure, not a behavioral one. Bounded by
		//                       maxDegenerateToolCallRetries to prevent
		//                       infinite loops if the provider keeps
		//                       lying.
		//   "stop"           -> Final answer (per OpenAI spec). For
		//   "end_turn"          reasoning-heavy models, this is also how
		//   "" (legacy)         a planning hand-off arrives ("Let me start
		//                       by reading the README, then..."). When
		//                       Options.MaxEmptyToolCallContinuations > 0,
		//                       nudge once-per-budget with "act or
		//                       finalize." Default 0 preserves strict
		//                       OpenAI behavior.
		//   "content_filter" -> Provider redacted the response. Surface
		//                       as an error so callers can react; do not
		//                       silently ship empty content.
		//   anything else    -> Treat as final answer with WARN log so
		//                       the next unknown finish_reason from a new
		//                       model surfaces in telemetry.
		const maxDegenerateToolCallRetries = 2
		if len(choice.Message.ToolCalls) == 0 {
			fr := choice.FinishReason

			// Case 1: provider claimed tool_calls but parser surfaced none.
			if fr == "tool_calls" {
				if consecutiveDegenerateToolCalls < maxDegenerateToolCallRetries {
					consecutiveDegenerateToolCalls++
					a.logger.Warn("finish_reason=tool_calls but no tool_calls parsed; nudging",
						"turn", a.turnNumber, "iteration", iteration,
						"attempt", consecutiveDegenerateToolCalls,
						"max_attempts", maxDegenerateToolCallRetries,
						"content_len", len(choice.Message.Content),
						"content_preview", truncateForLog(choice.Message.Content, 120))
					a.context.AddMessage(choice.Message)
					a.context.AddMessage(completion.Message{
						Role: completion.RoleUser,
						Content: "Your previous turn was marked as a tool call (finish_reason=tool_calls) " +
							"but no tool was actually emitted. Please call the appropriate tool now " +
							"with valid JSON arguments, or give a final answer if no tool is needed.",
					})
					continue
				}
				a.logger.Error("finish_reason=tool_calls degenerate; budget exhausted",
					"turn", a.turnNumber, "iteration", iteration,
					"attempts", consecutiveDegenerateToolCalls)
				return nil, errs.Retriable(errs.KindCompletion, "agent.completion",
					fmt.Errorf("provider repeatedly returned finish_reason=tool_calls with no parsed tool_calls (%d attempts)", consecutiveDegenerateToolCalls),
					"degenerate tool_calls response")
			}

			// Case 2: provider redacted the response.
			if fr == "content_filter" {
				a.logger.Error("completion blocked by content filter",
					"turn", a.turnNumber, "iteration", iteration,
					"content_len", len(choice.Message.Content))
				return nil, errs.Newf(errs.KindCompletion, "agent.completion",
					"completion blocked by provider content filter")
			}

			// Case 3: stop / end_turn / "" / unknown — treat as a (potential)
			// final answer. Apply the planning-turn nudge if configured.
			isStopLike := fr == "stop" || fr == "end_turn" || fr == ""
			if !isStopLike {
				a.logger.Warn("unknown finish_reason; treating as final response",
					"turn", a.turnNumber, "iteration", iteration,
					"finish_reason", fr,
					"content_len", len(choice.Message.Content))
			}

			if isStopLike &&
				strings.TrimSpace(choice.Message.Content) != "" &&
				a.maxEmptyToolCallContinuations > 0 &&
				consecutiveEmptyToolCalls < a.maxEmptyToolCallContinuations {
				consecutiveEmptyToolCalls++
				a.logger.Warn("text-only response with no tool calls; nudging agent",
					"turn", a.turnNumber, "iteration", iteration,
					"attempt", consecutiveEmptyToolCalls,
					"max_attempts", a.maxEmptyToolCallContinuations,
					"finish_reason", fr,
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

			// Diagnostic: every clean exit logs finish_reason + content
			// shape so a future "premature exit" report comes with data.
			a.logger.Info("turn complete: text-only response",
				"turn", a.turnNumber, "iteration", iteration,
				"finish_reason", fr,
				"content_len", len(choice.Message.Content),
				"empty_continuations_used", consecutiveEmptyToolCalls,
				"degenerate_retries_used", consecutiveDegenerateToolCalls)
			a.context.AddMessage(choice.Message)
			result.Response = choice.Message.Content
			completed = true
			break
		}
		// Tool-call response — reset all recovery counters.
		consecutiveEmptyToolCalls = 0
		consecutiveDegenerateToolCalls = 0

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
