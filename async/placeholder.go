package async

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

// State represents the lifecycle state of a Placeholder.
type State int32

const (
	// StatePending means the placeholder is queued but not yet running.
	StatePending State = iota
	// StateRunning means the underlying tool is being executed.
	StateRunning
	// StateComplete means execution finished successfully.
	StateComplete
	// StateError means execution finished with an error.
	StateError
	// StateCancelled means the placeholder was cancelled (e.g. by Race).
	StateCancelled
)

func (s State) String() string {
	switch s {
	case StatePending:
		return "pending"
	case StateRunning:
		return "running"
	case StateComplete:
		return "complete"
	case StateError:
		return "error"
	case StateCancelled:
		return "cancelled"
	default:
		return fmt.Sprintf("State(%d)", int(s))
	}
}

// Placeholder is a typed future for an async tool execution result.
// It is safe for concurrent use.
type Placeholder struct {
	id         string
	toolName   string
	launchedAt time.Time

	// state is accessed atomically (cast to int32).
	state atomic.Int32

	// done is closed exactly once when the placeholder is resolved.
	done     chan struct{}
	doneOnce sync.Once

	// mu guards result, err, and cancel fields.
	mu     sync.Mutex
	result string
	err    error
	cancel context.CancelFunc
}

// newPlaceholder creates a new Placeholder in StatePending.
func newPlaceholder(id, toolName string, cancel context.CancelFunc) *Placeholder {
	p := &Placeholder{
		id:         id,
		toolName:   toolName,
		launchedAt: time.Now(),
		done:       make(chan struct{}),
		cancel:     cancel,
	}
	return p
}

// ID returns the unique identifier for this placeholder.
func (p *Placeholder) ID() string { return p.id }

// ToolName returns the name of the tool being executed.
func (p *Placeholder) ToolName() string { return p.toolName }

// State returns the current lifecycle state.
func (p *Placeholder) State() State { return State(p.state.Load()) }

// LaunchedAt returns when the placeholder was created.
func (p *Placeholder) LaunchedAt() time.Time { return p.launchedAt }

// Done returns a channel that is closed when the placeholder is resolved.
func (p *Placeholder) Done() <-chan struct{} { return p.done }

// closeDone closes the done channel exactly once.
func (p *Placeholder) closeDone() {
	p.doneOnce.Do(func() { close(p.done) })
}

// Result returns the result string and error.
// Blocks until the placeholder is resolved or ctx is cancelled.
func (p *Placeholder) Result(ctx context.Context) (string, error) {
	select {
	case <-p.done:
	case <-ctx.Done():
		return "", wrap(KindTimeout, "context cancelled while waiting for result", ctx.Err())
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	st := State(p.state.Load())
	switch st {
	case StateCancelled:
		return "", ErrCancelled
	case StateError:
		return "", p.err
	default:
		return p.result, nil
	}
}

// markRunning transitions from StatePending → StateRunning.
func (p *Placeholder) markRunning() {
	p.state.CompareAndSwap(int32(StatePending), int32(StateRunning))
}

// resolve sets the result and transitions to StateComplete.
// If the placeholder was already cancelled, the result is discarded.
func (p *Placeholder) resolve(result string) {
	// Only transition if we're still in a live state.
	for {
		old := p.state.Load()
		s := State(old)
		if s == StateComplete || s == StateError || s == StateCancelled {
			return
		}
		if p.state.CompareAndSwap(old, int32(StateComplete)) {
			p.mu.Lock()
			p.result = result
			p.mu.Unlock()
			p.closeDone()
			return
		}
	}
}

// fail records an error and transitions to StateError.
// If the placeholder was already cancelled, the error is discarded.
func (p *Placeholder) fail(err error) {
	for {
		old := p.state.Load()
		s := State(old)
		if s == StateComplete || s == StateError || s == StateCancelled {
			return
		}
		if p.state.CompareAndSwap(old, int32(StateError)) {
			p.mu.Lock()
			p.err = err
			p.mu.Unlock()
			p.closeDone()
			return
		}
	}
}

// cancelPlaceholder transitions to StateCancelled and invokes the context cancel function.
func (p *Placeholder) cancelPlaceholder() {
	for {
		old := p.state.Load()
		s := State(old)
		if s == StateComplete || s == StateError || s == StateCancelled {
			return
		}
		if p.state.CompareAndSwap(old, int32(StateCancelled)) {
			p.mu.Lock()
			cf := p.cancel
			p.mu.Unlock()
			if cf != nil {
				cf()
			}
			p.closeDone()
			return
		}
	}
}

