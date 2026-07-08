package scripting

import (
	"context"
	"encoding/json"
	"fmt"

	"go.starlark.net/starlark"
	"go.starlark.net/starlarkstruct"

	"github.com/htekdev/ai-harness/async"
)

// asyncTurnStateKey is the context key for the per-turn async state.
type asyncTurnStateKey struct{}

// asyncTurnState holds the per-turn async graph and dispatch function.
type asyncTurnState struct {
	graph    *async.Graph
	dispatch async.DispatchFunc
	executor *async.Executor
}

// WithAsyncState attaches a fresh async.Graph and dispatch function to the
// context. The agent loop calls this at the start of each turn.
//
// The dispatch function should call the tool registry and return the tool
// output string. It runs in a separate goroutine per placeholder, so it must
// be safe for concurrent use.
func WithAsyncState(ctx context.Context, dispatch async.DispatchFunc) context.Context {
	return context.WithValue(ctx, asyncTurnStateKey{}, &asyncTurnState{
		graph:    async.NewGraph(),
		dispatch: dispatch,
		executor: async.NewExecutor(async.DefaultMaxConcurrent),
	})
}

// FlushAsync runs the turn's async graph to completion (the Loop-Boundary
// Barrier). The agent loop calls this after the tool-call iteration loop
// completes, before firing the turn.end hook.
//
// Any placeholders still in Pending state are dispatched and awaited here.
// This is a no-op if no async state is present in ctx.
func FlushAsync(ctx context.Context) error {
	state := asyncStateFromContext(ctx)
	if state == nil || state.graph.Size() == 0 {
		return nil
	}
	return state.executor.RunGraph(ctx, state.graph, state.dispatch)
}

// AsyncGraph returns the per-turn async.Graph from ctx, or nil if not present.
func AsyncGraph(ctx context.Context) *async.Graph {
	state := asyncStateFromContext(ctx)
	if state == nil {
		return nil
	}
	return state.graph
}

func asyncStateFromContext(ctx context.Context) *asyncTurnState {
	if ctx == nil {
		return nil
	}
	s, _ := ctx.Value(asyncTurnStateKey{}).(*asyncTurnState)
	return s
}

// makeAsyncModule builds the async.* Starlark module.
// The builtins are always registered; they return an error at call time if
// no async state is present in the Starlark thread's context.
func (e *Engine) makeAsyncModule() *starlarkstruct.Struct {
	return starlarkstruct.FromStringDict(starlarkstruct.Default, starlark.StringDict{
		"launch":   starlark.NewBuiltin("async.launch", e.builtinAsyncLaunch),
		"wait_all": starlark.NewBuiltin("async.wait_all", e.builtinAsyncWaitAll),
		"race":     starlark.NewBuiltin("async.race", e.builtinAsyncRace),
	})
}

// --- async.launch ---

// builtinAsyncLaunch registers a tool call as an async placeholder.
//
// Starlark signature:
//
//	async.launch(tool, args, depends_on=[]) -> placeholder_id (string)
//
// Parameters:
//   - tool (str): name of the registered tool to call
//   - args (dict): tool arguments
//   - depends_on (list of str, optional): IDs of placeholders that must
//     complete before this one is eligible to run
//
// Returns the placeholder ID string. The tool is NOT executed immediately;
// execution happens when async.wait_all / async.race is called, or at the
// automatic Loop-Boundary Barrier at turn end.
func (e *Engine) builtinAsyncLaunch(thread *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	ctx := contextFromThread(thread)
	state := asyncStateFromContext(ctx)
	if state == nil {
		return nil, fmt.Errorf("async.launch: async module not available in this context")
	}

	var toolName string
	var argsDict *starlark.Dict
	var depsOnList *starlark.List

	if err := starlark.UnpackArgs("async.launch", args, kwargs,
		"tool", &toolName,
		"args", &argsDict,
		"depends_on?", &depsOnList,
	); err != nil {
		return nil, err
	}

	// Convert Starlark dict → JSON for the tool call.
	var argsJSON json.RawMessage
	if argsDict != nil {
		goArgs := starlarkToGo(argsDict)
		data, err := json.Marshal(goArgs)
		if err != nil {
			return nil, fmt.Errorf("async.launch: marshal args: %w", err)
		}
		argsJSON = data
	} else {
		argsJSON = json.RawMessage("{}")
	}

	// Collect dependency IDs.
	var depsOn []string
	if depsOnList != nil {
		for i := 0; i < depsOnList.Len(); i++ {
			s, ok := starlark.AsString(depsOnList.Index(i))
			if !ok {
				return nil, fmt.Errorf("async.launch: depends_on must contain strings, got %T", depsOnList.Index(i))
			}
			depsOn = append(depsOn, s)
		}
	}

	p, err := state.graph.Add(toolName, argsJSON, depsOn)
	if err != nil {
		return nil, fmt.Errorf("async.launch: %w", err)
	}

	return starlark.String(p.ID), nil
}

