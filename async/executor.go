package async

import (
	"context"
	"encoding/json"
	"sync"
)

// DefaultMaxConcurrent is the default goroutine pool size for RunGraph.
const DefaultMaxConcurrent = 8

// DispatchFunc is the function signature used to execute a single tool call.
// It receives the tool name and raw JSON arguments and returns the tool output.
type DispatchFunc func(ctx context.Context, tool string, args json.RawMessage) (string, error)

// Executor runs a Graph of Placeholder nodes using a bounded goroutine pool.
// It executes nodes in dependency order: when a node's dependencies are all
// Done it becomes eligible to run. Failure cascades to transitive dependents.
type Executor struct {
	maxConcurrent int
}

// NewExecutor creates an Executor with the specified pool size.
// If maxConcurrent is ≤ 0, DefaultMaxConcurrent is used.
func NewExecutor(maxConcurrent int) *Executor {
	if maxConcurrent <= 0 {
		maxConcurrent = DefaultMaxConcurrent
	}
	return &Executor{maxConcurrent: maxConcurrent}
}

// RunGraph executes all Pending nodes in g in dependency order using dispatch.
//
// It proceeds in rounds: each round collects all Pending nodes whose deps are
// Done, dispatches them in parallel (bounded by the pool size), then checks
// for the next wave. This continues until AllDone or no forward progress can
// be made. Nodes whose dep failed or was cancelled are themselves cancelled
// (failure cascades to transitive dependents).
//
// RunGraph honours ctx cancellation: if the context is cancelled while
// waiting for a goroutine slot, the corresponding placeholder is cancelled.
//
// The first dispatch error is recorded but execution continues for independent
// branches. RunGraph returns nil when the graph is fully resolved.
func (e *Executor) RunGraph(ctx context.Context, g *Graph, dispatch DispatchFunc) error {
	sem := make(chan struct{}, e.maxConcurrent)

	for {
		if g.AllDone() {
			break
		}

		ready := g.ReadyNodes()
		if len(ready) == 0 {
			// No ready nodes. If any Pending nodes have failed/cancelled deps,
			// cascade the cancellation so the graph can make progress.
			if cascadePending(g) {
				continue
			}
			// Truly stuck — no pending nodes and no ready ones.
			break
		}

		var wg sync.WaitGroup
		for _, p := range ready {
			if !p.trySetRunning() {
				continue // another goroutine beat us (shouldn't happen here but safe)
			}
			p := p
			wg.Add(1)
			go func() {
				defer wg.Done()

				// Acquire pool slot, respecting context cancellation.
				select {
				case sem <- struct{}{}:
					defer func() { <-sem }()
				case <-ctx.Done():
					p.setCancelled()
					return
				}

				result, err := dispatch(ctx, p.Tool, p.Args)
				if err != nil {
					p.setFailed(err)
				} else {
					p.setDone(result)
				}
			}()
		}
		// Wait for this wave to finish before scheduling the next one.
		wg.Wait()

		// After each wave, propagate failures to blocked dependents.
		cascadePending(g)
	}

	return nil
}

// cascadePending cancels any Pending node whose dependency is Failed or Cancelled.
// Returns true if at least one node was cancelled (meaning forward progress occurred).
func cascadePending(g *Graph) bool {
	changed := false
	for _, p := range g.Nodes() {
		if p.Status() != StatusPending {
			continue
		}
		for _, depID := range p.DepsOn {
			dep, ok := g.Get(depID)
			if !ok {
				continue
			}
			s := dep.Status()
			if s == StatusFailed || s == StatusCancelled {
				p.setCancelled()
				changed = true
				break
			}
		}
	}
	return changed
}
