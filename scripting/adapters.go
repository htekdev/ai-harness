package scripting

import (
	"context"
	"encoding/json"

	"github.com/htekdev/ai-harness/hooks"
	"github.com/htekdev/ai-harness/tools"
)

// NewToolHandler creates a tools.Handler backed by a Starlark script.
func NewToolHandler(engine *Engine, name, script string) (tools.Handler, error) {
	runner, err := engine.CompileToolScript(name, script)
	if err != nil {
		return nil, err
	}

	return func(ctx context.Context, args json.RawMessage) (string, error) {
		return runner.Run(ctx, args)
	}, nil
}

// NewHookHandler creates a hooks.Handler backed by a Starlark script.
func NewHookHandler(engine *Engine, name, script string) (hooks.Handler, error) {
	return NewConditionalHookHandler(engine, name, "", script)
}

// NewConditionalHookHandler creates a hooks.Handler backed by a Starlark script
// with an optional Starlark when expression.
func NewConditionalHookHandler(engine *Engine, name, when, script string) (hooks.Handler, error) {
	runner, err := engine.CompileConditionalHookScript(name, when, script)
	if err != nil {
		return nil, err
	}

	return func(ctx context.Context, event hooks.Event, payload any) hooks.Result {
		return runner.Run(ctx, event, payload)
	}, nil
}
