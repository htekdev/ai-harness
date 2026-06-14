package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/htekdev/ai-harness/agent"
	"github.com/htekdev/ai-harness/harness"
)

func cmdRun(args []string) error {
	fs := flag.NewFlagSet("run", flag.ExitOnError)
	configPath := fs.String("config", "", "Path to harness config file")
	fs.StringVar(configPath, "c", "", "Path to harness config file (shorthand)")
	stream := fs.Bool("stream", false, "Stream model tokens to the terminal as they arrive (Phase 5.4)")
	fs.Parse(args)

	cfgPath := resolveConfig(*configPath)

	h, err := harness.New(cfgPath)
	if err != nil {
		return fmt.Errorf("loading harness from %s: %w", cfgPath, err)
	}

	ctx := context.Background()
	if err := h.RunSession(ctx); err != nil {
		return fmt.Errorf("starting session: %w", err)
	}
	defer h.EndSession(ctx)

	fmt.Println("🤖 AI Harness — Interactive Mode")
	fmt.Printf("   config: %s\n", cfgPath)
	if *stream {
		fmt.Println("   streaming: ON (tokens appear live; usage counters not reported)")
	}
	fmt.Println("   Type 'quit' to exit, '/tools' to list tools, '/hooks' to list hooks")
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

		// Built-in slash commands
		switch input {
		case "quit", "exit":
			fmt.Println("Goodbye!")
			return nil
		case "/tools":
			printToolList(h)
			continue
		case "/hooks":
			printHookList(h)
			continue
		case "/agents":
			printAgentList(h)
			continue
		case "/help":
			fmt.Println("Commands: /tools, /hooks, /agents, /help, quit")
			continue
		}

		var result *agentTurnResult
		if *stream {
			result, err = runStreaming(ctx, h, input)
		} else {
			result, err = runBatched(ctx, h, input)
		}
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			continue
		}

		if len(result.ToolCalls) > 0 {
			fmt.Printf("\n📎 Tool calls: %d\n", len(result.ToolCalls))
			for _, tc := range result.ToolCalls {
				fmt.Printf("   → %s(%s)\n", tc.Name, string(tc.Arguments))
			}
		}

		if !*stream {
			// In batch mode the response wasn't printed yet; print it now.
			fmt.Printf("\n%s\n", result.Response)
		} else {
			// In stream mode tokens already printed inline; just terminate the line.
			fmt.Println()
		}

		if !*stream {
			fmt.Printf("\n[tokens: %d prompt + %d completion = %d total]\n",
				result.Usage.PromptTokens, result.Usage.CompletionTokens, result.Usage.TotalTokens)
		}
	}

	return nil
}

// agentTurnResult is a thin alias kept local to the CLI to avoid importing
// the agent package's public symbol name into a function signature.
type agentTurnResult = agent.TurnResult

func runBatched(ctx context.Context, h *harness.Harness, input string) (*agentTurnResult, error) {
	return h.Run(ctx, input)
}

func runStreaming(ctx context.Context, h *harness.Harness, input string) (*agentTurnResult, error) {
	// Print tokens as they arrive. We rely on bufio-style flush of os.Stdout
	// (line-buffered on terminals, otherwise block-buffered) — for an
	// interactive TTY this is good enough. If users redirect stdout, they
	// will see the same final text just with less granular flushing.
	return h.RunStream(ctx, input, func(delta string) {
		fmt.Print(delta)
	})
}

func printToolList(h *harness.Harness) {
	// Access tool registry through the agent
	fmt.Println("\n📦 Registered Tools:")
	fmt.Println("   (use 'harness tools' for detailed listing)")
}

func printHookList(h *harness.Harness) {
	fmt.Println("\n🪝 Registered Hooks:")
	fmt.Println("   (use 'harness hooks' for detailed listing)")
}

func printAgentList(h *harness.Harness) {
	agents := h.Agents()
	if len(agents) == 0 {
		fmt.Println("\n🤖 No agents configured")
		return
	}
	fmt.Printf("\n🤖 Configured Agents (%d):\n", len(agents))
	for name, agent := range agents {
		model := agent.Model
		if model == "" {
			model = "(default)"
		}
		fmt.Printf("   • %s [model: %s]\n", name, model)
	}
}
