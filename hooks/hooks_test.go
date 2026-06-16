package hooks

import (
	"context"
	"testing"
)

func TestRegisterAndDispatch(t *testing.T) {
	sys := NewSystem()

	called := false
	sys.Register(Registration{
		Name:     "test-hook",
		Event:    EventToolPre,
		Priority: 10,
		Handler: func(ctx context.Context, event Event, payload any) Result {
			called = true
			return Result{Action: ActionContinue}
		},
	})

	result := sys.Dispatch(context.Background(), EventToolPre, "test-payload")
	if !called {
		t.Fatal("hook handler was not called")
	}
	if result.Action != ActionContinue {
		t.Fatalf("expected ActionContinue, got %d", result.Action)
	}
}

func TestBlockAction(t *testing.T) {
	sys := NewSystem()

	sys.Register(Registration{
		Name:     "blocker",
		Event:    EventToolPre,
		Priority: 1,
		Handler: func(ctx context.Context, event Event, payload any) Result {
			return Result{Action: ActionBlock, Reason: "not allowed"}
		},
	})

	secondCalled := false
	sys.Register(Registration{
		Name:     "second",
		Event:    EventToolPre,
		Priority: 10,
		Handler: func(ctx context.Context, event Event, payload any) Result {
			secondCalled = true
			return Result{Action: ActionContinue}
		},
	})

	result := sys.Dispatch(context.Background(), EventToolPre, nil)
	if result.Action != ActionBlock {
		t.Fatalf("expected ActionBlock, got %d", result.Action)
	}
	if secondCalled {
		t.Fatal("second handler should not have been called after block")
	}
	if result.Reason == "" {
		t.Fatal("expected a reason for the block")
	}
}

func TestModifyAction(t *testing.T) {
	sys := NewSystem()

	sys.Register(Registration{
		Name:     "modifier",
		Event:    EventTurnStart,
		Priority: 1,
		Handler: func(ctx context.Context, event Event, payload any) Result {
			return Result{Action: ActionModify, Payload: "modified-payload"}
		},
	})

	var received any
	sys.Register(Registration{
		Name:     "receiver",
		Event:    EventTurnStart,
		Priority: 10,
		Handler: func(ctx context.Context, event Event, payload any) Result {
			received = payload
			return Result{Action: ActionContinue}
		},
	})

	result := sys.Dispatch(context.Background(), EventTurnStart, "original")
	if received != "modified-payload" {
		t.Fatalf("expected modified payload, got %v", received)
	}
	if result.Payload != "modified-payload" {
		t.Fatalf("expected final payload to be modified, got %v", result.Payload)
	}
}

func TestDelegateActionShortCircuits(t *testing.T) {
	sys := NewSystem()

	secondCalled := false
	sys.Register(Registration{
		Name:     "delegate",
		Event:    EventAgentStop,
		Priority: 1,
		Handler: func(ctx context.Context, event Event, payload any) Result {
			return Result{Action: ActionDelegate, Delegate: map[string]any{"task": "follow-up"}}
		},
	})
	sys.Register(Registration{
		Name:     "second",
		Event:    EventAgentStop,
		Priority: 10,
		Handler: func(ctx context.Context, event Event, payload any) Result {
			secondCalled = true
			return Result{Action: ActionContinue}
		},
	})

	result := sys.Dispatch(context.Background(), EventAgentStop, "done")
	if result.Action != ActionDelegate {
		t.Fatalf("expected ActionDelegate, got %d", result.Action)
	}
	if secondCalled {
		t.Fatal("second handler should not have been called after delegate")
	}
	request, ok := result.Delegate.(map[string]any)
	if !ok || request["task"] != "follow-up" {
		t.Fatalf("unexpected delegate payload: %#v", result.Delegate)
	}
}

