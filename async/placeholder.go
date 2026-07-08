package async

import (
	"encoding/json"
	"sync"
)

// Status is the lifecycle state of a Placeholder.
type Status int

const (
	// StatusPending means the placeholder has been registered but not yet started.
	StatusPending Status = iota
	// StatusRunning means the placeholder's tool dispatch is in progress.
	StatusRunning
	// StatusDone means the tool dispatch completed successfully.
	StatusDone
	// StatusFailed means the tool dispatch returned an error.
	StatusFailed
	// StatusCancelled means the placeholder was cancelled (e.g. a dependency failed).
	StatusCancelled
)

// String returns a human-readable name for the status.
func (s Status) String() string {
	switch s {
	case StatusPending:
		return "pending"
	case StatusRunning:
		return "running"
	case StatusDone:
		return "done"
	case StatusFailed:
		return "failed"
	case StatusCancelled:
		return "cancelled"
	default:
		return "unknown"
	}
}

// Placeholder is a typed reference to a pending async tool execution.
// It is created by Graph.Add and transitions through the status lifecycle:
//
//	Pending → Running → Done | Failed | Cancelled
//
// Placeholder is safe for concurrent use.
type Placeholder struct {
	// ID is the unique identifier within the graph.
	ID string
	// Tool is the name of the tool to be dispatched.
	Tool string
	// Args is the raw JSON arguments for the tool call.
	Args json.RawMessage
	// DepsOn holds IDs of placeholders that must complete before this one runs.
	DepsOn []string

	mu       sync.Mutex
	status   Status
	result   string
	err      error
	doneOnce sync.Once
	done     chan struct{}
}

func newPlaceholder(id, tool string, args json.RawMessage, depsOn []string) *Placeholder {
	return &Placeholder{
		ID:     id,
		Tool:   tool,
		Args:   args,
		DepsOn: depsOn,
		done:   make(chan struct{}),
	}
}

// Status returns the current lifecycle status.
func (p *Placeholder) Status() Status {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.status
}

// Result returns the tool output and any error.
// For pending/running placeholders this returns the zero values.
func (p *Placeholder) Result() (string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.result, p.err
}

// Done returns a channel that is closed when the placeholder reaches a terminal
// state (Done, Failed, or Cancelled).
func (p *Placeholder) Done() <-chan struct{} {
	return p.done
}

// trySetRunning transitions from Pending to Running atomically.
// Returns false if the placeholder is not in Pending state.
func (p *Placeholder) trySetRunning() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.status != StatusPending {
		return false
	}
	p.status = StatusRunning
	return true
}

// setDone transitions to Done and records the tool result.
func (p *Placeholder) setDone(result string) {
	p.mu.Lock()
	p.status = StatusDone
	p.result = result
	p.mu.Unlock()
	p.doneOnce.Do(func() { close(p.done) })
}

// setFailed transitions to Failed and records the error.
func (p *Placeholder) setFailed(err error) {
	p.mu.Lock()
	p.status = StatusFailed
	p.err = err
	p.mu.Unlock()
	p.doneOnce.Do(func() { close(p.done) })
}

// setCancelled transitions to Cancelled (idempotent).
func (p *Placeholder) setCancelled() {
	p.mu.Lock()
	if p.status == StatusPending || p.status == StatusRunning {
		p.status = StatusCancelled
	}
	p.mu.Unlock()
	p.doneOnce.Do(func() { close(p.done) })
}
