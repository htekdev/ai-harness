package scripting

import (
	"context"
	"encoding/json"
	"fmt"

	"go.starlark.net/starlark"
	"go.starlark.net/starlarkstruct"

	"github.com/htekdev/ai-harness/async"
)

// placeholderValue wraps an *async.Placeholder as a Starlark value so scripts
// can pass placeholder refs between async.launch and wait_all / wait_any / race.
type placeholderValue struct {
	p *async.Placeholder
}

var _ starlark.Value = (*placeholderValue)(nil)

func (v *placeholderValue) String() string {
	return fmt.Sprintf("<async.placeholder id=%s tool=%s state=%s>",
		v.p.ID(), v.p.ToolName(), v.p.State())
}
func (v *placeholderValue) Type() string          { return "async.placeholder" }
func (v *placeholderValue) Freeze()               {}
func (v *placeholderValue) Truth() starlark.Bool  { return starlark.True }
func (v *placeholderValue) Hash() (uint32, error) { return 0, fmt.Errorf("unhashable: async.placeholder") }

// resultToStarlark converts an async.Result to a Starlark dict with
// keys: id (string), tool (string), result (string), is_error (bool).
func resultToStarlark(r async.Result) starlark.Value {
	isErr := starlark.Bool(r.Err != nil)
	resultStr := starlark.String(r.Value)
	if r.Err != nil {
		resultStr = starlark.String(r.Err.Error())
	}
	d := starlark.NewDict(4)
	_ = d.SetKey(starlark.String("id"), starlark.String(r.ID))
	_ = d.SetKey(starlark.String("tool"), starlark.String(r.ToolName))
	_ = d.SetKey(starlark.String("result"), resultStr)
	_ = d.SetKey(starlark.String("is_error"), isErr)
	return d
}

// asyncModule returns the parallel.* Starlark module.
// Note: the Starlark variable name is "parallel" (not "async") because
// "async" is a reserved keyword in the Starlark language specification.
func asyncModule() starlark.Value {
	return starlarkstruct.FromStringDict(starlarkstruct.Default, starlark.StringDict{
		"launch":   starlark.NewBuiltin("parallel.launch", builtinAsyncLaunch),
		"wait_all": starlark.NewBuiltin("parallel.wait_all", builtinAsyncWaitAll),
		"wait_any": starlark.NewBuiltin("parallel.wait_any", builtinAsyncWaitAny),
		"race":     starlark.NewBuiltin("parallel.race", builtinAsyncRace),
	})
}

// requireExecutor retrieves the async Executor from the thread context.
func requireExecutor(thread *starlark.Thread, caller string) (*async.Executor, context.Context, error) {
	ctx, _ := thread.Local(threadContextKey).(context.Context)
	if ctx == nil {
		return nil, nil, fmt.Errorf("%s: no context in thread", caller)
	}
	exec := async.ExecutorFromContext(ctx)
	if exec == nil {
		return nil, nil, fmt.Errorf("%s: parallel executor not available (not in an async-enabled turn)", caller)
	}
	return exec, ctx, nil
}

