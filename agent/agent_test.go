package agent

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/htekdev/ai-harness/artifact"
	"github.com/htekdev/ai-harness/completion"
	agentctx "github.com/htekdev/ai-harness/context"
	"github.com/htekdev/ai-harness/hooks"
	"github.com/htekdev/ai-harness/scripting"
	"github.com/htekdev/ai-harness/tools"
)

func setupTestAgent(handler http.HandlerFunc) *Agent {
	return setupTestAgentWithOptions(handler, Options{})
}

func setupTestAgentWithOptions(handler http.HandlerFunc, opts Options) *Agent {
	server := httptest.NewServer(handler)

	client := completion.NewClient(completion.ClientConfig{
		BaseURL:    server.URL,
		APIKey:     "test-key",
		MaxRetries: 1,
	})

	registry := tools.NewRegistry()
	_ = registry.Register(tools.Definition{
		Name:        "greet",
		Description: "Greet someone",
		Parameters:  []tools.Parameter{{Name: "name", Type: tools.TypeString, Required: true}},
	}, func(ctx context.Context, args json.RawMessage) (string, error) {
		var params struct{ Name string }
		_ = json.Unmarshal(args, &params)
		return "Hello, " + params.Name + "!", nil
	})

	ctxMgr := agentctx.NewManager(agentctx.Config{SystemPrompt: "You are a test assistant."})

	opts.Client = client
	opts.Tools = registry
	opts.Context = ctxMgr
	return New(opts)
}

