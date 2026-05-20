package agent

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/htekdev/ai-harness/completion"
	"github.com/htekdev/ai-harness/hooks"
	"github.com/htekdev/ai-harness/tools"
)

func TestRunCompletionPreModifyHook(t *testing.T) {
	var toolsSeen int
	agent := setupTestAgent(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		var req completion.Request
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		toolsSeen = len(req.Tools)
		_ = json.NewEncoder(w).Encode(completion.Response{
			Choices: []completion.Choice{{
				Message:      completion.Message{Role: completion.RoleAssistant, Content: "done"},
				FinishReason: "stop",
			}},
		})
	})

	agent.Hooks().Register(hooks.Registration{
		Name:  "strip-tools",
		Event: hooks.EventCompletionPre,
		Handler: func(ctx context.Context, event hooks.Event, payload any) hooks.Result {
			req := *(payload.(*completion.Request))
			req.Tools = nil
			return hooks.Result{Action: hooks.ActionModify, Payload: req}
		},
	})

	if _, err := agent.Run(context.Background(), "hello"); err != nil {
		t.Fatalf("run: %v", err)
	}
	if toolsSeen != 0 {
		t.Fatalf("expected completion.pre hook to remove tools, saw %d", toolsSeen)
	}
}

func TestRunToolPreModifyHook(t *testing.T) {
	callCount := 0
	agent := setupTestAgent(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		var resp completion.Response
		if callCount == 1 {
			resp = completion.Response{Choices: []completion.Choice{{
				Message: completion.Message{Role: completion.RoleAssistant, ToolCalls: []completion.ToolCall{{
					ID:       "call_1",
					Type:     "function",
					Function: completion.FunctionCall{Name: "greet", Arguments: `{"name":"World"}`},
				}}},
				FinishReason: "tool_calls",
			}}}
		} else {
			resp = completion.Response{Choices: []completion.Choice{{
				Message:      completion.Message{Role: completion.RoleAssistant, Content: "done"},
				FinishReason: "stop",
			}}}
		}
		_ = json.NewEncoder(w).Encode(resp)
	})

	agent.Hooks().Register(hooks.Registration{
		Name:  "rewrite-tool-args",
		Event: hooks.EventToolPre,
		Handler: func(ctx context.Context, event hooks.Event, payload any) hooks.Result {
			call := *(payload.(*tools.Call))
			call.Arguments = json.RawMessage(`{"name":"Copilot"}`)
			return hooks.Result{Action: hooks.ActionModify, Payload: call}
		},
	})

	result, err := agent.Run(context.Background(), "greet")
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(result.ToolCalls) != 1 || string(result.ToolCalls[0].Arguments) != `{"name":"Copilot"}` {
		t.Fatalf("unexpected tool calls: %+v", result.ToolCalls)
	}
	if len(result.ToolResults) != 1 || result.ToolResults[0].Content != "Hello, Copilot!" {
		t.Fatalf("unexpected tool results: %+v", result.ToolResults)
	}
}

func TestRunToolPostAndTurnEndModifyHooks(t *testing.T) {
	callCount := 0
	agent := setupTestAgent(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		var resp completion.Response
		if callCount == 1 {
			resp = completion.Response{Choices: []completion.Choice{{
				Message: completion.Message{Role: completion.RoleAssistant, ToolCalls: []completion.ToolCall{{
					ID:       "call_1",
					Type:     "function",
					Function: completion.FunctionCall{Name: "greet", Arguments: `{"name":"World"}`},
				}}},
				FinishReason: "tool_calls",
			}}}
		} else {
			resp = completion.Response{Choices: []completion.Choice{{
				Message:      completion.Message{Role: completion.RoleAssistant, Content: "base response"},
				FinishReason: "stop",
			}}}
		}
		_ = json.NewEncoder(w).Encode(resp)
	})

	agent.Hooks().Register(hooks.Registration{
		Name:  "decorate-tool-result",
		Event: hooks.EventToolPost,
		Handler: func(ctx context.Context, event hooks.Event, payload any) hooks.Result {
			toolResult := *(payload.(*tools.Result))
			toolResult.Content = toolResult.Content + " [post-hook]"
			return hooks.Result{Action: hooks.ActionModify, Payload: toolResult}
		},
	})
	agent.Hooks().Register(hooks.Registration{
		Name:  "decorate-turn-result",
		Event: hooks.EventTurnEnd,
		Handler: func(ctx context.Context, event hooks.Event, payload any) hooks.Result {
			turn := *(payload.(*TurnResult))
			turn.Response = turn.Response + " [turn-end]"
			return hooks.Result{Action: hooks.ActionModify, Payload: turn}
		},
	})

	result, err := agent.Run(context.Background(), "greet")
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if result.ToolResults[0].Content != "Hello, World! [post-hook]" {
		t.Fatalf("unexpected tool result: %+v", result.ToolResults[0])
	}
	if result.Response != "base response [turn-end]" {
		t.Fatalf("unexpected response: %q", result.Response)
	}
}
