// Package persistence provides an append-only event log for artifact/runtime state.
package persistence

import (
	"fmt"
	"sync"
	"time"
)

// Kind is a persistence event type.
type Kind string

const (
	KindArtifactUpsert Kind = "artifact.upsert"
	KindArtifactRemove Kind = "artifact.remove"

	KindRuntimeTurnStart    Kind = "runtime.turn.start"
	KindRuntimeTurnEnd      Kind = "runtime.turn.end"
	KindRuntimeHookDispatch Kind = "runtime.hook.dispatch"
	KindRuntimeContextBuilt Kind = "runtime.context.built"
)

// Event is a single append-only persistence record.
type Event struct {
	ID        string
	ParentID  string
	Branch    string
	Timestamp time.Time
	Kind      Kind

	Artifact *ArtifactChange
	Runtime  *RuntimeChange
}

// ArtifactChange describes artifact state mutation.
type ArtifactChange struct {
	Name    string
	Type    string
	Source  string
	Version string
	Active  bool
}

// RuntimeChange describes runtime state mutation.
type RuntimeChange struct {
	SessionID     string
	Turn          int
	HookEvent     string
	ContextTokens *int
}

// ArtifactState is the projected artifact state.
type ArtifactState struct {
	Name      string
	Type      string
	Source    string
	Version   string
	Active    bool
	UpdatedAt time.Time
}

// RuntimeState is the projected runtime state.
type RuntimeState struct {
	SessionID     string
	Turn          int
	LastHookEvent string
	ContextTokens int
	UpdatedAt     time.Time
}

// State is the projected state rebuilt by replaying events.
type State struct {
	Artifacts map[string]ArtifactState
	Runtime   RuntimeState
}

// EventLog stores append-only events and supports branch replay.
type EventLog struct {
	mu      sync.RWMutex
	order   []string
	events  map[string]Event
	heads   map[string]string
	childOf map[string][]string
}

// NewEventLog creates an empty append-only event log.
func NewEventLog() *EventLog {
	return &EventLog{
		order:   make([]string, 0),
		events:  make(map[string]Event),
		heads:   make(map[string]string),
		childOf: make(map[string][]string),
	}
}

// Append validates and appends an event.
func (l *EventLog) Append(evt Event) (Event, error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	if evt.ID == "" {
		return Event{}, fmt.Errorf("event id cannot be empty")
	}
	if _, exists := l.events[evt.ID]; exists {
		return Event{}, fmt.Errorf("event %q already exists", evt.ID)
	}
	if evt.Branch == "" {
		evt.Branch = "main"
	}
	if evt.Timestamp.IsZero() {
		evt.Timestamp = time.Now().UTC()
	}
	if evt.ParentID != "" {
		if _, ok := l.events[evt.ParentID]; !ok {
			return Event{}, fmt.Errorf("parent event %q not found", evt.ParentID)
		}
	} else if head, ok := l.heads[evt.Branch]; ok {
		evt.ParentID = head
	}

	l.events[evt.ID] = evt
	l.order = append(l.order, evt.ID)
	l.heads[evt.Branch] = evt.ID
	if evt.ParentID != "" {
		l.childOf[evt.ParentID] = append(l.childOf[evt.ParentID], evt.ID)
	}

	return evt, nil
}

// Events returns all events in append order.
func (l *EventLog) Events() []Event {
	l.mu.RLock()
	defer l.mu.RUnlock()

	out := make([]Event, 0, len(l.order))
	for _, id := range l.order {
		out = append(out, l.events[id])
	}
	return out
}

// Replay returns the event lineage for the branch head (oldest -> newest).
func (l *EventLog) Replay(branch string) ([]Event, error) {
	l.mu.RLock()
	defer l.mu.RUnlock()

	if branch == "" {
		branch = "main"
	}
	head, ok := l.heads[branch]
	if !ok {
		return nil, fmt.Errorf("branch %q has no events", branch)
	}
	return l.replayFromHeadLocked(head), nil
}

// Rebuild replays a branch head and projects current state.
func (l *EventLog) Rebuild(branch string) (State, error) {
	events, err := l.Replay(branch)
	if err != nil {
		return State{}, err
	}
	return Rebuild(events), nil
}

func (l *EventLog) replayFromHeadLocked(head string) []Event {
	eventsFromHead := make([]Event, 0)
	current := head
	for current != "" {
		evt := l.events[current]
		eventsFromHead = append(eventsFromHead, evt)
		current = evt.ParentID
	}

	for i, j := 0, len(eventsFromHead)-1; i < j; i, j = i+1, j-1 {
		eventsFromHead[i], eventsFromHead[j] = eventsFromHead[j], eventsFromHead[i]
	}
	return eventsFromHead
}

// Rebuild projects state by replaying the provided events in order.
func Rebuild(events []Event) State {
	state := State{
		Artifacts: make(map[string]ArtifactState),
	}

	for _, evt := range events {
		switch evt.Kind {
		case KindArtifactUpsert:
			if evt.Artifact == nil || evt.Artifact.Name == "" {
				continue
			}
			state.Artifacts[evt.Artifact.Name] = ArtifactState{
				Name:      evt.Artifact.Name,
				Type:      evt.Artifact.Type,
				Source:    evt.Artifact.Source,
				Version:   evt.Artifact.Version,
				Active:    evt.Artifact.Active,
				UpdatedAt: evt.Timestamp,
			}
		case KindArtifactRemove:
			if evt.Artifact == nil || evt.Artifact.Name == "" {
				continue
			}
			delete(state.Artifacts, evt.Artifact.Name)
		case KindRuntimeTurnStart, KindRuntimeTurnEnd, KindRuntimeHookDispatch, KindRuntimeContextBuilt:
			if evt.Runtime == nil {
				continue
			}
			if evt.Runtime.SessionID != "" {
				state.Runtime.SessionID = evt.Runtime.SessionID
			}
			if evt.Runtime.Turn > 0 {
				state.Runtime.Turn = evt.Runtime.Turn
			}
			if evt.Runtime.HookEvent != "" {
				state.Runtime.LastHookEvent = evt.Runtime.HookEvent
			}
			if evt.Runtime.ContextTokens != nil {
				state.Runtime.ContextTokens = *evt.Runtime.ContextTokens
			}
			state.Runtime.UpdatedAt = evt.Timestamp
		}
	}

	return state
}
