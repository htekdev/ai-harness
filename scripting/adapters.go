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
	runner, err := engine.CompileHookScript(name, script)
	if err != nil {
		return nil, err
	}

	return func(ctx context.Context, event hooks.Event, payload any) hooks.Result {
		return runner.Run(ctx, event, payload)
	}, nil
}
