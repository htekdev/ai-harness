// Command example demonstrates the AI harness with a simple interactive loop.
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/htekdev/ai-harness/agent"
	"github.com/htekdev/ai-harness/completion"
	"github.com/htekdev/ai-harness/config"
	agentctx "github.com/htekdev/ai-harness/context"
	"github.com/htekdev/ai-harness/hooks"
	"github.com/htekdev/ai-harness/tools"
)

func main() {
	// Load config
	cfg, err := config.Load("harness.yaml")
	if err != nil {
		// Fall back to defaults if no config file
		cfg = &config.Config{}
		cfg.Model.Provider = "copilot"
		cfg.Model.Name = "gpt-4o"
		cfg.Model.MaxTokens = 4096
		cfg.Model.Temperature = 0.7
		cfg.Model.APIKeyEnv = "GITHUB_TOKEN"
		cfg.Context.MaxHistory = 50
		cfg.Context.MaxTokens = 128000
		cfg.Context.SystemPrompt = "You are a helpful AI assistant powered by the AI Harness. You have access to tools and can help with a variety of tasks."
	}

	// Resolve API key
	apiKey := cfg.ResolveAPIKey()
	if apiKey == "" {
		fmt.Fprintf(os.Stderr, "Error: %s environment variable not set\n", cfg.Model.APIKeyEnv)
		os.Exit(1)
	}

	// Create completion client
	client := completion.NewClient(completion.ClientConfig{
		BaseURL:    cfg.BaseURL(),
		APIKey:     apiKey,
		Model:      cfg.Model.Name,
		MaxRetries: 3,
		Timeout:    60 * time.Second,
	})

	// Create tool registry with example tools
	registry := tools.NewRegistry()
	registerExampleTools(registry)

	// Create hook system with example hooks
	hookSystem := hooks.NewSystem()
	registerExampleHooks(hookSystem)

	// Create context manager
	ctxMgr := agentctx.NewManager(agentctx.Config{
		SystemPrompt: cfg.Context.SystemPrompt,
		MaxMessages:  cfg.Context.MaxHistory,
		MaxTokens:    cfg.Context.MaxTokens,
	})

	// Create agent
	a := agent.New(agent.Options{
		Client:  client,
		Tools:   registry,
		Hooks:   hookSystem,
		Context: ctxMgr,
		Logger:  log.New(os.Stderr, "[harness] ", log.LstdFlags),
	})

	// Start session
	ctx := context.Background()
	if err := a.RunSession(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "Error starting session: %v\n", err)
		os.Exit(1)
	}
	defer a.EndSession(ctx)

	// Interactive loop
	fmt.Println("🤖 AI Harness — Interactive Mode")
	fmt.Println("Type your message and press Enter. Type 'quit' to exit.")
	fmt.Println("---")

	scanner := bufio.NewScanner(os.Stdin)
	for {
		fmt.Print("\n> ")
		if !scanner.Scan() {
			break
		}

		input := strings.TrimSpace(scanner.Text())
		if input == "" {
			continue
		}
		if input == "quit" || input == "exit" {
			fmt.Println("Goodbye!")
			break
		}

		result, err := a.Run(ctx, input)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			continue
		}

		// Print tool calls if any
		if len(result.ToolCalls) > 0 {
			fmt.Printf("\n📎 Tool calls: %d\n", len(result.ToolCalls))
			for _, tc := range result.ToolCalls {
				fmt.Printf("   → %s(%s)\n", tc.Name, string(tc.Arguments))
			}
		}

		// Print response
		fmt.Printf("\n%s\n", result.Response)

		// Print usage
		fmt.Printf("\n[tokens: %d prompt + %d completion = %d total]\n",
			result.Usage.PromptTokens, result.Usage.CompletionTokens, result.Usage.TotalTokens)
	}
}

// registerExampleTools adds demonstration tools to the registry.
func registerExampleTools(registry *tools.Registry) {
	// Current time tool
	registry.Register(tools.Definition{
		Name:        "current_time",
		Description: "Get the current date and time",
		Parameters:  []tools.Parameter{},
	}, func(ctx context.Context, args json.RawMessage) (string, error) {
		return time.Now().Format("Monday, January 2, 2006 at 3:04 PM MST"), nil
	})

	// Echo tool
	registry.Register(tools.Definition{
		Name:        "echo",
		Description: "Echo back a message (useful for testing)",
		Parameters: []tools.Parameter{
			{Name: "message", Type: tools.TypeString, Description: "Message to echo back", Required: true},
		},
	}, func(ctx context.Context, args json.RawMessage) (string, error) {
		var params struct {
			Message string `json:"message"`
		}
		if err := json.Unmarshal(args, &params); err != nil {
			return "", fmt.Errorf("invalid arguments: %w", err)
		}
		return params.Message, nil
	})

	// Calculator tool
	registry.Register(tools.Definition{
		Name:        "calculate",
		Description: "Perform basic arithmetic (add, subtract, multiply, divide)",
		Parameters: []tools.Parameter{
			{Name: "operation", Type: tools.TypeString, Description: "Operation: add, subtract, multiply, divide", Required: true},
			{Name: "a", Type: tools.TypeNumber, Description: "First number", Required: true},
			{Name: "b", Type: tools.TypeNumber, Description: "Second number", Required: true},
		},
	}, func(ctx context.Context, args json.RawMessage) (string, error) {
		var params struct {
			Operation string  `json:"operation"`
			A         float64 `json:"a"`
			B         float64 `json:"b"`
		}
		if err := json.Unmarshal(args, &params); err != nil {
			return "", fmt.Errorf("invalid arguments: %w", err)
		}

		var result float64
		switch params.Operation {
		case "add":
			result = params.A + params.B
		case "subtract":
			result = params.A - params.B
		case "multiply":
			result = params.A * params.B
		case "divide":
			if params.B == 0 {
				return "", fmt.Errorf("division by zero")
			}
			result = params.A / params.B
		default:
			return "", fmt.Errorf("unknown operation: %s", params.Operation)
		}
		return fmt.Sprintf("%g", result), nil
	})
}

// registerExampleHooks adds demonstration hooks to the system.
func registerExampleHooks(sys *hooks.System) {
	// Audit log hook — logs all tool calls
	sys.Register(hooks.Registration{
		Name:     "audit-log",
		Event:    hooks.EventToolPre,
		Priority: 100,
		Handler: func(ctx context.Context, event hooks.Event, payload any) hooks.Result {
			if call, ok := payload.(*tools.Call); ok {
				log.Printf("[audit] tool call: %s (id: %s)", call.Name, call.ID)
			}
			return hooks.Result{Action: hooks.ActionContinue}
		},
	})

	// Session logging
	sys.Register(hooks.Registration{
		Name:     "session-log",
		Event:    hooks.EventSessionStart,
		Priority: 1,
		Handler: func(ctx context.Context, event hooks.Event, payload any) hooks.Result {
			log.Println("[session] started")
			return hooks.Result{Action: hooks.ActionContinue}
		},
	})

	sys.Register(hooks.Registration{
		Name:     "session-end-log",
		Event:    hooks.EventSessionEnd,
		Priority: 1,
		Handler: func(ctx context.Context, event hooks.Event, payload any) hooks.Result {
			log.Println("[session] ended")
			return hooks.Result{Action: hooks.ActionContinue}
		},
	})
}
