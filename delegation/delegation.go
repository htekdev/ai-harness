// Package delegation provides the ability for an agent to spin up sub-agents
// with custom tools and hooks defined at runtime via Starlark scripts.
package delegation

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"reflect"
	"strings"
	"sync"
	"time"

	"github.com/htekdev/ai-harness/agent"
	"github.com/htekdev/ai-harness/completion"
	agentctx "github.com/htekdev/ai-harness/context"
	"github.com/htekdev/ai-harness/hooks"
	"github.com/htekdev/ai-harness/scripting"
	"github.com/htekdev/ai-harness/tools"
)

// MaxDelegationDepth is the default maximum recursion depth for delegation.
// Can be overridden via DelegatorConfig.MaxDepth.
const MaxDelegationDepth = 3

// MaxDelegateToolIterations is the max tool-call loops for delegate agents.
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
	Agent        string     `json:"agent,omitempty"`
	Model        string     `json:"model,omitempty"`
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
	client             *completion.Client
	engine             *scripting.Engine
	hookSystem         *hooks.System
	systemPrompt       string
	logger             *slog.Logger
	maxDepth           int
	iterationsPerDepth []int
	agentResolver      AgentResolver
	clientFactory      ClientFactory
	taskStore          *TaskStore
}

// AgentResolver resolves a named agent to its configuration.
type AgentResolver func(name string) (*ResolvedAgent, error)

// ResolvedAgent holds a fully resolved agent config ready to spawn.
type ResolvedAgent struct {
	SystemPrompt string
	Model        string
	Tools        []ToolSpec
	Hooks        []HookSpec
}

// ClientFactory creates a completion client for a given model name.
type ClientFactory func(modelName string) (*completion.Client, error)

// DelegatorConfig configures the delegator.
type DelegatorConfig struct {
	Client             *completion.Client
	Engine             *scripting.Engine
	HookSystem         *hooks.System
	SystemPrompt       string
	Logger             *slog.Logger
	MaxDepth           int
	IterationsPerDepth []int
	AgentResolver      AgentResolver
	ClientFactory      ClientFactory
	TaskStore          *TaskStore
}

// NewDelegator creates a new Delegator.
func NewDelegator(cfg DelegatorConfig) *Delegator {
	if cfg.Logger == nil {
		cfg.Logger = slog.Default().With("component", "delegate")
	}
	if cfg.Engine == nil {
		cfg.Engine = scripting.NewEngine()
	}
	maxDepth := cfg.MaxDepth
	if maxDepth <= 0 {
		maxDepth = MaxDelegationDepth
	}
	if maxDepth > MaxHardDepth {
		maxDepth = MaxHardDepth
	}
	iterPerDepth := cfg.IterationsPerDepth
	if len(iterPerDepth) == 0 {
		iterPerDepth = []int{20, 10, 5, 3}
	}
	return &Delegator{
		client:             cfg.Client,
		engine:             cfg.Engine,
		hookSystem:         cfg.HookSystem,
		systemPrompt:       cfg.SystemPrompt,
		logger:             cfg.Logger,
		maxDepth:           maxDepth,
		iterationsPerDepth: iterPerDepth,
		agentResolver:      cfg.AgentResolver,
		clientFactory:      cfg.ClientFactory,
		taskStore:          cfg.TaskStore,
	}
}

