package delegation

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/htekdev/ai-harness/completion"
	"github.com/htekdev/ai-harness/hooks"
	"github.com/htekdev/ai-harness/scripting"
)

func TestDelegator_DelegatePreHookCanBlock(t *testing.T) {
	hookSystem := hooks.NewSystem()
	hookSystem.Register(hooks.Registration{
		Name:  "delegate-blocker",
		Event: hooks.EventDelegatePre,
		Handler: func(ctx context.Context, event hooks.Event, payload any) hooks.Result {
			return hooks.Result{Action: hooks.ActionBlock, Reason: "delegation disabled"}
		},
	})

	d := NewDelegator(DelegatorConfig{
		Engine:     scripting.NewEngine(),
		HookSystem: hookSystem,
	})

	_, err := d.Execute(context.Background(), Request{
		Task: "do work",
		Tools: []ToolSpec{{
			Name:        "noop",
			Description: "No-op tool",
			Script:      "def run(args):\n    return \"ok\"",
		}},
	})
	if err == nil || err.Error() != "delegation blocked: blocked by hook \"delegate-blocker\": delegation disabled" {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDelegator_DelegateHooksCanModifyRequestAndResult(t *testing.T) {
	var capturedTask string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		var req completion.Request
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		capturedTask = req.Messages[len(req.Messages)-1].Content
		_ = json.NewEncoder(w).Encode(completion.Response{
			Choices: []completion.Choice{{
				Message:      completion.Message{Role: completion.RoleAssistant, Content: "delegate done"},
				FinishReason: "stop",
			}},
		})
	}))
	defer server.Close()

	hookSystem := hooks.NewSystem()
	hookSystem.Register(hooks.Registration{
		Name:  "delegate-pre-modifier",
		Event: hooks.EventDelegatePre,
		Handler: func(ctx context.Context, event hooks.Event, payload any) hooks.Result {
			req := *(payload.(*Request))
			req.Task = "rewritten delegate task"
			return hooks.Result{Action: hooks.ActionModify, Payload: req}
		},
	})
	hookSystem.Register(hooks.Registration{
		Name:  "delegate-post-modifier",
		Event: hooks.EventDelegatePost,
		Handler: func(ctx context.Context, event hooks.Event, payload any) hooks.Result {
			result := *(payload.(*Result))
			result.Response = result.Response + " (modified)"
			return hooks.Result{Action: hooks.ActionModify, Payload: result}
		},
	})

	d := NewDelegator(DelegatorConfig{
		Client: completion.NewClient(completion.ClientConfig{
			BaseURL: server.URL,
			APIKey:  "test-key",
		}),
		Engine:     scripting.NewEngine(),
		HookSystem: hookSystem,
	})

	result, err := d.Execute(context.Background(), Request{
		Task: "original task",
		Tools: []ToolSpec{{
			Name:        "noop",
			Description: "No-op tool",
			Script:      "def run(args):\n    return \"ok\"",
		}},
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if capturedTask != "rewritten delegate task" {
		t.Fatalf("captured task = %q", capturedTask)
	}
	if result.Response != "delegate done (modified)" {
		t.Fatalf("unexpected response: %q", result.Response)
	}
}
