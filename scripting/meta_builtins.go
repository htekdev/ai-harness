package scripting

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"sync"
	"sync/atomic"

	"go.starlark.net/starlark"
	"go.starlark.net/starlarkstruct"

	"github.com/htekdev/ai-harness/config"
	"github.com/htekdev/ai-harness/hooks"
	"github.com/htekdev/ai-harness/tools"
)

// metaCallDepthKey is a context key for passing call depth through Go context.
type metaCallDepthKey struct{}

// callDepthFromContext returns the current meta.call_tool recursion depth from context.
func callDepthFromContext(ctx context.Context) int {
	if d, ok := ctx.Value(metaCallDepthKey{}).(int); ok {
		return d
	}
	return 0
}

// withCallDepth returns a context with the updated call depth.
func withCallDepth(ctx context.Context, depth int) context.Context {
	return context.WithValue(ctx, metaCallDepthKey{}, depth)
}

// MetaConfig holds limits for meta built-in operations.
type MetaConfig struct {
	Enabled      bool `yaml:"enabled" json:"enabled"`
	MaxTools     int  `yaml:"max_tools" json:"max_tools"`
	MaxHooks     int  `yaml:"max_hooks" json:"max_hooks"`
	MaxAgents    int  `yaml:"max_agents" json:"max_agents"`
	MaxCallDepth int  `yaml:"max_call_depth" json:"max_call_depth"`
}

// DefaultMetaConfig returns sensible defaults for meta limits.
func DefaultMetaConfig() MetaConfig {
	return MetaConfig{
		Enabled:      true,
		MaxTools:     50,
		MaxHooks:     30,
		MaxAgents:    10,
		MaxCallDepth: 5,
	}
}

// MetaContext provides the meta module with access to the live runtime systems.
type MetaContext struct {
	Registry   *tools.Registry
	HookSystem *hooks.System
	Agents     map[string]*config.AgentConfig
	Engine     *Engine
	Config     MetaConfig

	mu             sync.Mutex
	registeredTools int32
	registeredHooks int32
	registeredAgents int32
}