// --- async.wait_all ---

// builtinAsyncWaitAll executes all pending placeholders (honoring dependency
// order) and returns their results as a list.
//
// Starlark signature:
//
//	async.wait_all(refs) -> list of result strings
//
// Parameters:
//   - refs (list of str): placeholder IDs returned by async.launch
//
// Blocks until all referenced placeholders (and their transitive deps) are
// resolved. Returns a list of result strings in the same order as refs.
func (e *Engine) builtinAsyncWaitAll(thread *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	ctx := contextFromThread(thread)
	state := asyncStateFromContext(ctx)
	if state == nil {
		return nil, fmt.Errorf("async.wait_all: async module not available in this context")
	}

	var refsList *starlark.List
	if err := starlark.UnpackArgs("async.wait_all", args, kwargs,
		"refs", &refsList,
	); err != nil {
		return nil, err
	}

	refs, err := starlarkListToStrings("async.wait_all", refsList)
	if err != nil {
		return nil, err
	}

	results, err := async.WaitAll(ctx, state.graph, refs, state.dispatch)
	if err != nil {
		return nil, fmt.Errorf("async.wait_all: %w", err)
	}

	out := starlark.NewList(nil)
	for _, r := range results {
		if appendErr := out.Append(starlark.String(r)); appendErr != nil {
			return nil, appendErr
		}
	}
	return out, nil
}

// --- async.race ---

// builtinAsyncRace executes all listed placeholders concurrently and returns
// the result of the first one to complete successfully. The remaining
// placeholders are cancelled.
//
// Starlark signature:
//
//	async.race(refs) -> first winner result string
//
// Parameters:
//   - refs (list of str): placeholder IDs returned by async.launch
//
// Returns ErrNoRaceWinner if all competitors fail or refs is empty.
func (e *Engine) builtinAsyncRace(thread *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	ctx := contextFromThread(thread)
	state := asyncStateFromContext(ctx)
	if state == nil {
		return nil, fmt.Errorf("async.race: async module not available in this context")
	}

	var refsList *starlark.List
	if err := starlark.UnpackArgs("async.race", args, kwargs,
		"refs", &refsList,
	); err != nil {
		return nil, err
	}

	refs, err := starlarkListToStrings("async.race", refsList)
	if err != nil {
		return nil, err
	}

	winner, err := async.Race(ctx, state.graph, refs, state.dispatch)
	if err != nil {
		return nil, fmt.Errorf("async.race: %w", err)
	}

	return starlark.String(winner), nil
}

// --- helpers ---

func starlarkListToStrings(caller string, list *starlark.List) ([]string, error) {
	if list == nil {
		return nil, nil
	}
	out := make([]string, list.Len())
	for i := 0; i < list.Len(); i++ {
		s, ok := starlark.AsString(list.Index(i))
		if !ok {
			return nil, fmt.Errorf("%s: refs must contain strings, got %T at index %d", caller, list.Index(i), i)
		}
		out[i] = s
	}
	return out, nil
}