func TestRunSimpleResponse(t *testing.T) {
	agent := setupTestAgent(func(w http.ResponseWriter, r *http.Request) {
		resp := completion.Response{
			Choices: []completion.Choice{{Message: completion.Message{Role: completion.RoleAssistant, Content: "I'm here to help!"}, FinishReason: "stop"}},
			Usage:   completion.Usage{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15},
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

func TestRunTurnStartModifyHook(t *testing.T) {
	var captured string
	agent := setupTestAgent(func(w http.ResponseWriter, r *http.Request) {
		var req completion.Request
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		captured = req.Messages[len(req.Messages)-1].Content
		json.NewEncoder(w).Encode(completion.Response{Choices: []completion.Choice{{Message: completion.Message{Role: completion.RoleAssistant, Content: "modified"}, FinishReason: "stop"}}})
	})

	agent.Hooks().Register(hooks.Registration{
		Name:  "modifier",
		Event: hooks.EventTurnStart,
		Handler: func(ctx context.Context, event hooks.Event, payload any) hooks.Result {
			return hooks.Result{Action: hooks.ActionModify, Payload: "rewritten input"}
		},
	})

	_, err := agent.Run(context.Background(), "original input")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if captured != "rewritten input" {
		t.Fatalf("expected modified input, got %q", captured)
	}
}

func TestRunWithToolCall(t *testing.T) {
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
			resp = completion.Response{Choices: []completion.Choice{{Message: completion.Message{Role: completion.RoleAssistant, Content: "I greeted the world for you!"}, FinishReason: "stop"}}}
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
	if len(result.ToolCalls) != 1 || result.ToolCalls[0].Name != "greet" {
		t.Fatalf("unexpected tool calls: %+v", result.ToolCalls)
	}
	if len(result.ToolResults) != 1 || result.ToolResults[0].Content != "Hello, World!" {
		t.Fatalf("unexpected tool results: %+v", result.ToolResults)
	}
}

func TestRunMaxIterationsLimit(t *testing.T) {
	agent := setupTestAgent(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(completion.Response{Choices: []completion.Choice{{
			Message: completion.Message{Role: completion.RoleAssistant, ToolCalls: []completion.ToolCall{{
				ID:       "call_1",
				Type:     "function",
				Function: completion.FunctionCall{Name: "greet", Arguments: `{"name":"loop"}`},
			}}},
			FinishReason: "tool_calls",
		}}})
	})

	_, err := agent.Run(context.Background(), "loop forever")
	if err == nil {
		t.Fatal("expected max-iteration error")
	}
	if !strings.Contains(err.Error(), "max tool iterations reached") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunAgentStopBlocksThenAllows(t *testing.T) {
	callCount := 0
	agent := setupTestAgentWithOptions(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		_ = json.NewEncoder(w).Encode(completion.Response{Choices: []completion.Choice{{
			Message:      completion.Message{Role: completion.RoleAssistant, Content: "attempt"},
			FinishReason: "stop",
		}}})
	}, Options{ExitPolicyMode: "hook"})

	stopCount := 0
	agent.Hooks().Register(hooks.Registration{
		Name:  "stop-controller",
		Event: hooks.EventAgentStop,
		Handler: func(ctx context.Context, event hooks.Event, payload any) hooks.Result {
			stopCount++
			if stopCount <= 3 {
				return hooks.Result{Action: hooks.ActionBlock, Reason: "keep going"}
			}
			return hooks.Result{Action: hooks.ActionContinue}
		},
	})

	result, err := agent.Run(context.Background(), "run")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Response != "attempt" {
		t.Fatalf("unexpected response: %q", result.Response)
	}
	if callCount != 4 {
		t.Fatalf("expected 4 model calls, got %d", callCount)
	}
}

func TestRunAgentStopAlwaysBlockHonorsMaxIterations(t *testing.T) {
	callCount := 0
	agent := setupTestAgentWithOptions(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		_ = json.NewEncoder(w).Encode(completion.Response{Choices: []completion.Choice{{
			Message:      completion.Message{Role: completion.RoleAssistant, Content: "attempt"},
			FinishReason: "stop",
		}}})
	}, Options{MaxToolIterations: 3, ExitPolicyMode: "hook"})

	agent.Hooks().Register(hooks.Registration{
		Name:  "always-block",
		Event: hooks.EventAgentStop,
		Handler: func(ctx context.Context, event hooks.Event, payload any) hooks.Result {
			return hooks.Result{Action: hooks.ActionBlock, Reason: "not yet"}
		},
	})

	_, err := agent.Run(context.Background(), "run")
	if err == nil {
		t.Fatal("expected max-iterations error")
	}
	if !strings.Contains(err.Error(), "max tool iterations reached") {
		t.Fatalf("unexpected error: %v", err)
	}
	if callCount != 3 {
		t.Fatalf("expected 3 model calls, got %d", callCount)
	}
}

func TestRunDoneToolWithVerificationGateEndToEnd(t *testing.T) {
	callCount := 0
	agent := setupTestAgentWithOptions(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		var resp completion.Response
		if callCount == 1 {
			resp = completion.Response{Choices: []completion.Choice{{
				Message: completion.Message{Role: completion.RoleAssistant, ToolCalls: []completion.ToolCall{{
					ID:       "call_done",
					Type:     "function",
					Function: completion.FunctionCall{Name: "done", Arguments: `{"summary":"completed","claims":[{"type":"file","path":"README.md"}]}`},
				}}},
				FinishReason: "tool_calls",
			}}}
		} else {
			resp = completion.Response{Choices: []completion.Choice{{
				Message:      completion.Message{Role: completion.RoleAssistant, Content: "all done"},
				FinishReason: "stop",
			}}}
		}
		_ = json.NewEncoder(w).Encode(resp)
	}, Options{ExitPolicyMode: "hook"})

	_ = agent.Tools().Register(tools.Definition{
		Name:        "done",
		Description: "Signal completion",
		Parameters: []tools.Parameter{
			{Name: "summary", Type: tools.TypeString, Required: false},
			{Name: "claims", Type: tools.TypeArray, Required: false},
		},
	}, func(ctx context.Context, args json.RawMessage) (string, error) {
		var in map[string]any
		_ = json.Unmarshal(args, &in)
		scripting.SetTurnState(ctx, scripting.TurnStateAgentDoneFlagKey, true)
		scripting.SetTurnState(ctx, scripting.TurnStateAgentDoneSummaryKey, in["summary"])
		scripting.SetTurnState(ctx, scripting.TurnStateAgentDoneClaimsKey, in["claims"])
		scripting.SetTurnState(ctx, "agent.verification_result", map[string]any{"ok": true, "reason": ""})
		return `{"acknowledged":true}`, nil
	})

	agent.Hooks().Register(hooks.Registration{
		Name:  "done-verify-gate",
		Event: hooks.EventAgentStop,
		Handler: func(ctx context.Context, event hooks.Event, payload any) hooks.Result {
			state, ok := scripting.TurnStateValues(ctx)
			if !ok {
				return hooks.Result{Action: hooks.ActionBlock, Reason: "missing turn state"}
			}
			done, _ := state[scripting.TurnStateAgentDoneFlagKey].(bool)
			if !done {
				return hooks.Result{Action: hooks.ActionBlock, Reason: "call done first"}
			}
			verify, _ := state["agent.verification_result"].(map[string]any)
			okVerified, _ := verify["ok"].(bool)
			if !okVerified {
				return hooks.Result{Action: hooks.ActionBlock, Reason: "verification failed"}
			}
			return hooks.Result{Action: hooks.ActionContinue}
		},
	})

	result, err := agent.Run(context.Background(), "complete task")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Response != "all done" {
		t.Fatalf("unexpected response: %q", result.Response)
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
			resp = completion.Response{Choices: []completion.Choice{{
				Message:      completion.Message{Role: completion.RoleAssistant, ToolCalls: []completion.ToolCall{{ID: "call_1", Type: "function", Function: completion.FunctionCall{Name: "greet", Arguments: `{"name":"blocked"}`}}}},
				FinishReason: "tool_calls",
			}}}
		} else {
			resp = completion.Response{Choices: []completion.Choice{{Message: completion.Message{Role: completion.RoleAssistant, Content: "Tool was blocked"}, FinishReason: "stop"}}}
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
	if len(result.ToolResults) != 1 || !result.ToolResults[0].IsError {
		t.Fatalf("expected blocked tool result, got %+v", result.ToolResults)
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

	if err := agent.RunSession(context.Background()); err != nil {
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

func setupTestAgentWithComposer(handler http.HandlerFunc) (*Agent, *artifact.Registry) {
	server := httptest.NewServer(handler)
	client := completion.NewClient(completion.ClientConfig{
		BaseURL:    server.URL,
		APIKey:     "test-key",
		MaxRetries: 1,
	})
	toolReg := tools.NewRegistry()
	ctxMgr := agentctx.NewManager(agentctx.Config{SystemPrompt: "You are a test assistant."})
	artReg := artifact.NewRegistry()
	composer := artifact.NewComposer(artReg)
	return New(Options{Client: client, Tools: toolReg, Context: ctxMgr, Composer: composer}), artReg
}

func TestAgentLoop_ConditionReEval(t *testing.T) {
	a, artReg := setupTestAgentWithComposer(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(completion.Response{
			Choices: []completion.Choice{{Message: completion.Message{Role: completion.RoleAssistant, Content: "ok"}, FinishReason: "stop"}},
		})
	})

	cond := &artifact.Artifact{
		Metadata:  artifact.Metadata{Name: "turn-ctx", Type: artifact.TypePlugin, Description: "turn-aware plugin"},
		Condition: `ctx.get("turn", 0) >= 2`,
	}
	if err := artReg.Register(cond); err != nil {
		t.Fatal(err)
	}

	// Turn 1: condition false → inactive
	if _, err := a.Run(context.Background(), "turn 1"); err != nil {
		t.Fatalf("turn 1 failed: %v", err)
	}
	if cond.Active {
		t.Error("artifact should be inactive at turn 1")
	}

	// Turn 2: condition true → active
	if _, err := a.Run(context.Background(), "turn 2"); err != nil {
		t.Fatalf("turn 2 failed: %v", err)
	}
	if !cond.Active {
		t.Error("artifact should be active at turn 2")
	}
}

func TestAgentLoop_NoComposer_NoPanic(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(completion.Response{
			Choices: []completion.Choice{{Message: completion.Message{Role: completion.RoleAssistant, Content: "ok"}, FinishReason: "stop"}},
		})
	}))
	client := completion.NewClient(completion.ClientConfig{BaseURL: server.URL, APIKey: "test", MaxRetries: 1})
	ctxMgr := agentctx.NewManager(agentctx.Config{})
	a := New(Options{Client: client, Context: ctxMgr})
	// No composer — should not panic
	if _, err := a.Run(context.Background(), "hello"); err != nil {
		t.Fatalf("unexpected error without composer: %v", err)
	}
}
