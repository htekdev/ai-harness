// Package delegation provides the ability for an agent to spin up sub-agents
// with custom tools and hooks defined at runtime via Starlark scripts.
package delegation

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"reflect"
	"sync"

	"github.com/htekdev/ai-harness/agent"
	"github.com/htekdev/ai-harness/completion"
	agentctx "github.com/htekdev/ai-harness/context"
	"github.com/htekdev/ai-harness/hooks"
	"github.com/htekdev/ai-harness/scripting"
	"github.com/htekdev/ai-harness/tools"
)

// MaxDelegationDepth is the maximum recursion depth for delegation.
const MaxDelegationDepth = 1

// MaxDelegateToolIterations is the max tool-call loops for delegate agents.
// Kept low to fail fast when tools error repeatedly.
const MaxDelegateToolIterations = 5

// MaxToolRetries is how many times a tool can error before the retry guard blocks it.
const MaxToolRetries = 2

// ToolSpec defines a tool to be created for a delegate agent.
type ToolSpec struct {
	Name        string               `json:"name"`
	Description string               `json:"description"`
	Parameters  map[string]ParamSpec `json:"parameters"`
	Script      string               `json:"script"`
}

// ParamSpec defines a tool parameter.
type ParamSpec struct {
	Type        string `json:"type"`
	Description string `json:"description,omitempty"`
	Required    bool   `json:"required,omitempty"`
}

// HookSpec defines a hook to be created for a delegate agent.
type HookSpec struct {
	Event    string `json:"event"`
	Handler  string `json:"handler"`
	Script   string `json:"script"`
	When     string `json:"when,omitempty"`
	Priority int    `json:"priority,omitempty"`
}

// Request contains everything needed to spin up a delegate agent.
type Request struct {
	Task         string     `json:"task"`
	Tools        []ToolSpec `json:"tools"`
	Hooks        []HookSpec `json:"hooks,omitempty"`
	SystemPrompt string     `json:"system_prompt,omitempty"`
}

// Result is the output of a delegation.
type Result struct {
	Response    string         `json:"response"`
	ToolCalls   []tools.Call   `json:"tool_calls,omitempty"`
	ToolResults []tools.Result `json:"tool_results,omitempty"`
}

// Delegator manages the creation and execution of delegate agents.
type Delegator struct {
	client       *completion.Client
	engine       *scripting.Engine
	hookSystem   *hooks.System
	systemPrompt string
	logger       *log.Logger
}

// DelegatorConfig configures the delegator.
type DelegatorConfig struct {
	Client       *completion.Client
	Engine       *scripting.Engine
	HookSystem   *hooks.System
	SystemPrompt string
	Logger       *log.Logger
}

// NewDelegator creates a new Delegator.
func NewDelegator(cfg DelegatorConfig) *Delegator {
	if cfg.Logger == nil {
		cfg.Logger = log.New(os.Stderr, "[delegate] ", log.LstdFlags)
	}
	if cfg.Engine == nil {
		cfg.Engine = scripting.NewEngine()
	}
	return &Delegator{
		client:       cfg.Client,
		engine:       cfg.Engine,
		hookSystem:   cfg.HookSystem,
		systemPrompt: cfg.SystemPrompt,
		logger:       cfg.Logger,
	}
}