// builtinAsyncLaunch implements parallel.launch(tool, args, depends_on=[]).
//
//	tool      - string, the registered tool name to invoke
//	args      - dict, the arguments to pass (will be JSON-encoded)
//	depends_on - optional list of placeholder refs that must complete first
func builtinAsyncLaunch(thread *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var toolName string
	var argsVal starlark.Value = starlark.NewDict(0)
	var depsVal starlark.Value = starlark.None

	if err := starlark.UnpackArgs("parallel.launch", args, kwargs,
		"tool", &toolName,
		"args?", &argsVal,
		"depends_on?", &depsVal,
	); err != nil {
		return nil, err
	}

	executor, ctx, err := requireExecutor(thread, "parallel.launch")
	if err != nil {
		return nil, err
	}

	// Convert args dict → JSON.
	argsJSON, err := starlarkToJSON(argsVal)
	if err != nil {
		return nil, fmt.Errorf("parallel.launch: convert args: %w", err)
	}

	// Convert depends_on list → []*Placeholder.
	var deps []*async.Placeholder
	if depsVal != starlark.None {
		depList, ok := depsVal.(*starlark.List)
		if !ok {
			return nil, fmt.Errorf("parallel.launch: depends_on must be a list, got %s", depsVal.Type())
		}
		for i := 0; i < depList.Len(); i++ {
			item := depList.Index(i)
			pv, ok := item.(*placeholderValue)
			if !ok {
				return nil, fmt.Errorf("parallel.launch: depends_on[%d] must be an async.placeholder, got %s", i, item.Type())
			}
			deps = append(deps, pv.p)
		}
	}

	p, err := executor.Launch(ctx, toolName, json.RawMessage(argsJSON), deps)
	if err != nil {
		return nil, fmt.Errorf("parallel.launch: %w", err)
	}

	return &placeholderValue{p: p}, nil
}

// builtinAsyncWaitAll implements parallel.wait_all(refs) → list of result dicts.
func builtinAsyncWaitAll(thread *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var refsVal starlark.Value
	if err := starlark.UnpackArgs("parallel.wait_all", args, kwargs, "refs", &refsVal); err != nil {
		return nil, err
	}

	_, ctx, err := requireExecutor(thread, "parallel.wait_all")
	if err != nil {
		return nil, err
	}

	refs, err := unpackPlaceholderList(refsVal, "parallel.wait_all")
	if err != nil {
		return nil, err
	}

	results := async.WaitAll(ctx, refs)

	out := make([]starlark.Value, len(results))
	for i, r := range results {
		out[i] = resultToStarlark(r)
	}
	return starlark.NewList(out), nil
}

// builtinAsyncWaitAny implements parallel.wait_any(refs) → first result dict.
func builtinAsyncWaitAny(thread *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var refsVal starlark.Value
	if err := starlark.UnpackArgs("parallel.wait_any", args, kwargs, "refs", &refsVal); err != nil {
		return nil, err
	}

	_, ctx, err := requireExecutor(thread, "parallel.wait_any")
	if err != nil {
		return nil, err
	}

	refs, err := unpackPlaceholderList(refsVal, "parallel.wait_any")
	if err != nil {
		return nil, err
	}

	result := async.WaitAny(ctx, refs)
	return resultToStarlark(result), nil
}

// builtinAsyncRace implements parallel.race(refs) → first result dict, cancels losers.
func builtinAsyncRace(thread *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var refsVal starlark.Value
	if err := starlark.UnpackArgs("parallel.race", args, kwargs, "refs", &refsVal); err != nil {
		return nil, err
	}

	_, ctx, err := requireExecutor(thread, "parallel.race")
	if err != nil {
		return nil, err
	}

	refs, err := unpackPlaceholderList(refsVal, "parallel.race")
	if err != nil {
		return nil, err
	}

	result := async.Race(ctx, refs)
	return resultToStarlark(result), nil
}

// unpackPlaceholderList converts a Starlark list of placeholderValues
// into a []*async.Placeholder.
func unpackPlaceholderList(v starlark.Value, caller string) ([]*async.Placeholder, error) {
	list, ok := v.(*starlark.List)
	if !ok {
		return nil, fmt.Errorf("%s: refs must be a list, got %s", caller, v.Type())
	}
	refs := make([]*async.Placeholder, list.Len())
	for i := 0; i < list.Len(); i++ {
		item := list.Index(i)
		pv, ok := item.(*placeholderValue)
		if !ok {
			return nil, fmt.Errorf("%s: refs[%d] must be an async.placeholder, got %s", caller, i, item.Type())
		}
		refs[i] = pv.p
	}
	return refs, nil
}

// starlarkToJSON converts a Starlark value to a JSON byte slice.
func starlarkToJSON(v starlark.Value) ([]byte, error) {
	goVal := starlarkToGo(v)
	return json.Marshal(goVal)
}
