package scripting

import (
	"context"
	"fmt"
	"sort"
	"sync"

	"go.starlark.net/starlark"
	"go.starlark.net/starlarkstruct"
)

type turnStateContextKey struct{}

type turnStateStore struct {
	mu     sync.RWMutex
	values map[string]any
}

// WithTurnState attaches a per-turn scratchpad to the context so hooks and tools
// can exchange structured data during a single agent turn.
func WithTurnState(ctx context.Context) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if turnStateFromContext(ctx) != nil {
		return ctx
	}
	return context.WithValue(ctx, turnStateContextKey{}, &turnStateStore{values: make(map[string]any)})
}

func turnStateFromContext(ctx context.Context) *turnStateStore {
	if ctx == nil {
		return nil
	}
	store, _ := ctx.Value(turnStateContextKey{}).(*turnStateStore)
	return store
}

func ctxModule() starlark.Value {
	return starlarkstruct.FromStringDict(starlarkstruct.Default, starlark.StringDict{
		"set":      starlark.NewBuiltin("ctx.set", builtinCtxSet),
		"get":      starlark.NewBuiltin("ctx.get", builtinCtxGet),
		"has":      starlark.NewBuiltin("ctx.has", builtinCtxHas),
		"delete":   starlark.NewBuiltin("ctx.delete", builtinCtxDelete),
		"clear":    starlark.NewBuiltin("ctx.clear", builtinCtxClear),
		"snapshot": starlark.NewBuiltin("ctx.snapshot", builtinCtxSnapshot),
	})
}

func builtinCtxSet(thread *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var key string
	var value starlark.Value
	if err := starlark.UnpackArgs("ctx.set", args, kwargs, "key", &key, "value", &value); err != nil {
		return nil, err
	}
	store, err := requireTurnState(thread, "ctx.set")
	if err != nil {
		return nil, err
	}
	store.mu.Lock()
	store.values[key] = starlarkToGo(value)
	store.mu.Unlock()
	return value, nil
}

func builtinCtxGet(thread *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var key string
	defaultVal := starlark.Value(starlark.None)
	if err := starlark.UnpackArgs("ctx.get", args, kwargs, "key", &key, "default?", &defaultVal); err != nil {
		return nil, err
	}
	store, err := requireTurnState(thread, "ctx.get")
	if err != nil {
		return nil, err
	}
	store.mu.RLock()
	value, ok := store.values[key]
	store.mu.RUnlock()
	if !ok {
		return defaultVal, nil
	}
	return goToStarlark(value)
}

func builtinCtxHas(thread *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var key string
	if err := starlark.UnpackArgs("ctx.has", args, kwargs, "key", &key); err != nil {
		return nil, err
	}
	store, err := requireTurnState(thread, "ctx.has")
	if err != nil {
		return nil, err
	}
	store.mu.RLock()
	_, ok := store.values[key]
	store.mu.RUnlock()
	return starlark.Bool(ok), nil
}

func builtinCtxDelete(thread *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var key string
	if err := starlark.UnpackArgs("ctx.delete", args, kwargs, "key", &key); err != nil {
		return nil, err
	}
	store, err := requireTurnState(thread, "ctx.delete")
	if err != nil {
		return nil, err
	}
	store.mu.Lock()
	_, ok := store.values[key]
	delete(store.values, key)
	store.mu.Unlock()
	return starlark.Bool(ok), nil
}

func builtinCtxClear(thread *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	if err := starlark.UnpackArgs("ctx.clear", args, kwargs); err != nil {
		return nil, err
	}
	store, err := requireTurnState(thread, "ctx.clear")
	if err != nil {
		return nil, err
	}
	store.mu.Lock()
	store.values = make(map[string]any)
	store.mu.Unlock()
	return starlark.None, nil
}

func builtinCtxSnapshot(thread *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	if err := starlark.UnpackArgs("ctx.snapshot", args, kwargs); err != nil {
		return nil, err
	}
	store, err := requireTurnState(thread, "ctx.snapshot")
	if err != nil {
		return nil, err
	}
	store.mu.RLock()
	keys := make([]string, 0, len(store.values))
	for key := range store.values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	snapshot := make(map[string]any, len(keys))
	for _, key := range keys {
		snapshot[key] = store.values[key]
	}
	store.mu.RUnlock()
	return goToStarlark(snapshot)
}

func requireTurnState(thread *starlark.Thread, name string) (*turnStateStore, error) {
	ctx, _ := thread.Local(threadContextKey).(context.Context)
	store := turnStateFromContext(ctx)
	if store == nil {
		return nil, fmt.Errorf("%s: turn state is not available", name)
	}
	return store, nil
}
