// Package harness provides a high-level API for constructing and running
// an AI harness from configuration.
package harness

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/htekdev/ai-harness/agent"
	"github.com/htekdev/ai-harness/completion"
	"github.com/htekdev/ai-harness/config"
	agentctx "github.com/htekdev/ai-harness/context"
	"github.com/htekdev/ai-harness/delegation"
	"github.com/htekdev/ai-harness/hooks"
	"github.com/htekdev/ai-harness/scripting"
	"github.com/htekdev/ai-harness/tools"
)

// Harness ties configuration, completion, hooks, context, tools, and the agent together.
type Harness struct {
	cfg        *config.Config
	client     *completion.Client
	registry   *tools.Registry
	hookSystem *hooks.System
	ctxMgr     *agentctx.Manager
	engine     *scripting.Engine
	delegator  *delegation.Delegator
	agent      *agent.Agent
	agents     map[string]*config.AgentConfig

	// baseDir is the directory containing the .harness/ folder. Used by
	// the self-augmenting meta-tools (harness_create_tool / _create_hook /
	// _remove_artifact) to write artifact files and by Reload() to
	// rescan them. Defaults to the directory of the config file passed
	// to New(); falls back to "." for NewFromConfig.
	baseDir string

	// reloadMu serializes Reload() against itself so concurrent meta-tool
	// calls (e.g. two create_tool calls in a single agent turn) cannot
	// race when they all trigger a rescan.
	reloadMu sync.Mutex

	// fileTools / fileHooks track artifacts loaded from disk so Reload()
	// can unregister entries that were deleted on-disk without touching
	// inline-config or built-in (delegate, harness_*) registrations.
	fileTools map[string]struct{}
	fileHooks map[string]struct{}
}

// New loads a harness from a configuration file (supports .md and .yaml).
func New(configPath string) (*Harness, error) {
	cfg, agents, err := config.LoadFull(configPath)
	if err != nil {
		return nil, err
	}
	h, err := NewFromConfig(cfg, agents)
	if err != nil {
		return nil, err
	}
	h.baseDir = filepath.Dir(configPath)
	// Track which tools/hooks came from the .harness/ directory so
	// Reload() can correctly diff disk state against the registry.
	if dirResult, derr := config.LoadDirectory(h.baseDir); derr == nil && dirResult != nil && dirResult.Config != nil {
		for _, t := range dirResult.Config.Tools {
			h.fileTools[t.Name] = struct{}{}
		}
		for _, hk := range dirResult.Config.Hooks {
			h.fileHooks[hk.Handler] = struct{}{}
		}
	}
	return h, nil
}

