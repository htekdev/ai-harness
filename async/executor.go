package async

import (
	"context"
	"encoding/json"
	"sync"
	"sync/atomic"

	"github.com/htekdev/ai-harness/tools"
)

// ToolCaller is the interface the executor uses to invoke tools.
// tools.Registry satisfies this interface via its Execute method.
type ToolCaller interface {
	Execute(ctx context.Context, call tools.Call) tools.Result
}

// Executor manages a pool of goroutines for async tool execution.
// It respects a dependency graph (DAG) so that a placeholder with
// depends_on=[refA] will not start until refA has resolved.
//
// All methods are safe for concurrent use.
type Executor struct {
	maxConcurrent int
	sem           chan struct{} // counting semaphore
	caller        ToolCaller
	graph         *Graph

	mu       sync.Mutex
	pending  []*Placeholder     // all placeholders launched this turn
	byID     map[string]*Placeholder
	nextSeq  atomic.Uint64
}

// NewExecutor creates an executor with the given max concurrency limit.
// maxConcurrent ≤ 0 means "no limit" (uses a large default).
func NewExecutor(maxConcurrent int, caller ToolCaller) *Executor {
	if maxConcurrent <= 0 {
		maxConcurrent = 64
	}
	return &Executor{
		maxConcurrent: maxConcurrent,
		sem:           make(chan struct{}, maxConcurrent),
		caller:        caller,
		graph:         NewGraph(),
		byID:          make(map[string]*Placeholder),
	}
}

// Launch dispatches an async tool call and returns a Placeholder immediately.
//
// deps is an optional slice of Placeholders that must complete before this
// task starts (dependency chain / DAG ordering). If any dependency fails or
// is cancelled, the dependent placeholder is also failed.
func (e *Executor) Launch(ctx context.Context, toolName string, args json.RawMessage, deps []*Placeholder) (*Placeholder, error) {
	// Build canonical dep ID list and validate.
	depIDs := make([]string, 0, len(deps))
	for _, d := range deps {
		if d == nil {
			continue
		}
		depIDs = append(depIDs, d.ID())
	}

	// Assign a unique ID.
	seq := e.nextSeq.Add(1)
	id := placeholderID(toolName, seq)

	// Register with graph (cycle detection).
	if err := e.graph.Add(id, depIDs); err != nil {
		return nil, err
	}

	// Create placeholder with a cancellable child context.
	childCtx, cancel := context.WithCancel(ctx)
	p := newPlaceholder(id, toolName, cancel)

	e.mu.Lock()
	e.pending = append(e.pending, p)
	e.byID[id] = p
	e.mu.Unlock()

	// Launch goroutine.
	go e.run(childCtx, p, toolName, args, deps)

	return p, nil
}

// run is the goroutine body for a single async task.
func (e *Executor) run(ctx context.Context, p *Placeholder, toolName string, args json.RawMessage, deps []*Placeholder) {
	// 1. Wait for all dependencies to complete.
	for _, dep := range deps {
		if dep == nil {
			continue
		}
		select {
		case <-dep.Done():
			// Check if dep failed or was cancelled.
			st := dep.State()
			if st == StateError || st == StateCancelled {
				_, depErr := dep.Result(ctx)
				p.fail(wrap(KindExecution, "dependency failed", depErr))
				e.graph.Remove(p.id)
				return
			}
		case <-ctx.Done():
			p.fail(wrap(KindCancelled, "context cancelled while waiting for dependency", ctx.Err()))
			e.graph.Remove(p.id)
			return
		}
	}

	// 2. Acquire semaphore slot.
	select {
	case e.sem <- struct{}{}:
		// acquired
	case <-ctx.Done():
		p.fail(wrap(KindCancelled, "context cancelled while waiting for semaphore", ctx.Err()))
		e.graph.Remove(p.id)
		return
	}
	defer func() { <-e.sem }()

	// 3. Mark running.
	p.markRunning()

	// 4. Execute the tool.
	call := tools.Call{
		ID:        p.id,
		Name:      toolName,
		Arguments: args,
	}
	result := e.caller.Execute(ctx, call)

	// 5. Resolve or fail the placeholder.
	if result.IsError {
		p.fail(newf(KindExecution, "tool %q returned error: %s", toolName, result.Content))
	} else {
		p.resolve(result.Content)
	}

	e.graph.Remove(p.id)
}

// Barrier waits for all launched-but-not-yet-resolved placeholders to complete.
// It is called by the agent loop at the turn boundary to drain any fire-and-forget
// async work before the next LLM completion.
//
// Barrier does NOT inject results into the conversation; scripts that want results
// should use Wait / WaitAll / Race explicitly. Barrier is purely a cleanup mechanism.
func (e *Executor) Barrier(ctx context.Context) {
	e.mu.Lock()
	snapshot := make([]*Placeholder, len(e.pending))
	copy(snapshot, e.pending)
	e.mu.Unlock()

	for _, p := range snapshot {
		select {
		case <-p.Done():
		case <-ctx.Done():
			return
		}
	}
}

// Pending returns the number of placeholders that have not yet resolved.
func (e *Executor) Pending() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	n := 0
	for _, p := range e.pending {
		st := p.State()
		if st == StatePending || st == StateRunning {
			n++
		}
	}
	return n
}

// Snapshot returns a copy of all launched placeholders for inspection.
func (e *Executor) Snapshot() []*Placeholder {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := make([]*Placeholder, len(e.pending))
	copy(out, e.pending)
	return out
}

// placeholderID produces a deterministic ID string.
func placeholderID(toolName string, seq uint64) string {
	// Format: async_<toolName>_<seq>
	return "async_" + toolName + "_" + uint64ToString(seq)
}

func uint64ToString(n uint64) string {
	if n == 0 {
		return "0"
	}
	buf := [20]byte{}
	pos := len(buf)
	for n > 0 {
		pos--
		buf[pos] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[pos:])
}