// toolNamePattern validates tool/hook/agent names.
var toolNamePattern = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,63}$`)

// SetMetaContext attaches meta runtime references to the engine.
// This must be called after construction when the registry and hook system are available.
func (e *Engine) SetMetaContext(mc *MetaContext) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.meta = mc
	// Rebuild builtins to include the meta module.
	e.builtins = e.makeBuiltins()
}

// MetaContext returns the engine's meta context (may be nil if not configured).
func (e *Engine) MetaContext() *MetaContext {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.meta
}

// makeMetaModule builds the meta.* Starlark module.
func (e *Engine) makeMetaModule() *starlarkstruct.Struct {
	return starlarkstruct.FromStringDict(starlarkstruct.Default, starlark.StringDict{
		"register_tool":  starlark.NewBuiltin("meta.register_tool", e.builtinMetaRegisterTool),
		"register_hook":  starlark.NewBuiltin("meta.register_hook", e.builtinMetaRegisterHook),
		"register_agent": starlark.NewBuiltin("meta.register_agent", e.builtinMetaRegisterAgent),
		"call_tool":      starlark.NewBuiltin("meta.call_tool", e.builtinMetaCallTool),
		"list_tools":     starlark.NewBuiltin("meta.list_tools", e.builtinMetaListTools),
	})
}

// --- meta.register_tool ---

func (e *Engine) builtinMetaRegisterTool(thread *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	mc := e.meta
	if mc == nil {
		return nil, fmt.Errorf("meta.register_tool: meta module not enabled")
	}

	var name, description, script string
	var parameters *starlark.Dict
	replace := false

	if err := starlark.UnpackArgs("meta.register_tool", args, kwargs,
		"name", &name,
		"description", &description,
		"parameters?", &parameters,
		"script", &script,
		"replace?", &replace,
	); err != nil {
		return nil, err
	}

	if !toolNamePattern.MatchString(name) {
		return nil, fmt.Errorf("meta.register_tool: invalid name %q (must match ^[a-z][a-z0-9_-]{0,63}$)", name)
	}

	// Check registration limits.
	if !replace || !mc.Registry.Has(name) {
		count := atomic.LoadInt32(&mc.registeredTools)
		if int(count) >= mc.Config.MaxTools {
			return nil, fmt.Errorf("meta.register_tool: registration limit reached (%d)", mc.Config.MaxTools)
		}
	}

	// Emit meta.register_tool hook event (can be blocked by governance).
	if mc.HookSystem != nil {
		ctx := contextFromThread(thread)
		payload := map[string]any{
			"name":        name,
			"description": description,
			"replace":     replace,
		}
		result := mc.HookSystem.Dispatch(ctx, "meta.register_tool", payload)
		if result.Action == hooks.ActionBlock {
			return nil, fmt.Errorf("meta.register_tool: blocked by governance: %s", result.Reason)
		}
	}

	// Prevent tool-creates-tool recursion: check if we're inside a meta.call_tool.
	ctx := contextFromThread(thread)
	if callDepthFromContext(ctx) > 0 {
		return nil, fmt.Errorf("meta.register_tool: cannot register tools from within meta.call_tool")
	}

	// Compile the script.
	handler, err := NewToolHandler(mc.Engine, name, script)
	if err != nil {
		return nil, fmt.Errorf("meta.register_tool: script compilation failed: %w", err)
	}

	// Build the tool definition.
	def := tools.Definition{
		Name:        name,
		Description: description,
		Parameters:  buildParametersFromDict(parameters),
	}

	// Register or replace.
	if replace {
		if err := mc.Registry.Replace(def, handler); err != nil {
			return nil, fmt.Errorf("meta.register_tool: %w", err)
		}
	} else {
		if err := mc.Registry.Register(def, handler); err != nil {
			return nil, fmt.Errorf("meta.register_tool: %w", err)
		}
		atomic.AddInt32(&mc.registeredTools, 1)
	}

	return starlark.True, nil
}

// --- meta.register_hook ---

func (e *Engine) builtinMetaRegisterHook(thread *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	mc := e.meta
	if mc == nil {
		return nil, fmt.Errorf("meta.register_hook: meta module not enabled")
	}

	var name, event, script, when string
	priority := 100

	if err := starlark.UnpackArgs("meta.register_hook", args, kwargs,
		"name", &name,
		"event", &event,
		"script", &script,
		"when?", &when,
		"priority?", &priority,
	); err != nil {
		return nil, err
	}

	if !toolNamePattern.MatchString(name) {
		return nil, fmt.Errorf("meta.register_hook: invalid name %q", name)
	}

	if !hooks.IsValidEvent(event) {
		return nil, fmt.Errorf("meta.register_hook: invalid event %q", event)
	}

	// Prevent hook-registers-hook infinite loops.
	if event == "meta.register_hook" {
		return nil, fmt.Errorf("meta.register_hook: cannot register hooks for meta.register_hook event")
	}

	// Priority floor enforcement: meta-registered hooks cannot be below 50.
	if priority < 50 {
		priority = 50
	}

	// Check registration limits.
	count := atomic.LoadInt32(&mc.registeredHooks)
	if int(count) >= mc.Config.MaxHooks {
		return nil, fmt.Errorf("meta.register_hook: registration limit reached (%d)", mc.Config.MaxHooks)
	}

	// Emit meta.register_hook hook event.
	if mc.HookSystem != nil {
		ctx := contextFromThread(thread)
		payload := map[string]any{
			"name":     name,
			"event":    event,
			"priority": priority,
		}
		result := mc.HookSystem.Dispatch(ctx, "meta.register_hook", payload)
		if result.Action == hooks.ActionBlock {
			return nil, fmt.Errorf("meta.register_hook: blocked by governance: %s", result.Reason)
		}
	}

	// Compile the hook script.
	handler, err := NewConditionalHookHandler(mc.Engine, name, when, script)
	if err != nil {
		return nil, fmt.Errorf("meta.register_hook: script compilation failed: %w", err)
	}

	mc.HookSystem.Register(hooks.Registration{
		Name:     name,
		Event:    hooks.Event(event),
		Priority: priority,
		Handler:  handler,
	})

	atomic.AddInt32(&mc.registeredHooks, 1)
	return starlark.True, nil
}

// --- meta.register_agent ---

func (e *Engine) builtinMetaRegisterAgent(thread *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	mc := e.meta
	if mc == nil {
		return nil, fmt.Errorf("meta.register_agent: meta module not enabled")
	}

	var name, model, systemPrompt string
	var toolsList *starlark.List
	var hooksList *starlark.List

	if err := starlark.UnpackArgs("meta.register_agent", args, kwargs,
		"name", &name,
		"model?", &model,
		"system_prompt?", &systemPrompt,
		"tools?", &toolsList,
		"hooks?", &hooksList,
	); err != nil {
		return nil, err
	}

	if !toolNamePattern.MatchString(name) {
		return nil, fmt.Errorf("meta.register_agent: invalid name %q", name)
	}

	// Check registration limits.
	count := atomic.LoadInt32(&mc.registeredAgents)
	if int(count) >= mc.Config.MaxAgents {
		return nil, fmt.Errorf("meta.register_agent: registration limit reached (%d)", mc.Config.MaxAgents)
	}

	// Emit meta.register_agent hook event.
	if mc.HookSystem != nil {
		ctx := contextFromThread(thread)
		toolCount := 0
		if toolsList != nil {
			toolCount = toolsList.Len()
		}
		payload := map[string]any{
			"name":       name,
			"model":      model,
			"tool_count": toolCount,
		}
		result := mc.HookSystem.Dispatch(ctx, "meta.register_agent", payload)
		if result.Action == hooks.ActionBlock {
			return nil, fmt.Errorf("meta.register_agent: blocked by governance: %s", result.Reason)
		}
	}

	// Build agent tools list.
	var agentTools []config.AgentTool
	if toolsList != nil {
		for i := 0; i < toolsList.Len(); i++ {
			s, ok := starlark.AsString(toolsList.Index(i))
			if !ok {
				return nil, fmt.Errorf("meta.register_agent: tools list must contain strings")
			}
			agentTools = append(agentTools, config.AgentTool{Ref: s})
		}
	}

	// Build agent hooks list.
	var agentHooks []config.AgentHook
	if hooksList != nil {
		for i := 0; i < hooksList.Len(); i++ {
			s, ok := starlark.AsString(hooksList.Index(i))
			if !ok {
				return nil, fmt.Errorf("meta.register_agent: hooks list must contain strings")
			}
			agentHooks = append(agentHooks, config.AgentHook{Ref: s})
		}
	}

	mc.mu.Lock()
	mc.Agents[name] = &config.AgentConfig{
		Name:         name,
		Model:        model,
		SystemPrompt: systemPrompt,
		Tools:        agentTools,
		Hooks:        agentHooks,
	}
	mc.mu.Unlock()

	atomic.AddInt32(&mc.registeredAgents, 1)
	return starlark.True, nil
}

// --- meta.call_tool ---

func (e *Engine) builtinMetaCallTool(thread *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	mc := e.meta
	if mc == nil {
		return nil, fmt.Errorf("meta.call_tool: meta module not enabled")
	}

	var name string
	var arguments *starlark.Dict

	if err := starlark.UnpackArgs("meta.call_tool", args, kwargs,
		"name", &name,
		"arguments?", &arguments,
	); err != nil {
		return nil, err
	}

	// Get context from thread and check recursion depth via context.
	ctx := contextFromThread(thread)
	depth := callDepthFromContext(ctx)
	if depth >= mc.Config.MaxCallDepth {
		return nil, fmt.Errorf("meta.call_tool: maximum call depth exceeded (%d)", mc.Config.MaxCallDepth)
	}

	// Build arguments JSON.
	var argsJSON json.RawMessage
	if arguments != nil {
		goArgs := starlarkToGo(arguments)
		data, err := json.Marshal(goArgs)
		if err != nil {
			return nil, fmt.Errorf("meta.call_tool: failed to marshal arguments: %w", err)
		}
		argsJSON = data
	} else {
		argsJSON = []byte("{}")
	}

	// Create a new context with incremented depth.
	ctxWithDepth := withCallDepth(ctx, depth+1)

	// Fire tool.pre hook.
	if mc.HookSystem != nil {
		prePayload := map[string]any{
			"name":      name,
			"arguments": string(argsJSON),
		}
		// Also emit meta.call_tool custom event.
		metaResult := mc.HookSystem.Dispatch(ctxWithDepth, "meta.call_tool", prePayload)
		if metaResult.Action == hooks.ActionBlock {
			return starlark.String(fmt.Sprintf("error: blocked by governance: %s", metaResult.Reason)), nil
		}

		preResult := mc.HookSystem.Dispatch(ctxWithDepth, hooks.EventToolPre, map[string]any{
			"id":        fmt.Sprintf("meta-call-%s-%d", name, depth),
			"name":      name,
			"arguments": string(argsJSON),
		})
		if preResult.Action == hooks.ActionBlock {
			return starlark.String(fmt.Sprintf("error: tool.pre blocked: %s", preResult.Reason)), nil
		}
	}

	// Execute the tool with the depth-incremented context.
	call := tools.Call{
		ID:        fmt.Sprintf("meta-call-%s-%d", name, depth),
		Name:      name,
		Arguments: argsJSON,
	}
	result := mc.Registry.Execute(ctxWithDepth, call)

	// Fire tool.post hook.
	if mc.HookSystem != nil {
		mc.HookSystem.Dispatch(ctxWithDepth, hooks.EventToolPost, map[string]any{
			"id":       call.ID,
			"name":     name,
			"result":   result.Content,
			"is_error": result.IsError,
		})
	}

	if result.IsError {
		return starlark.String("error: " + result.Content), nil
	}
	return starlark.String(result.Content), nil
}

// --- meta.list_tools ---

func (e *Engine) builtinMetaListTools(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	mc := e.meta
	if mc == nil {
		return nil, fmt.Errorf("meta.list_tools: meta module not enabled")
	}

	var pattern string
	if err := starlark.UnpackArgs("meta.list_tools", args, kwargs,
		"pattern?", &pattern,
	); err != nil {
		return nil, err
	}

	defs := mc.Registry.List()

	var matchPattern *regexp.Regexp
	if pattern != "" {
		// Convert glob-like pattern to regex.
		regexStr := "^" + regexp.QuoteMeta(pattern) + "$"
		regexStr = replaceGlobStar(regexStr)
		var err error
		matchPattern, err = regexp.Compile(regexStr)
		if err != nil {
			return nil, fmt.Errorf("meta.list_tools: invalid pattern %q: %w", pattern, err)
		}
	}

	result := starlark.NewList(nil)
	for _, def := range defs {
		if matchPattern != nil && !matchPattern.MatchString(def.Name) {
			continue
		}
		entry := starlark.NewDict(3)
		_ = entry.SetKey(starlark.String("name"), starlark.String(def.Name))
		_ = entry.SetKey(starlark.String("description"), starlark.String(def.Description))
		_ = entry.SetKey(starlark.String("parameter_count"), starlark.MakeInt(len(def.Parameters)))
		_ = result.Append(entry)
	}

	return result, nil
}

// --- Helpers ---

// contextFromThread extracts the Go context from a Starlark thread.
func contextFromThread(thread *starlark.Thread) context.Context {
	if ctx, ok := thread.Local(threadContextKey).(context.Context); ok && ctx != nil {
		return ctx
	}
	return context.Background()
}

// buildParametersFromDict converts a Starlark dict of parameter definitions to tools.Parameter slice.
func buildParametersFromDict(d *starlark.Dict) []tools.Parameter {
	if d == nil {
		return nil
	}

	var params []tools.Parameter
	for _, item := range d.Items() {
		name, _ := starlark.AsString(item[0])
		paramDict, ok := item[1].(*starlark.Dict)
		if !ok {
			continue
		}

		p := tools.Parameter{Name: name}

		if v, found, _ := paramDict.Get(starlark.String("type")); found {
			if s, ok := starlark.AsString(v); ok {
				p.Type = tools.ParameterType(s)
			}
		}
		if v, found, _ := paramDict.Get(starlark.String("description")); found {
			if s, ok := starlark.AsString(v); ok {
				p.Description = s
			}
		}
		if v, found, _ := paramDict.Get(starlark.String("required")); found {
			p.Required = bool(v.Truth())
		}

		params = append(params, p)
	}
	return params
}

// replaceGlobStar replaces escaped glob stars (\*) with regex (.*) for pattern matching.
func replaceGlobStar(s string) string {
	// QuoteMeta escapes * to \*, we want to convert it back to .* for glob matching.
	result := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		if i+1 < len(s) && s[i] == '\\' && s[i+1] == '*' {
			result = append(result, '.', '*')
			i++ // skip the *
		} else {
			result = append(result, s[i])
		}
	}
	return string(result)
}
