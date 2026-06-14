// Package hooks provides a lifecycle hook system for the AI harness.
// Hooks allow governance, auditing, and modification of agent behavior
// at well-defined points in the execution lifecycle.
package hooks

import (
	"context"
	"fmt"
	"strings"
	"sync"
)

// Event represents a lifecycle event that hooks can subscribe to.
type Event string

const (
	EventSessionStart   Event = "session.start"
	EventSessionEnd     Event = "session.end"
	EventTurnStart      Event = "turn.start"
	EventTurnEnd        Event = "turn.end"
	EventToolPre        Event = "tool.pre"
	EventToolPost       Event = "tool.post"
	EventCompletionPre  Event = "completion.pre"
	EventCompletionPost Event = "completion.post"
	EventDelegatePre  Event = "delegation.pre"
	EventDelegatePost Event = "delegation.post"
	// EventDelegatePostVerify fires after EventDelegatePost when a
	// delegation specifies a `verify:` block (or any registered hook
	// listens on this event). Hooks may inspect the delegation Result
	// and return ActionBlock with a Reason to mark the delegation as
	// failing verification — the Delegator will then re-prompt the
	// same delegate (Ralph loop) up to MaxVerifyRetries with the
	// failure reason injected. See issue #103.
	EventDelegatePostVerify Event = "delegation.post_verify"
	EventError              Event = "error"
)

const CustomEventPrefix = "custom."

// MetaEventPrefix is the prefix for meta built-in events.
const MetaEventPrefix = "meta."

// Action determines what happens after a hook executes.
type Action int

const (
	// ActionContinue allows the operation to proceed normally.
	ActionContinue Action = iota
	// ActionBlock prevents the operation from executing.
	ActionBlock
	// ActionModify allows the hook to modify the payload before continuing.
	ActionModify
)

// Result is returned by a hook handler after execution.
type Result struct {
	Action  Action
	Payload any
	Reason  string
}

// Handler is a function that processes a hook event.
// It receives the event context and payload, and returns a result.
type Handler func(ctx context.Context, event Event, payload any) Result

// Registration represents a registered hook handler with metadata.
type Registration struct {
	Name     string
	Event    Event
	Priority int // Lower numbers execute first
	Handler  Handler
}

// System manages hook registrations and dispatching.
type System struct {
	mu       sync.RWMutex
	handlers map[Event][]Registration
}

// NewSystem creates a new hook system.
func NewSystem() *System {
	return &System{
		handlers: make(map[Event][]Registration),
	}
}

// ValidEvents returns the supported lifecycle events in registration order.
func ValidEvents() []Event {
	return []Event{
		EventSessionStart,
		EventSessionEnd,
		EventTurnStart,
		EventTurnEnd,
		EventToolPre,
		EventToolPost,
		EventCompletionPre,
		EventCompletionPost,
		EventDelegatePre,
		EventDelegatePost,
		EventDelegatePostVerify,
		EventError,
	}
}

// IsValidEvent reports whether the provided event name is supported.
func IsValidEvent(event string) bool {
	for _, valid := range ValidEvents() {
		if string(valid) == event {
			return true
		}
	}
	return IsCustomEvent(event)
}

// IsCustomEvent reports whether the event is a user-defined custom event.
func IsCustomEvent(event string) bool {
	event = strings.TrimSpace(event)
	if strings.HasPrefix(event, CustomEventPrefix) && len(event) > len(CustomEventPrefix) {
		return true
	}
	if strings.HasPrefix(event, MetaEventPrefix) && len(event) > len(MetaEventPrefix) {
		return true
	}
	return false
}

type dispatcherContextKey struct{}

// WithDispatcher stores the hook system on a context so scripts can emit custom events.
func WithDispatcher(ctx context.Context, system *System) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if system == nil {
		return ctx
	}
	return context.WithValue(ctx, dispatcherContextKey{}, system)
}

// DispatcherFromContext returns the hook system associated with the context, if any.
func DispatcherFromContext(ctx context.Context) *System {
	if ctx == nil {
		return nil
	}
	system, _ := ctx.Value(dispatcherContextKey{}).(*System)
	return system
}

// Register adds a hook handler for the given event.
func (s *System) Register(reg Registration) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.handlers[reg.Event] = append(s.handlers[reg.Event], reg)

	// Sort by priority (insertion sort — small lists)
	handlers := s.handlers[reg.Event]
	for i := 1; i < len(handlers); i++ {
		for j := i; j > 0 && handlers[j].Priority < handlers[j-1].Priority; j-- {
			handlers[j], handlers[j-1] = handlers[j-1], handlers[j]
		}
	}
}

// Unregister removes a hook handler by name and event.
func (s *System) Unregister(name string, event Event) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	handlers, ok := s.handlers[event]
	if !ok {
		return false
	}

	for i, h := range handlers {
		if h.Name == name {
			s.handlers[event] = append(handlers[:i], handlers[i+1:]...)
			return true
		}
	}
	return false
}

// Dispatch fires an event and runs all registered handlers in priority order.
// Returns the final result. If any handler blocks, dispatch stops immediately.
// If a handler modifies the payload, subsequent handlers see the modified version.
func (s *System) Dispatch(ctx context.Context, event Event, payload any) Result {
	ctx = WithDispatcher(ctx, s)

	s.mu.RLock()
	handlers := make([]Registration, len(s.handlers[event]))
	copy(handlers, s.handlers[event])
	s.mu.RUnlock()

	result := Result{Action: ActionContinue, Payload: payload}

	for _, reg := range handlers {
		r := reg.Handler(ctx, event, result.Payload)

		switch r.Action {
		case ActionBlock:
			return Result{
				Action:  ActionBlock,
				Payload: result.Payload,
				Reason:  fmt.Sprintf("blocked by hook %q: %s", reg.Name, r.Reason),
			}
		case ActionModify:
			result.Payload = r.Payload
			result.Action = ActionModify
		case ActionContinue:
			// nothing to do
		}
	}

	return result
}

// HandlersFor returns the registered handlers for an event (for inspection/testing).
func (s *System) HandlersFor(event Event) []Registration {
	s.mu.RLock()
	defer s.mu.RUnlock()

	handlers := s.handlers[event]
	out := make([]Registration, len(handlers))
	copy(out, handlers)
	return out
}
