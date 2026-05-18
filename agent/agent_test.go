package agent

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/htekdev/ai-harness/completion"
	agentctx "github.com/htekdev/ai-harness/context"
	"github.com/htekdev/ai-harness/hooks"
	"github.com/htekdev/ai-harness/tools"
)

func setupTestAgent(handler http.HandlerFunc) *Agent {
	server := httptest.NewServer(handler)

	client := completion.NewClient(completion.ClientConfig{
		BaseURL:    server.URL,
		APIKey:     "test-key",
		MaxRetries: 0,
	})

	registry := tools.NewRegistry()
	registry.Register(tools.Definition{
		Name:        "greet",
		Description: "Greet someone",
		Parameters:  []tools.Parameter{{Name: "name", Type: tools.TypeString, Required: true}},
	}, func(ctx context.Context, args json.RawMessage) (string, error) {
		var params struct{ Name string }
		json.Unmarshal(args, &params)
		return "Hello, " + params.Name + "!", nil
	})

	ctxMgr := agentctx.NewManager(agentctx.Config{
		SystemPrompt: "You are a test assistant.",
	})

	return New(Options{
		Client:  client,
		Tools:   registry,
		Context: ctxMgr,
	})
}

func TestRunSimpleResponse(t *testing.T) {
	agent := setupTestAgent(func(w http.ResponseWriter, r *http.Request) {
		resp := completion.Response{
			Choices: []completion.Choice{
				{Message: completion.Message{Role: completion.RoleAssistant, Content: "I'm here to help!"}, FinishReason: "stop"},
			},
			Usage: completion.Usage{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15},
		}
		json.NewEncoder(w).Encode(resp)
	})

	result, err := agent.Run(context.Background(), "Hello")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Response != "I'm here to help!" {
		t.Fatalf("unexpected response: %s", result.Response)
	}
	if result.Usage.TotalTokens != 15 {
		t.Fatalf("unexpected usage: %+v", result.Usage)
	}
}

func TestRunWithToolCall(t *testing.T) {
	callCount := 0
	agent := setupTestAgent(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		var resp completion.Response

		if callCount == 1 {
			// First call: model requests a tool call
			resp = completion.Response{
				Choices: []completion.Choice{
					{
						Message: completion.Message{
							Role: completion.RoleAssistant,
							ToolCalls: []completion.ToolCall{
								{
									ID:   "call_1",
									Type: "function",
									Function: completion.FunctionCall{
										Name:      "greet",
										Arguments: `{"name":"World"}`,
									},
								},
							},
						},
						FinishReason: "tool_calls",
					},
				},
			}
		} else {
			// Second call: model responds after tool result
			resp = completion.Response{
				Choices: []completion.Choice{
					{Message: completion.Message{Role: completion.RoleAssistant, Content: "I greeted the world for you!"}, FinishReason: "stop"},
				},
			}
		}
		json.NewEncoder(w).Encode(resp)
	})

	result, err := agent.Run(context.Background(), "Greet the world")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Response != "I greeted the world for you!" {
		t.Fatalf("unexpected response: %s", result.Response)
	}
	if len(result.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(result.ToolCalls))
	}
	if result.ToolCalls[0].Name != "greet" {
		t.Fatalf("unexpected tool name: %s", result.ToolCalls[0].Name)
	}
	if len(result.ToolResults) != 1 {
		t.Fatalf("expected 1 tool result, got %d", len(result.ToolResults))
	}
	if result.ToolResults[0].Content != "Hello, World!" {
		t.Fatalf("unexpected tool result: %s", result.ToolResults[0].Content)
	}
}

func TestRunWithHookBlock(t *testing.T) {
	agent := setupTestAgent(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("should not reach completion API when turn is blocked")
	})

	agent.Hooks().Register(hooks.Registration{
		Name:     "blocker",
		Event:    hooks.EventTurnStart,
		Priority: 1,
		Handler: func(ctx context.Context, event hooks.Event, payload any) hooks.Result {
			return hooks.Result{Action: hooks.ActionBlock, Reason: "testing block"}
		},
	})

	_, err := agent.Run(context.Background(), "blocked input")
	if err == nil {
		t.Fatal("expected error from hook block")
	}
}

func TestRunWithToolPreHookBlock(t *testing.T) {
	callCount := 0
	agent := setupTestAgent(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		var resp completion.Response
		if callCount == 1 {
			resp = completion.Response{
				Choices: []completion.Choice{
					{
						Message: completion.Message{
							Role: completion.RoleAssistant,
							ToolCalls: []completion.ToolCall{
								{ID: "call_1", Type: "function", Function: completion.FunctionCall{Name: "greet", Arguments: `{"name":"blocked"}`}},
							},
						},
						FinishReason: "tool_calls",
					},
				},
			}
		} else {
			resp = completion.Response{
				Choices: []completion.Choice{
					{Message: completion.Message{Role: completion.RoleAssistant, Content: "Tool was blocked"}, FinishReason: "stop"},
				},
			}
		}
		json.NewEncoder(w).Encode(resp)
	})

	agent.Hooks().Register(hooks.Registration{
		Name:  "tool-blocker",
		Event: hooks.EventToolPre,
		Handler: func(ctx context.Context, event hooks.Event, payload any) hooks.Result {
			return hooks.Result{Action: hooks.ActionBlock, Reason: "not permitted"}
		},
	})

	result, err := agent.Run(context.Background(), "do something")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.ToolResults) != 1 {
		t.Fatalf("expected 1 tool result, got %d", len(result.ToolResults))
	}
	if !result.ToolResults[0].IsError {
		t.Fatal("expected tool result to be an error (blocked)")
	}
}

func TestRunSession(t *testing.T) {
	agent := setupTestAgent(func(w http.ResponseWriter, r *http.Request) {})

	sessionStarted := false
	agent.Hooks().Register(hooks.Registration{
		Name:  "session-tracker",
		Event: hooks.EventSessionStart,
		Handler: func(ctx context.Context, event hooks.Event, payload any) hooks.Result {
			sessionStarted = true
			return hooks.Result{Action: hooks.ActionContinue}
		},
	})

	err := agent.RunSession(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !sessionStarted {
		t.Fatal("session.start hook was not called")
	}
}

func TestEndSession(t *testing.T) {
	agent := setupTestAgent(func(w http.ResponseWriter, r *http.Request) {})

	sessionEnded := false
	agent.Hooks().Register(hooks.Registration{
		Name:  "session-end-tracker",
		Event: hooks.EventSessionEnd,
		Handler: func(ctx context.Context, event hooks.Event, payload any) hooks.Result {
			sessionEnded = true
			return hooks.Result{Action: hooks.ActionContinue}
		},
	})

	agent.EndSession(context.Background())
	if !sessionEnded {
		t.Fatal("session.end hook was not called")
	}
}

func TestContextAccessors(t *testing.T) {
	agent := setupTestAgent(func(w http.ResponseWriter, r *http.Request) {})

	if agent.Context() == nil {
		t.Fatal("Context() should not be nil")
	}
	if agent.Tools() == nil {
		t.Fatal("Tools() should not be nil")
	}
	if agent.Hooks() == nil {
		t.Fatal("Hooks() should not be nil")
	}
}