// Execute spins up a delegate agent with the specified tools and hooks, then runs the task.
func (d *Delegator) Execute(ctx context.Context, req Request) (*Result, error) {
	if d.hookSystem != nil {
		preResult := d.hookSystem.Dispatch(ctx, hooks.EventDelegatePre, &req)
		if preResult.Action == hooks.ActionBlock {
			return nil, fmt.Errorf("delegation blocked: %s", preResult.Reason)
		}
		if preResult.Action == hooks.ActionModify {
			if err := applyHookPayload(preResult.Payload, &req); err != nil {
				return nil, fmt.Errorf("delegate.pre modify payload: %w", err)
			}
		}
	}

	registry := tools.NewRegistry()
	for _, toolSpec := range req.Tools {
		// Auto-inject task context: if tool has no declared parameters,
		// add a hidden "_task" param so the script always has context.
		params := toolSpec.Parameters
		if len(params) == 0 {
			params = map[string]ParamSpec{
				"_task": {Type: "string", Description: "The task context (auto-injected)"},
			}
		}

		def := tools.Definition{
			Name:        toolSpec.Name,
			Description: toolSpec.Description,
			Parameters:  convertParams(params),
		}

		// Wrap the handler to auto-inject task context into args when parameters are empty
		handler, err := scripting.NewToolHandler(d.engine, toolSpec.Name, toolSpec.Script)
		if err != nil {
			return nil, fmt.Errorf("compile tool %q: %w", toolSpec.Name, err)
		}

		// If original spec had no parameters, wrap handler to inject task as "_task" arg
		if len(toolSpec.Parameters) == 0 {
			originalHandler := handler
			taskJSON := req.Task
			handler = func(ctx context.Context, args json.RawMessage) (string, error) {
				// Merge _task into args if not already present
				var argsMap map[string]any
				if err := json.Unmarshal(args, &argsMap); err != nil || argsMap == nil {
					argsMap = make(map[string]any)
				}
				if _, exists := argsMap["_task"]; !exists {
					argsMap["_task"] = taskJSON
				}
				enriched, _ := json.Marshal(argsMap)
				return originalHandler(ctx, enriched)
			}
		}

		if err := registry.Register(def, handler); err != nil {
			return nil, fmt.Errorf("register tool %q: %w", toolSpec.Name, err)
		}
	}

	hookSystem := hooks.NewSystem()

	// Built-in retry guard: blocks a tool after MaxToolRetries consecutive errors.
	retryGuard := newRetryGuardHook()
	hookSystem.Register(hooks.Registration{
		Name:     "_retry_guard",
		Event:    hooks.EventToolPre,
		Priority: 1, // Runs first
		Handler:  retryGuard.preHook,
	})
	hookSystem.Register(hooks.Registration{
		Name:     "_retry_guard_post",
		Event:    hooks.EventToolPost,
		Priority: 1,
		Handler:  retryGuard.postHook,
	})

	// Register user-provided hooks
	for _, hookSpec := range req.Hooks {
		handler, err := scripting.NewConditionalHookHandler(d.engine, hookSpec.Handler, hookSpec.When, hookSpec.Script)
		if err != nil {
			return nil, fmt.Errorf("compile hook %q: %w", hookSpec.Handler, err)
		}

		priority := hookSpec.Priority
		if priority == 0 {
			priority = 100
		}

		hookSystem.Register(hooks.Registration{
			Name:     hookSpec.Handler,
			Event:    hooks.Event(hookSpec.Event),
			Priority: priority,
			Handler:  handler,
		})
	}

	systemPrompt := req.SystemPrompt
	if systemPrompt == "" {
		systemPrompt = d.buildDelegateSystemPrompt(req)
	}

	ctxMgr := agentctx.NewManager(agentctx.Config{
		SystemPrompt: systemPrompt,
		MaxMessages:  50,
		MaxTokens:    128000,
	})

	delegateAgent := agent.New(agent.Options{
		Client:            d.client,
		Tools:             registry,
		Hooks:             hookSystem,
		Context:           ctxMgr,
		Logger:            d.logger,
		MaxToolIterations: MaxDelegateToolIterations,
	})

	turnResult, err := delegateAgent.Run(ctx, req.Task)
	if err != nil {
		return nil, fmt.Errorf("delegate execution: %w", err)
	}

	result := &Result{
		Response:    turnResult.Response,
		ToolCalls:   turnResult.ToolCalls,
		ToolResults: turnResult.ToolResults,
	}

	if d.hookSystem != nil {
		postResult := d.hookSystem.Dispatch(ctx, hooks.EventDelegatePost, result)
		if postResult.Action == hooks.ActionBlock {
			return nil, fmt.Errorf("delegation blocked: %s", postResult.Reason)
		}
		if postResult.Action == hooks.ActionModify {
			if err := applyHookPayload(postResult.Payload, result); err != nil {
				return nil, fmt.Errorf("delegate.post modify payload: %w", err)
			}
		}
	}

	return result, nil
}

// buildDelegateSystemPrompt creates a focused system prompt for the delegate
// that tells it exactly what tools it has and to just use them.
func (d *Delegator) buildDelegateSystemPrompt(req Request) string {
	prompt := "You are a task execution agent. You have been given specific tools to complete a task.\n\n"
	prompt += "RULES:\n"
	prompt += "- Call your tools immediately to complete the task. Do not explain, just execute.\n"
	prompt += "- If a tool errors, do NOT retry with the same arguments. Adjust or report failure.\n"
	prompt += "- Respond with the final result once done.\n\n"
	prompt += "Your tools:\n"
	for _, t := range req.Tools {
		prompt += fmt.Sprintf("- %s: %s\n", t.Name, t.Description)
		if len(t.Parameters) > 0 {
			for name, p := range t.Parameters {
				prompt += fmt.Sprintf("    %s (%s): %s\n", name, p.Type, p.Description)
			}
		} else {
			prompt += "    (no parameters required — call with empty args or any args)\n"
		}
	}
	return prompt
}

func applyHookPayload(payload any, target any) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	rv := reflect.ValueOf(target)
	if rv.Kind() != reflect.Pointer || rv.IsNil() {
		return fmt.Errorf("target must be a non-nil pointer")
	}
	rv.Elem().Set(reflect.Zero(rv.Elem().Type()))
	return json.Unmarshal(data, target)
}

// retryGuardHook tracks tool errors and blocks repeated failures.
type retryGuardHook struct {
	mu           sync.Mutex
	errorCounts  map[string]int // tool name → consecutive error count
	lastCallName string         // tracks which tool is being called
}

func newRetryGuardHook() *retryGuardHook {
	return &retryGuardHook{
		errorCounts: make(map[string]int),
	}
}