// NewFromConfig constructs a harness from a parsed configuration.
func NewFromConfig(cfg *config.Config, agents map[string]*config.AgentConfig) (*Harness, error) {
	if cfg == nil {
		return nil, fmt.Errorf("config cannot be nil")
	}
	if agents == nil {
		agents = make(map[string]*config.AgentConfig)
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	apiKey := cfg.ResolveAPIKey()
	if apiKey == "" {
		return nil, fmt.Errorf("environment variable %q is not set", cfg.Model.APIKeyEnv)
	}

	engine := scripting.NewEngine()
	if cfg.Network != nil && len(cfg.Network.AllowedDomains) > 0 {
		engine.SetNetworkSandbox(scripting.NewNetworkSandbox(cfg.Network.AllowedDomains))
	}
	registry := tools.NewRegistry()

	for _, toolCfg := range cfg.Tools {
		def := definitionFromConfig(toolCfg)
		var handler tools.Handler

		if toolCfg.Script != "" {
			h, err := scripting.NewToolHandler(engine, toolCfg.Name, toolCfg.Script)
			if err != nil {
				return nil, fmt.Errorf("compile tool script %q: %w", toolCfg.Name, err)
			}
			handler = h
		} else {
			handler = unimplementedToolHandler(toolCfg.Name)
		}
		if toolCfg.TimeoutMS > 0 {
			handler = tools.WithTimeout(handler, time.Duration(toolCfg.TimeoutMS)*time.Millisecond)
		}

		if err := registry.Register(def, handler); err != nil {
			return nil, fmt.Errorf("register config tool %q: %w", toolCfg.Name, err)
		}
	}

	if !cfg.ToolsPolicy.IsEmpty() {
		policy := &tools.Policy{
			Mode:  tools.PolicyMode(cfg.ToolsPolicy.Mode),
			Allow: cfg.ToolsPolicy.Allow,
			Deny:  cfg.ToolsPolicy.Deny,
		}
		if err := registry.SetPolicy(policy); err != nil {
			return nil, fmt.Errorf("apply tools_policy: %w", err)
		}
	}

	hookSystem := hooks.NewSystem()
	for _, hookCfg := range cfg.Hooks {
		var handler hooks.Handler

		if hookCfg.Script != "" {
			h, err := scripting.NewConditionalHookHandler(engine, hookCfg.Handler, hookCfg.When, hookCfg.Script)
			if err != nil {
				return nil, fmt.Errorf("compile hook script %q: %w", hookCfg.Handler, err)
			}
			handler = h
		} else {
			handler = unimplementedHookHandler(hookCfg.Handler)
		}

		priority := hookCfg.Priority
		if priority == 0 {
			priority = 100
		}

		hookSystem.Register(hooks.Registration{
			Name:     hookCfg.Handler,
			Event:    hooks.Event(hookCfg.Event),
			Priority: priority,
			Handler:  handler,
		})
	}

	client := completion.NewClient(completion.ClientConfig{
		BaseURL:     cfg.BaseURL(),
		APIKey:      apiKey,
		Model:       cfg.Model.Name,
		MaxRetries:  3,
		Timeout:     60 * time.Second,
		RetryPolicy: retryPolicyFromConfig(cfg.Model.Retry),
	})

	// Build model registry
	modelClients := make(map[string]*completion.Client)
	modelClients[cfg.Model.Name] = client // default model always available
	for _, modelCfg := range cfg.Models {
		if modelCfg.Name == cfg.Model.Name {
			continue // already registered as default
		}
		modelKey := modelCfg.APIKeyEnv
		if modelKey == "" {
			modelKey = cfg.Model.APIKeyEnv
		}
		mAPIKey := os.Getenv(modelKey)
		if mAPIKey == "" {
			mAPIKey = apiKey // fallback to main key
		}
		mBaseURL := modelCfg.BaseURL
		if mBaseURL == "" {
			mBaseURL = cfg.BaseURL()
		}
		mc := completion.NewClient(completion.ClientConfig{
			BaseURL:     mBaseURL,
			APIKey:      mAPIKey,
			Model:       modelCfg.Name,
			MaxRetries:  3,
			Timeout:     60 * time.Second,
			RetryPolicy: retryPolicyFromConfig(modelCfg.Retry),
		})
		modelClients[modelCfg.Name] = mc
	}

	clientFactory := func(modelName string) (*completion.Client, error) {
		if c, ok := modelClients[modelName]; ok {
			return c, nil
		}
		return nil, fmt.Errorf("model %q not found in registry", modelName)
	}

	// Build agent resolver
	agentResolver := buildAgentResolver(agents)

	// Create task store for async delegation
	maxConc := cfg.Delegation.MaxConcurrent
	if maxConc <= 0 {
		maxConc = 5
	}
	taskStore := delegation.NewTaskStore(maxConc, 5*time.Minute)

	ctxMgr := agentctx.NewManager(agentctx.Config{
		SystemPrompt: cfg.Context.SystemPrompt,
		MaxMessages:  cfg.Context.MaxHistory,
		MaxTokens:    cfg.Context.MaxTokens,
	})

	// Create delegator for the built-in delegate meta-tool
	delegator := delegation.NewDelegator(delegation.DelegatorConfig{
		Client:             client,
		Engine:             engine,
		HookSystem:         hookSystem,
		SystemPrompt:       cfg.Context.SystemPrompt,
		Logger:             Logger().With("component", "delegate"),
		MaxDepth:           cfg.Delegation.MaxDepth,
		IterationsPerDepth: cfg.Delegation.IterationsPerDepth,
		AgentResolver:      agentResolver,
		ClientFactory:      clientFactory,
		TaskStore:          taskStore,
	})

	// Register the delegate meta-tool
	delegateDef := delegation.DelegateToolDefinition()
	if err := registry.Register(delegateDef, delegator.CreateDelegateToolHandler()); err != nil {
		return nil, fmt.Errorf("register delegate tool: %w", err)
	}

	// Register async delegation tools
	asyncDefs := delegation.AsyncDelegateToolDefinitions()
	asyncHandlers := delegator.CreateAsyncDelegateHandlers()
	for _, def := range asyncDefs {
		if handler, ok := asyncHandlers[def.Name]; ok {
			if err := registry.Register(def, handler); err != nil {
				return nil, fmt.Errorf("register async tool %q: %w", def.Name, err)
			}
		}
	}

	h := &Harness{
		cfg:        cfg,
		client:     client,
		registry:   registry,
		hookSystem: hookSystem,
		ctxMgr:     ctxMgr,
		engine:     engine,
		delegator:  delegator,
		agents:     agents,
		baseDir:    ".",
		fileTools:  make(map[string]struct{}),
		fileHooks:  make(map[string]struct{}),
	}
	if h.cfg == nil {
		h.cfg = &config.Config{}
	}

	// Wire meta built-ins into the scripting engine.
	// This gives Starlark scripts runtime access to the tool registry, hook system, and agent configs.
	metaCfg := scripting.DefaultMetaConfig()
	if cfg.Meta != nil {
		metaCfg = scripting.MetaConfig{
			Enabled:      cfg.Meta.Enabled,
			MaxTools:     cfg.Meta.MaxTools,
			MaxHooks:     cfg.Meta.MaxHooks,
			MaxAgents:    cfg.Meta.MaxAgents,
			MaxCallDepth: cfg.Meta.MaxCallDepth,
		}
	}
	if metaCfg.Enabled {
		engine.SetMetaContext(&scripting.MetaContext{
			Registry:   registry,
			HookSystem: hookSystem,
			Agents:     agents,
			Engine:     engine,
			Config:     metaCfg,
		})
	}

	h.agent = agent.New(agent.Options{
		Client:  client,
		Tools:   registry,
		Hooks:   hookSystem,
		Context: ctxMgr,
		Logger:  Logger().With("component", "harness"),
		StopDelegate: func(ctx context.Context, request any) (*agent.TurnResult, error) {
			result, err := delegator.ExecuteControlFlow(ctx, request)
			if err != nil {
				return nil, err
			}
			return &agent.TurnResult{
				Response:    result.Response,
				ToolCalls:   result.ToolCalls,
				ToolResults: result.ToolResults,
			}, nil
		},
	})

	// Register self-augmenting meta-tools (Phase 5.8). These let the
	// model author its own tools, hooks, and context-augmenting files
	// at runtime through plain tool calls. See selfaugment.go.
	if err := registerSelfAugmentTools(h); err != nil {
		return nil, fmt.Errorf("register self-augment tools: %w", err)
	}

	// Augment the active context manager's system prompt with a short
	// note so the LLM is aware it can extend its own harness. We do this
	// AFTER the ctxMgr is constructed so we don't disturb the original
	// cfg.Context.SystemPrompt that gets persisted/audited elsewhere.
	if augmented := augmentSystemPromptForSelfAugment(cfg.Context.SystemPrompt); augmented != cfg.Context.SystemPrompt {
		ctxMgr.SetSystemPrompt(augmented)
	}

	return h, nil
}

// Run executes a single agent turn.
func (h *Harness) Run(ctx context.Context, input string) (*agent.TurnResult, error) {
	return h.agent.Run(ctx, input)
}

// RunStream executes a single agent turn with streaming token delivery.
// onDelta is invoked synchronously for each text delta as the model emits it.
// Tool execution semantics are identical to Run; only the assistant text
// is streamed. Token usage is not reported (most providers omit it on
// streaming responses) — fall back to Run when accurate accounting matters.
func (h *Harness) RunStream(ctx context.Context, input string, onDelta agent.StreamCallback) (*agent.TurnResult, error) {
	return h.agent.RunStream(ctx, input, onDelta)
}

// RunSession starts the session lifecycle.
func (h *Harness) RunSession(ctx context.Context) error {
	return h.agent.RunSession(ctx)
}

// EndSession ends the session lifecycle.
func (h *Harness) EndSession(ctx context.Context) {
	h.agent.EndSession(ctx)
}

// Agent returns the underlying agent.
func (h *Harness) Agent() *agent.Agent {
	return h.agent
}

// Agents returns the custom agent registry.
func (h *Harness) Agents() map[string]*config.AgentConfig {
	return h.agents
}

// RegisterTool registers or replaces a tool handler.
func (h *Harness) RegisterTool(def tools.Definition, handler tools.Handler) error {
	if _, exists := h.registry.Get(def.Name); exists {
		h.registry.Unregister(def.Name)
	}
	return h.registry.Register(def, handler)
}

// RegisterHook registers or replaces a named hook.
func (h *Harness) RegisterHook(reg hooks.Registration) {
	h.hookSystem.Unregister(reg.Name, reg.Event)
	h.hookSystem.Register(reg)
}

func definitionFromConfig(toolCfg config.ToolConfig) tools.Definition {
	params := make([]tools.Parameter, 0, len(toolCfg.Parameters))
	for name, paramCfg := range toolCfg.Parameters {
		params = append(params, tools.Parameter{
			Name:        name,
			Type:        tools.ParameterType(paramCfg.Type),
			Description: paramCfg.Description,
			Required:    paramCfg.Required,
		})
	}

	return tools.Definition{
		Name:        toolCfg.Name,
		Description: toolCfg.Description,
		Parameters:  params,
	}
}

func unimplementedToolHandler(name string) tools.Handler {
	return func(ctx context.Context, args json.RawMessage) (string, error) {
		return "", fmt.Errorf("tool %q is configured but no handler has been registered", name)
	}
}

func unimplementedHookHandler(name string) hooks.Handler {
	return func(ctx context.Context, event hooks.Event, payload any) hooks.Result {
		return hooks.Result{Action: hooks.ActionContinue}
	}
}

// buildAgentResolver creates a resolver that looks up agents from the loaded registry.
func buildAgentResolver(agents map[string]*config.AgentConfig) delegation.AgentResolver {
	return func(name string) (*delegation.ResolvedAgent, error) {
		agentCfg, ok := agents[name]
		if !ok {
			return nil, fmt.Errorf("agent %q not found (available: %v)", name, agentNames(agents))
		}

		var tools []delegation.ToolSpec
		for _, t := range agentCfg.Tools {
			if t.Ref != "" {
				// Reference to another tool — needs to be resolved from the global registry
				// For now, skip references that can't be resolved inline
				continue
			}
			if t.Inline != nil {
				params := make(map[string]delegation.ParamSpec, len(t.Inline.Parameters))
				for pName, pCfg := range t.Inline.Parameters {
					params[pName] = delegation.ParamSpec{
						Type:        pCfg.Type,
						Description: pCfg.Description,
						Required:    pCfg.Required,
					}
				}
				tools = append(tools, delegation.ToolSpec{
					Name:        t.Inline.Name,
					Description: t.Inline.Description,
					Parameters:  params,
					Script:      t.Inline.Script,
				})
			}
		}

		var hooks []delegation.HookSpec
		for _, h := range agentCfg.Hooks {
			if h.Ref != "" {
				// String reference — look up from loaded hooks
				hooks = append(hooks, delegation.HookSpec{
					Handler: h.Ref,
				})
			} else if h.Inline != nil {
				hooks = append(hooks, delegation.HookSpec{
					Event:    h.Inline.Event,
					Handler:  h.Inline.Handler,
					Script:   h.Inline.Script,
					When:     h.Inline.When,
					Priority: h.Inline.Priority,
				})
			}
		}

		return &delegation.ResolvedAgent{
			SystemPrompt: agentCfg.SystemPrompt,
			Model:        agentCfg.Model,
			Tools:        tools,
			Hooks:        hooks,
		}, nil
	}
}

func agentNames(agents map[string]*config.AgentConfig) []string {
	names := make([]string, 0, len(agents))
	for name := range agents {
		names = append(names, name)
	}
	return names
}