// Execute spins up a delegate agent with the specified tools and hooks, then runs the task.
func (d *Delegator) Execute(ctx context.Context, req Request) (*Result, error) {
	// Check depth limit
	currentDepth := GetDepth(ctx)
	if currentDepth >= d.maxDepth {
		return nil, fmt.Errorf("delegation depth limit reached (%d/%d)", currentDepth, d.maxDepth)
	}

	// Resolve named agent if specified
	if req.Agent != "" && d.agentResolver != nil {
		resolved, err := d.agentResolver(req.Agent)
		if err != nil {
			return nil, fmt.Errorf("resolve agent %q: %w", req.Agent, err)
		}
		// Merge: agent provides base, request overrides/adds
		if req.SystemPrompt == "" {
			req.SystemPrompt = resolved.SystemPrompt
		}
		if req.Model == "" {
			req.Model = resolved.Model
		}
		// Merge tools: agent tools + request tools (request tools override by name)
		req.Tools = mergeToolSpecs(resolved.Tools, req.Tools)
		// Merge hooks: agent hooks + request hooks
		req.Hooks = append(resolved.Hooks, req.Hooks...)
	}

	// Resolve model-specific client
	client := d.client
	if req.Model != "" && d.clientFactory != nil {
		modelClient, err := d.clientFactory(req.Model)
		if err != nil {
			d.logger.Warn("model not found in registry, using default",
				"requested_model", req.Model)
		} else {
			client = modelClient
		}
	}

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

	if len(req.Tools) == 0 {
		return nil, fmt.Errorf("delegate requires at least one tool (provide tools or use a named agent)")
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

	// Determine max iterations for this depth level
	maxIter := MaxDelegateToolIterations
	nextDepth := currentDepth + 1
	if nextDepth < len(d.iterationsPerDepth) {
		maxIter = d.iterationsPerDepth[nextDepth]
	}
	if maxIter <= 0 {
		maxIter = 3
	}

	delegateAgent := agent.New(agent.Options{
		Client:            client,
		Tools:             registry,
		Hooks:             hookSystem,
		Context:           ctxMgr,
		Logger:            d.logger,
		MaxToolIterations: maxIter,
	})

	// Propagate depth to child context
	childCtx := WithDepth(ctx, nextDepth)
	turnResult, err := delegateAgent.Run(childCtx, req.Task)
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
	rv := reflect.ValueOf(target)
	if rv.Kind() != reflect.Pointer || rv.IsNil() {
		return fmt.Errorf("target must be a non-nil pointer")
	}

	// If payload is a map (from Starlark scripts), merge onto target preserving unspecified fields.
	if m, isMap := payload.(map[string]interface{}); isMap {
		data, err := json.Marshal(m)
		if err != nil {
			return err
		}
		return json.Unmarshal(data, target)
	}

	// If payload is a Go struct (from Go hooks), replace the target entirely.
	data, err := json.Marshal(payload)
	if err != nil {
		return err
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
			return "", fmt.Errorf("parse delegate args (expected JSON with 'task' and 'tools' fields): %w", err)
		}

		if req.Task == "" {
			return "", fmt.Errorf("delegate requires a 'task' field describing what the sub-agent should do")
		}
		// Tools can come from agent resolution, so don't require them upfront
		if len(req.Tools) == 0 && req.Agent == "" {
			return "", fmt.Errorf("delegate requires 'tools' array (each with name, script, parameters) or an 'agent' name")
		}

		result, err := d.Execute(ctx, req)
		if err != nil {
			return "", err
		}

		// Return a clearly structured response so the parent agent knows delegation completed
		if result.Response == "" {
			return "DELEGATION COMPLETE: sub-agent finished but produced no output", nil
		}
		return fmt.Sprintf("DELEGATION COMPLETE: %s", result.Response), nil
	}
}

// CreateAsyncDelegateHandlers creates handlers for async delegation tools.
func (d *Delegator) CreateAsyncDelegateHandlers() map[string]tools.Handler {
	handlers := make(map[string]tools.Handler)

	handlers["delegate_async"] = func(ctx context.Context, args json.RawMessage) (string, error) {
		var req Request
		if err := json.Unmarshal(args, &req); err != nil {
			return "", fmt.Errorf("parse delegate_async args: %w", err)
		}
		if req.Task == "" {
			return "", fmt.Errorf("delegate_async requires a task")
		}
		if len(req.Tools) == 0 && req.Agent == "" {
			return "", fmt.Errorf("delegate_async requires tools or an agent name")
		}

		if d.taskStore == nil {
			return "", fmt.Errorf("async delegation not configured (no task store)")
		}

		taskID := fmt.Sprintf("task_%d", time.Now().UnixNano())
		entry, err := d.taskStore.Submit(taskID)
		if err != nil {
			return "", fmt.Errorf("submit async task: %w", err)
		}

		// Launch in background goroutine
		go func() {
			result, err := d.Execute(ctx, req)
			if err != nil {
				d.taskStore.Fail(entry.ID, err)
			} else {
				d.taskStore.Complete(entry.ID, result)
			}
		}()

		return fmt.Sprintf(`{"task_id": "%s", "status": "running"}`, taskID), nil
	}

	handlers["delegate_status"] = func(ctx context.Context, args json.RawMessage) (string, error) {
		var params struct {
			TaskID string `json:"task_id"`
		}
		if err := json.Unmarshal(args, &params); err != nil {
			return "", err
		}
		if d.taskStore == nil {
			return "", fmt.Errorf("async delegation not configured")
		}
		entry, ok := d.taskStore.Get(params.TaskID)
		if !ok {
			return fmt.Sprintf(`{"error": "task %s not found"}`, params.TaskID), nil
		}
		return fmt.Sprintf(`{"task_id": "%s", "status": "%s"}`, entry.ID, entry.Status), nil
	}

	handlers["delegate_result"] = func(ctx context.Context, args json.RawMessage) (string, error) {
		var params struct {
			TaskID string `json:"task_id"`
		}
		if err := json.Unmarshal(args, &params); err != nil {
			return "", err
		}
		if d.taskStore == nil {
			return "", fmt.Errorf("async delegation not configured")
		}
		entry, ok := d.taskStore.Get(params.TaskID)
		if !ok {
			return "", fmt.Errorf("task %s not found", params.TaskID)
		}
		if entry.Status == TaskStatusRunning {
			return fmt.Sprintf(`{"task_id": "%s", "status": "running", "message": "task still in progress"}`, entry.ID), nil
		}
		if entry.Status == TaskStatusFailed {
			return fmt.Sprintf(`{"task_id": "%s", "status": "failed", "error": "%s"}`, entry.ID, entry.Error.Error()), nil
		}
		return entry.Result.Response, nil
	}

	handlers["delegate_await"] = func(ctx context.Context, args json.RawMessage) (string, error) {
		var params struct {
			TaskIDs string `json:"task_ids"`
		}
		if err := json.Unmarshal(args, &params); err != nil {
			return "", err
		}
		if d.taskStore == nil {
			return "", fmt.Errorf("async delegation not configured")
		}

		ids := splitCSV(params.TaskIDs)
		entries, err := d.taskStore.WaitMultiple(ctx, ids)
		if err != nil {
			return "", err
		}

		var results []string
		for _, entry := range entries {
			if entry == nil {
				continue
			}
			switch entry.Status {
			case TaskStatusCompleted:
				results = append(results, fmt.Sprintf(`{"task_id": "%s", "status": "completed", "response": %s}`,
					entry.ID, jsonString(entry.Result.Response)))
			case TaskStatusFailed:
				results = append(results, fmt.Sprintf(`{"task_id": "%s", "status": "failed", "error": "%s"}`,
					entry.ID, entry.Error.Error()))
			}
		}

		return "[" + joinStrings(results, ",") + "]", nil
	}

	return handlers
}

