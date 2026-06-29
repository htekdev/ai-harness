package scripting

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"

	"go.starlark.net/starlark"
	"go.starlark.net/starlarkstruct"
)

type turnStateContextKey struct{}

type turnStateStore struct {
	mu     sync.RWMutex
	values map[string]any
}

const (
	// TurnStateAgentDoneFlagKey tracks whether the agent explicitly signaled done.
	TurnStateAgentDoneFlagKey = "agent.done_flag"
	// TurnStateAgentDoneSummaryKey stores the done summary passed by done tool.
	TurnStateAgentDoneSummaryKey = "agent.done_summary"
	// TurnStateAgentDoneClaimsKey stores structured claims passed by done tool.
	TurnStateAgentDoneClaimsKey = "agent.done_claims"
)

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
		"agent":    &agentRuntimeValue{},
	})
}

type agentRuntimeValue struct{}

func (a *agentRuntimeValue) String() string       { return "ctx.agent" }
func (a *agentRuntimeValue) Type() string         { return "ctx.agent" }
func (a *agentRuntimeValue) Freeze()              {}
func (a *agentRuntimeValue) Truth() starlark.Bool { return starlark.True }
func (a *agentRuntimeValue) Hash() (uint32, error) {
	return 0, fmt.Errorf("unhashable type: %s", a.Type())
}
func (a *agentRuntimeValue) AttrNames() []string {
	return []string{"done_flag", "set_done_flag", "run_verification_chain"}
}
func (a *agentRuntimeValue) Attr(name string) (starlark.Value, error) {
	switch name {
	case "done_flag":
		return starlark.NewBuiltin("ctx.agent.done_flag", builtinAgentDoneFlag), nil
	case "set_done_flag":
		return starlark.NewBuiltin("ctx.agent.set_done_flag", builtinAgentSetDoneFlag), nil
	case "run_verification_chain":
		return starlark.NewBuiltin("ctx.agent.run_verification_chain", builtinAgentRunVerificationChain), nil
	default:
		return nil, nil
	}
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

func builtinAgentDoneFlag(thread *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	if err := starlark.UnpackArgs("ctx.agent.done_flag", args, kwargs); err != nil {
		return nil, err
	}
	store, err := requireTurnState(thread, "ctx.agent.done_flag")
	if err != nil {
		return nil, err
	}
	store.mu.RLock()
	defer store.mu.RUnlock()
	v, _ := store.values[TurnStateAgentDoneFlagKey]
	flag, _ := v.(bool)
	return starlark.Bool(flag), nil
}

func builtinAgentSetDoneFlag(thread *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var summary string
	claimsVal := starlark.Value(starlark.None)
	if err := starlark.UnpackArgs("ctx.agent.set_done_flag", args, kwargs, "summary?", &summary, "claims?", &claimsVal); err != nil {
		return nil, err
	}
	store, err := requireTurnState(thread, "ctx.agent.set_done_flag")
	if err != nil {
		return nil, err
	}
	store.mu.Lock()
	store.values[TurnStateAgentDoneFlagKey] = true
	store.values[TurnStateAgentDoneSummaryKey] = summary
	if claimsVal != starlark.None {
		store.values[TurnStateAgentDoneClaimsKey] = starlarkToGo(claimsVal)
	}
	store.mu.Unlock()
	return goToStarlark(map[string]any{"acknowledged": true})
}

func builtinAgentRunVerificationChain(thread *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	if err := starlark.UnpackArgs("ctx.agent.run_verification_chain", args, kwargs); err != nil {
		return nil, err
	}
	store, err := requireTurnState(thread, "ctx.agent.run_verification_chain")
	if err != nil {
		return nil, err
	}
	store.mu.RLock()
	defer store.mu.RUnlock()

	// Optional explicit override for tests/hooks.
	if raw, ok := store.values["agent.verification_result"]; ok {
		if m, ok := raw.(map[string]any); ok {
			return goToStarlark(m)
		}
	}

	done, _ := store.values[TurnStateAgentDoneFlagKey].(bool)
	if !done {
		return goToStarlark(map[string]any{
			"ok":     false,
			"reason": "done flag not set",
		})
	}

	summary, _ := store.values[TurnStateAgentDoneSummaryKey].(string)
	return goToStarlark(map[string]any{
		"ok":     true,
		"reason": strings.TrimSpace(summary),
	})
}

func requireTurnState(thread *starlark.Thread, name string) (*turnStateStore, error) {
	ctx, _ := thread.Local(threadContextKey).(context.Context)
	store := turnStateFromContext(ctx)
	if store == nil {
		return nil, fmt.Errorf("%s: turn state is not available", name)
	}
	return store, nil
}

// SetTurnState sets a value in the per-turn scratchpad.
// Returns false if no turn state is available on the context.
func SetTurnState(ctx context.Context, key string, value any) bool {
	store := turnStateFromContext(ctx)
	if store == nil {
		return false
	}
	store.mu.Lock()
	store.values[key] = value
	store.mu.Unlock()
	return true
}

// TurnStateValues returns a snapshot copy of all values in the per-turn scratchpad.
// Returns (nil, false) if no turn state is available on the context.
func TurnStateValues(ctx context.Context) (map[string]any, bool) {
	store := turnStateFromContext(ctx)
	if store == nil {
		return nil, false
	}
	store.mu.RLock()
	defer store.mu.RUnlock()
	out := make(map[string]any, len(store.values))
	for k, v := range store.values {
		out[k] = v
	}
	return out, true
}