func (r *retryGuardHook) preHook(ctx context.Context, event hooks.Event, payload any) hooks.Result {
	call, ok := payload.(*tools.Call)
	if !ok {
		return hooks.Result{Action: hooks.ActionContinue}
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	r.lastCallName = call.Name
	count := r.errorCounts[call.Name]
	if count >= MaxToolRetries {
		return hooks.Result{
			Action: hooks.ActionBlock,
			Reason: fmt.Sprintf("tool %q has failed %d times consecutively — stopping to prevent infinite retry loop", call.Name, count),
		}
	}
	return hooks.Result{Action: hooks.ActionContinue}
}

func (r *retryGuardHook) postHook(ctx context.Context, event hooks.Event, payload any) hooks.Result {
	result, ok := payload.(*tools.Result)
	if !ok {
		return hooks.Result{Action: hooks.ActionContinue}
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if result.IsError {
		r.errorCounts[r.lastCallName]++
	} else {
		// Reset on success
		r.errorCounts[r.lastCallName] = 0
	}
	return hooks.Result{Action: hooks.ActionContinue}
}

// CreateDelegateToolHandler creates a tools.Handler for the built-in "delegate" meta-tool.
func (d *Delegator) CreateDelegateToolHandler() tools.Handler {
	return func(ctx context.Context, args json.RawMessage) (string, error) {
		var req Request
		if err := json.Unmarshal(args, &req); err != nil {
			return "", fmt.Errorf("parse delegate args: %w", err)
		}

		if len(req.Tools) == 0 {
			return "", fmt.Errorf("delegate requires at least one tool definition")
		}
		if req.Task == "" {
			return "", fmt.Errorf("delegate requires a task")
		}

		result, err := d.Execute(ctx, req)
		if err != nil {
			return "", err
		}

		return result.Response, nil
	}
}

// DelegateToolDefinition returns the tool definition for the built-in delegate meta-tool.
func DelegateToolDefinition() tools.Definition {
	return tools.Definition{
		Name: "delegate",
		Description: `Spin up a sub-agent with custom tools and hooks to handle a task.
Available Starlark built-ins for scripts: time.now(), env(key), json.encode(val), json.decode(s), log(msg), random(min, max), sleep(ms), str(val), math.abs(x), math.min(a,b), math.max(a,b), math.floor(x), math.ceil(x), os.cwd(), os.hostname(), os.platform(), os.args(), url.parse(s), url.encode(params), uuid.v4(), http.get(url, headers?, timeout_seconds?), http.post(url, body?, headers?, timeout_seconds?), re.match(pattern, text), re.find_all(pattern, text), re.replace(pattern, repl, text), hash.sha256(text), hash.md5(text), cache.set/get/has/delete/clear, and the fs module.
Hook scripts also get: allow(), block(reason), modify(payload), and optional when expressions for conditional execution. Supported hook events include tool.pre/post, completion.pre/post, turn.start/end, session.start/end, and delegate.pre/post.
Starlark is Python-like but has NO imports. Use only the built-ins listed above.`,
		Parameters: []tools.Parameter{
			{Name: "task", Type: tools.TypeString, Description: "What you want the delegate to accomplish", Required: true},
			{
				Name:        "tools",
				Type:        tools.TypeArray,
				Description: "Array of tool definitions for the delegate",
				Required:    true,
				Items: &tools.ParameterSchema{
					Type: tools.TypeObject,
					Properties: map[string]*tools.ParameterSchema{
						"name":        {Type: tools.TypeString, Description: "Tool name"},
						"description": {Type: tools.TypeString, Description: "What the tool does"},
						"parameters":  {Type: tools.TypeObject, Description: "Tool parameters as JSON Schema properties"},
						"script":      {Type: tools.TypeString, Description: "Starlark script implementing run(args)"},
					},
					Required: []string{"name", "description", "script"},
				},
			},
			{
				Name:        "hooks",
				Type:        tools.TypeArray,
				Description: "Array of hook definitions for governance/guardrails",
				Required:    false,
				Items: &tools.ParameterSchema{
					Type: tools.TypeObject,
					Properties: map[string]*tools.ParameterSchema{
						"event":    {Type: tools.TypeString, Description: "Hook event (tool.pre, tool.post, etc.)"},
						"handler":  {Type: tools.TypeString, Description: "Hook handler name"},
						"script":   {Type: tools.TypeString, Description: "Starlark script implementing handle(event, payload)"},
						"when":     {Type: tools.TypeString, Description: "Optional Starlark expression to decide whether the hook fires"},
						"priority": {Type: tools.TypeNumber, Description: "Execution priority (lower = first)"},
					},
					Required: []string{"event", "handler", "script"},
				},
			},
			{Name: "system_prompt", Type: tools.TypeString, Description: "System prompt for the delegate (inherits parent if omitted)", Required: false},
		},
	}
}

func convertParams(params map[string]ParamSpec) []tools.Parameter {
	result := make([]tools.Parameter, 0, len(params))
	for name, spec := range params {
		result = append(result, tools.Parameter{
			Name:        name,
			Type:        tools.ParameterType(spec.Type),
			Description: spec.Description,
			Required:    spec.Required,
		})
	}
	return result
}
