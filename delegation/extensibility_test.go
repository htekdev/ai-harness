package delegation

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
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

func TestDelegator_DelegatePostHookCanChainDelegation(t *testing.T) {
	tool := ToolSpec{
		Name:        "noop",
		Description: "No-op tool",
		Script:      "def run(args):\n    return \"ok\"",
	}
	var tasks []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		var req completion.Request
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		task := req.Messages[len(req.Messages)-1].Content
		tasks = append(tasks, task)
		response := "delegate done"
		if task == "task two" {
			response = "follow-up done"
		}
		_ = json.NewEncoder(w).Encode(completion.Response{
			Choices: []completion.Choice{{
				Message:      completion.Message{Role: completion.RoleAssistant, Content: response},
				FinishReason: "stop",
			}},
		})
	}))
	defer server.Close()

	hookSystem := hooks.NewSystem()
	var firstID, secondParentID string
	hookSystem.Register(hooks.Registration{
		Name:  "capture-ids",
		Event: hooks.EventDelegatePre,
		Handler: func(ctx context.Context, event hooks.Event, payload any) hooks.Result {
			req := payload.(*Request)
			if req.Task == "task one" {
				firstID = req.ID
			}
			if req.Task == "task two" {
				secondParentID = req.ParentID
			}
			return hooks.Result{Action: hooks.ActionContinue}
		},
	})
	hookSystem.Register(hooks.Registration{
		Name:  "chain-second-delegate",
		Event: hooks.EventDelegatePost,
		Handler: func(ctx context.Context, event hooks.Event, payload any) hooks.Result {
			result := payload.(*Result)
			if result.ID == firstID {
				return hooks.Result{
					Action: hooks.ActionDelegate,
					Delegate: Request{
						Task:  "task two",
						Tools: []ToolSpec{tool},
					},
				}
			}
			return hooks.Result{Action: hooks.ActionContinue}
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
		Task:  "task one",
		Tools: []ToolSpec{tool},
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if firstID == "" {
		t.Fatal("expected first delegation request to have an id")
	}
	if secondParentID != firstID {
		t.Fatalf("expected chained delegation parent_id %q, got %q", firstID, secondParentID)
	}
	if len(tasks) != 2 || tasks[0] != "task one" || tasks[1] != "task two" {
		t.Fatalf("unexpected task sequence: %#v", tasks)
	}
	if result.Response != "follow-up done" {
		t.Fatalf("unexpected response: %q", result.Response)
	}
}

func TestDelegator_DelegatePostHookRejectsCycles(t *testing.T) {
	tool := ToolSpec{
		Name:        "noop",
		Description: "No-op tool",
		Script:      "def run(args):\n    return \"ok\"",
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
		Name:  "cycle",
		Event: hooks.EventDelegatePost,
		Handler: func(ctx context.Context, event hooks.Event, payload any) hooks.Result {
			return hooks.Result{
				Action: hooks.ActionDelegate,
				Delegate: Request{
					Task:  "loop forever",
					Tools: []ToolSpec{tool},
				},
			}
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

	_, err := d.Execute(context.Background(), Request{
		Task:  "loop forever",
		Tools: []ToolSpec{tool},
	})
	if err == nil || !strings.Contains(err.Error(), "control-flow hook cycle detected") {
		t.Fatalf("expected cycle-detected error, got %v", err)
	}
}

func TestDelegator_DelegatePostHookEnforcesBudget(t *testing.T) {
	tool := ToolSpec{
		Name:        "noop",
		Description: "No-op tool",
		Script:      "def run(args):\n    return \"ok\"",
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(completion.Response{
			Choices: []completion.Choice{{
				Message:      completion.Message{Role: completion.RoleAssistant, Content: "delegate done"},
				FinishReason: "stop",
			}},
		})
	}))
	defer server.Close()

	hookSystem := hooks.NewSystem()
	step := 0
	hookSystem.Register(hooks.Registration{
		Name:  "budget",
		Event: hooks.EventDelegatePost,
		Handler: func(ctx context.Context, event hooks.Event, payload any) hooks.Result {
			step++
			return hooks.Result{
				Action: hooks.ActionDelegate,
				Delegate: Request{
					Task:  fmt.Sprintf("step-%d", step),
					Tools: []ToolSpec{tool},
				},
			}
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

	_, err := d.Execute(context.Background(), Request{
		Task:  "start",
		Tools: []ToolSpec{tool},
	})
	if err == nil || !strings.Contains(err.Error(), "control-flow hook budget exhausted") {
		t.Fatalf("expected budget-exhausted error, got %v", err)
	}
}
