// Package agent provides the core agent loop for the AI harness.
// It orchestrates the turn-based conversation between user, model, and tools.
package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"reflect"

	"github.com/htekdev/ai-harness/completion"
	agentctx "github.com/htekdev/ai-harness/context"
	"github.com/htekdev/ai-harness/hooks"
	"github.com/htekdev/ai-harness/scripting"
	"github.com/htekdev/ai-harness/tools"
)

// DefaultMaxToolIterations is the default maximum number of tool-call loops before forcing a stop.
const DefaultMaxToolIterations = 20

// Agent is the core agent loop that orchestrates conversation turns.
type Agent struct {
	client            *completion.Client
	tools             *tools.Registry
	hooks             *hooks.System
	context           *agentctx.Manager
	logger            *log.Logger
	maxToolIterations int
}

// Options configures the agent.
type Options struct {
	Client  *completion.Client
	Tools   *tools.Registry
	Hooks   *hooks.System
	Context *agentctx.Manager
	Logger  *log.Logger
	// MaxToolIterations overrides the default cap on tool-call loops per turn.
	// 0 means use DefaultMaxToolIterations.
	MaxToolIterations int
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
		opts.Logger = log.Default()
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
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	rv := reflect.ValueOf(target)
	if rv.Kind() != reflect.Pointer || rv.IsNil() {
		return fmt.Errorf("target must be a non-nil pointer")
	}
	rv.Elem().Set(reflect.Zero(rv.Elem().Type()))
	return json.Unmarshal(data, target)
}

// Run executes a single turn: takes user input, runs the agent loop until
// the model produces a final response (no more tool calls).
func (a *Agent) Run(ctx context.Context, userMessage string) (*TurnResult, error) {
	turnCtx := hooks.WithDispatcher(scripting.WithTurnState(ctx), a.hooks)

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

	result := &TurnResult{}
	completed := false

	for iteration := 0; iteration < a.maxToolIterations; iteration++ {
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
			return nil, fmt.Errorf("completion error: %w", err)
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
			return nil, fmt.Errorf("no choices in response")
		}

		choice := resp.Choices[0]

		// If no tool calls, we have a final response
		if len(choice.Message.ToolCalls) == 0 {
			a.context.AddMessage(choice.Message)
			result.Response = choice.Message.Content
			completed = true
			break
		}

		// Add assistant message with tool calls to context
		a.context.AddMessage(choice.Message)

		// Execute each tool call
		for _, tc := range choice.Message.ToolCalls {
			call := tools.Call{
				ID:        tc.ID,
				Name:      tc.Function.Name,
				Arguments: json.RawMessage(tc.Function.Arguments),
			}

			// Fire tool.pre hook
			preResult := a.hooks.Dispatch(turnCtx, hooks.EventToolPre, &call)
			if preResult.Action == hooks.ActionBlock {
				result.ToolCalls = append(result.ToolCalls, call)
				// Tool was blocked — send error result back to model
				toolResult := tools.Result{
					CallID:  call.ID,
					Content: fmt.Sprintf("tool blocked: %s", preResult.Reason),
					IsError: true,
				}
				result.ToolResults = append(result.ToolResults, toolResult)
				a.context.AddMessage(completion.Message{
					Role:       completion.RoleTool,
					Content:    toolResult.Content,
					ToolCallID: call.ID,
				})
				continue
			}
			if preResult.Action == hooks.ActionModify {
				if err := applyHookPayload(preResult.Payload, &call); err != nil {
					return nil, fmt.Errorf("tool.pre modify payload: %w", err)
				}
			}

			result.ToolCalls = append(result.ToolCalls, call)

			// Execute the tool
			a.logger.Printf("executing tool: %s (call_id: %s)", call.Name, call.ID)
			toolResult := a.tools.Execute(turnCtx, call)

			// Fire tool.post hook
			postResult := a.hooks.Dispatch(turnCtx, hooks.EventToolPost, &toolResult)
			if postResult.Action == hooks.ActionBlock {
				toolResult.Content = fmt.Sprintf("tool result blocked: %s", postResult.Reason)
				toolResult.IsError = true
			}
			if postResult.Action == hooks.ActionModify {
				if err := applyHookPayload(postResult.Payload, &toolResult); err != nil {
					return nil, fmt.Errorf("tool.post modify payload: %w", err)
				}
			}

			result.ToolResults = append(result.ToolResults, toolResult)

			// Add tool result to context
			a.context.AddMessage(completion.Message{
				Role:       completion.RoleTool,
				Content:    toolResult.Content,
				ToolCallID: call.ID,
			})
		}
	}

	if !completed {
		return nil, fmt.Errorf("max tool iterations reached (%d)", a.maxToolIterations)
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
