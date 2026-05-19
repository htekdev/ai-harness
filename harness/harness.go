// Package harness provides a high-level API for constructing and running
// an AI harness from configuration.
package harness

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/htekdev/ai-harness/agent"
	"github.com/htekdev/ai-harness/completion"
	"github.com/htekdev/ai-harness/config"
	agentctx "github.com/htekdev/ai-harness/context"
	"github.com/htekdev/ai-harness/hooks"
	"github.com/htekdev/ai-harness/tools"
)

// Harness ties configuration, completion, hooks, context, tools, and the agent together.
type Harness struct {
	cfg        *config.Config
	client     *completion.Client
	registry   *tools.Registry
	hookSystem *hooks.System
	ctxMgr     *agentctx.Manager
	agent      *agent.Agent
}

// New loads a harness from a configuration file.
func New(configPath string) (*Harness, error) {
	cfg, err := config.Load(configPath)
	if err != nil {
		return nil, err
	}
	return NewFromConfig(cfg)
}

// NewFromConfig constructs a harness from a parsed configuration.
func NewFromConfig(cfg *config.Config) (*Harness, error) {
	if cfg == nil {
		return nil, fmt.Errorf("config cannot be nil")
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	apiKey := cfg.ResolveAPIKey()
	if apiKey == "" {
		return nil, fmt.Errorf("environment variable %q is not set", cfg.Model.APIKeyEnv)
	}

	registry := tools.NewRegistry()
	for _, toolCfg := range cfg.Tools {
		def := definitionFromConfig(toolCfg)
		if err := registry.Register(def, unimplementedToolHandler(toolCfg.Name)); err != nil {
			return nil, fmt.Errorf("register config tool %q: %w", toolCfg.Name, err)
		}
	}

	hookSystem := hooks.NewSystem()
	for _, hookCfg := range cfg.Hooks {
		hookSystem.Register(hooks.Registration{
			Name:     hookCfg.Handler,
			Event:    hooks.Event(hookCfg.Event),
			Priority: 100,
			Handler:  unimplementedHookHandler(hookCfg.Handler),
		})
	}

	client := completion.NewClient(completion.ClientConfig{
		BaseURL:    cfg.BaseURL(),
		APIKey:     apiKey,
		Model:      cfg.Model.Name,
		MaxRetries: 3,
		Timeout:    60 * time.Second,
	})

	ctxMgr := agentctx.NewManager(agentctx.Config{
		SystemPrompt: cfg.Context.SystemPrompt,
		MaxMessages:  cfg.Context.MaxHistory,
		MaxTokens:    cfg.Context.MaxTokens,
	})

	h := &Harness{
		cfg:        cfg,
		client:     client,
		registry:   registry,
		hookSystem: hookSystem,
		ctxMgr:     ctxMgr,
	}
	if h.cfg == nil {
		h.cfg = &config.Config{}
	}

	h.agent = agent.New(agent.Options{
		Client:  client,
		Tools:   registry,
		Hooks:   hookSystem,
		Context: ctxMgr,
		Logger:  log.New(os.Stderr, "[harness] ", log.LstdFlags),
	})

	return h, nil
}

// Run executes a single agent turn.
func (h *Harness) Run(ctx context.Context, input string) (*agent.TurnResult, error) {
	return h.agent.Run(ctx, input)
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
