// Command example demonstrates the AI harness with a simple interactive loop.
package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/htekdev/ai-harness/harness"
)

func main() {
	// Detect config: prefer harness.md, fall back to harness.yaml
	configPath := "harness.md"
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		configPath = "harness.yaml"
	}

	// Load harness from config (includes tools, hooks, agents, and delegate meta-tool)
	h, err := harness.New(configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading harness: %v\n", err)
		os.Exit(1)
	}

	// Start session
	ctx := context.Background()
	if err := h.RunSession(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "Error starting session: %v\n", err)
		os.Exit(1)
	}
	defer h.EndSession(ctx)

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

		result, err := h.Run(ctx, input)
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