// DelegateToolDefinition returns the tool definition for the built-in delegate meta-tool.
func DelegateToolDefinition() tools.Definition {
	return tools.Definition{
		Name: "delegate",
		Description: `Spin up a sub-agent to handle a task. You can specify a named agent or provide inline tools/hooks.
If using a named agent, its tools, hooks, model, and system prompt are loaded automatically.
You can override model or add extra tools/hooks on top of the agent's defaults.`,
		Parameters: []tools.Parameter{
			{Name: "task", Type: tools.TypeString, Description: "What you want the delegate to accomplish", Required: true},
			{Name: "agent", Type: tools.TypeString, Description: "Name of a custom agent to use (from .harness/agents/)", Required: false},
			{Name: "model", Type: tools.TypeString, Description: "Override model for this delegate", Required: false},
			{
				Name:        "tools",
				Type:        tools.TypeArray,
				Description: "Array of tool definitions (required if no agent specified)",
				Required:    false,
				Items: &tools.ParameterSchema{
					Type: tools.TypeObject,
					Properties: map[string]*tools.ParameterSchema{
						"name":        {Type: tools.TypeString, Description: "Tool name"},
						"description": {Type: tools.TypeString, Description: "What the tool does"},
						"parameters":  {Type: tools.TypeObject, Description: "Tool parameters as JSON Schema properties"},
						"script":      {Type: tools.TypeString, Description: "Starlark script implementing run(args)"},
					},
					Required: []string{"name", "script"},
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
						"when":     {Type: tools.TypeString, Description: "Optional Starlark expression"},
						"priority": {Type: tools.TypeNumber, Description: "Execution priority (lower = first)"},
					},
					Required: []string{"event", "handler", "script"},
				},
			},
			{Name: "system_prompt", Type: tools.TypeString, Description: "System prompt override (uses agent's prompt if omitted)", Required: false},
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

// mergeToolSpecs merges base tools with override tools. Overrides win on name collision.
func mergeToolSpecs(base, overrides []ToolSpec) []ToolSpec {
	if len(overrides) == 0 {
		return base
	}
	if len(base) == 0 {
		return overrides
	}

	seen := make(map[string]int, len(base))
	merged := make([]ToolSpec, len(base))
	copy(merged, base)
	for i, t := range merged {
		seen[t.Name] = i
	}

	for _, t := range overrides {
		if idx, exists := seen[t.Name]; exists {
			merged[idx] = t
		} else {
			merged = append(merged, t)
		}
	}
	return merged
}

func splitCSV(s string) []string {
	var parts []string
	for _, p := range strings.Split(s, ",") {
		p = strings.TrimSpace(p)
		if p != "" {
			parts = append(parts, p)
		}
	}
	return parts
}

func joinStrings(ss []string, sep string) string {
	return strings.Join(ss, sep)
}

func jsonString(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}