func TestPriorityOrdering(t *testing.T) {
	sys := NewSystem()

	var order []string

	sys.Register(Registration{
		Name:     "third",
		Event:    EventSessionStart,
		Priority: 30,
		Handler: func(ctx context.Context, event Event, payload any) Result {
			order = append(order, "third")
			return Result{Action: ActionContinue}
		},
	})
	sys.Register(Registration{
		Name:     "first",
		Event:    EventSessionStart,
		Priority: 1,
		Handler: func(ctx context.Context, event Event, payload any) Result {
			order = append(order, "first")
			return Result{Action: ActionContinue}
		},
	})
	sys.Register(Registration{
		Name:     "second",
		Event:    EventSessionStart,
		Priority: 15,
		Handler: func(ctx context.Context, event Event, payload any) Result {
			order = append(order, "second")
			return Result{Action: ActionContinue}
		},
	})

	sys.Dispatch(context.Background(), EventSessionStart, nil)

	if len(order) != 3 {
		t.Fatalf("expected 3 handlers called, got %d", len(order))
	}
	if order[0] != "first" || order[1] != "second" || order[2] != "third" {
		t.Fatalf("unexpected order: %v", order)
	}
}

func TestUnregister(t *testing.T) {
	sys := NewSystem()

	sys.Register(Registration{
		Name:     "removable",
		Event:    EventToolPost,
		Priority: 1,
		Handler: func(ctx context.Context, event Event, payload any) Result {
			return Result{Action: ActionBlock, Reason: "should be removed"}
		},
	})

	removed := sys.Unregister("removable", EventToolPost)
	if !removed {
		t.Fatal("expected Unregister to return true")
	}

	result := sys.Dispatch(context.Background(), EventToolPost, nil)
	if result.Action != ActionContinue {
		t.Fatal("removed hook should not have fired")
	}
}

func TestUnregisterNonExistent(t *testing.T) {
	sys := NewSystem()
	removed := sys.Unregister("ghost", EventToolPre)
	if removed {
		t.Fatal("expected Unregister to return false for non-existent handler")
	}
}

func TestDispatchNoHandlers(t *testing.T) {
	sys := NewSystem()
	result := sys.Dispatch(context.Background(), EventSessionEnd, "data")
	if result.Action != ActionContinue {
		t.Fatal("dispatch with no handlers should return ActionContinue")
	}
	if result.Payload != "data" {
		t.Fatal("payload should pass through unchanged")
	}
}

func TestHandlersFor(t *testing.T) {
	sys := NewSystem()
	sys.Register(Registration{Name: "a", Event: EventToolPre, Priority: 1, Handler: func(ctx context.Context, e Event, p any) Result { return Result{} }})
	sys.Register(Registration{Name: "b", Event: EventToolPre, Priority: 2, Handler: func(ctx context.Context, e Event, p any) Result { return Result{} }})

	handlers := sys.HandlersFor(EventToolPre)
	if len(handlers) != 2 {
		t.Fatalf("expected 2 handlers, got %d", len(handlers))
	}
	if handlers[0].Name != "a" || handlers[1].Name != "b" {
		t.Fatalf("unexpected handler names: %s, %s", handlers[0].Name, handlers[1].Name)
	}
}

func TestValidEventsIncludesDelegateHooks(t *testing.T) {
	if !IsValidEvent(string(EventDelegatePre)) || !IsValidEvent(string(EventDelegatePost)) || !IsValidEvent(string(EventAgentStop)) {
		t.Fatalf("delegate events should be valid: %v", ValidEvents())
	}
}

func TestCustomEventsAreValid(t *testing.T) {
	if !IsValidEvent("custom.audit") {
		t.Fatal("custom events should be valid")
	}
	if IsValidEvent("custom.") {
		t.Fatal("custom event prefix alone should be invalid")
	}
}

func TestWithDispatcherStoresSystem(t *testing.T) {
	system := NewSystem()
	ctx := WithDispatcher(context.Background(), system)
	if DispatcherFromContext(ctx) != system {
		t.Fatal("expected dispatcher to round-trip through context")
	}
}
