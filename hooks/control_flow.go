package hooks

import (
	"context"
	"fmt"
)

// DefaultControlFlowBudget caps how many hook-driven delegations may be
// chained from a single lifecycle decision before the runtime aborts.
const DefaultControlFlowBudget = 8

type controlFlowContextKey struct{}

type controlFlowState struct {
	remaining int
	seen      map[string]struct{}
}

// SeedControlFlow initializes control-flow tracking on the context and records
// the current request key without consuming budget.
func SeedControlFlow(ctx context.Context, key string, budget int) context.Context {
	state := controlFlowStateFromContext(ctx)
	if state == nil {
		if budget <= 0 {
			budget = DefaultControlFlowBudget
		}
		state = &controlFlowState{
			remaining: budget,
			seen:      make(map[string]struct{}),
		}
	}
	if key != "" {
		if _, ok := state.seen[key]; !ok {
			state = state.clone()
			state.seen[key] = struct{}{}
		}
	}
	return context.WithValue(ctx, controlFlowContextKey{}, state)
}

// AdvanceControlFlow records a hook-triggered delegation step, consuming one
// unit of budget and rejecting repeated keys as cycles.
func AdvanceControlFlow(ctx context.Context, key string, budget int) (context.Context, error) {
	state := controlFlowStateFromContext(ctx)
	if state == nil {
		ctx = SeedControlFlow(ctx, "", budget)
		state = controlFlowStateFromContext(ctx)
	}
	if state.remaining <= 0 {
		return ctx, fmt.Errorf("control-flow hook budget exhausted")
	}
	if key != "" {
		if _, ok := state.seen[key]; ok {
			return ctx, fmt.Errorf("control-flow hook cycle detected")
		}
	}
	next := state.clone()
	next.remaining--
	if key != "" {
		next.seen[key] = struct{}{}
	}
	return context.WithValue(ctx, controlFlowContextKey{}, next), nil
}

func controlFlowStateFromContext(ctx context.Context) *controlFlowState {
	if ctx == nil {
		return nil
	}
	state, _ := ctx.Value(controlFlowContextKey{}).(*controlFlowState)
	return state
}

// clone deep-copies the state so each derived context gets its own immutable
// control-flow snapshot, including an isolated seen-set map.
func (s *controlFlowState) clone() *controlFlowState {
	clone := &controlFlowState{
		remaining: s.remaining,
		seen:      make(map[string]struct{}, len(s.seen)),
	}
	for key := range s.seen {
		clone.seen[key] = struct{}{}
	}
	return clone
}
