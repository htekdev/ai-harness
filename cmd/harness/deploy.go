package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/htekdev/ai-harness/harness"
)

func cmdDeploy(args []string) error {
	fs := flag.NewFlagSet("deploy", flag.ExitOnError)
	configPath := fs.String("config", "", "Path to harness config file")
	fs.StringVar(configPath, "c", "", "Path to harness config file (shorthand)")
	input := fs.String("input", "", "Input prompt (reads from stdin if not set)")
	dryRun := fs.Bool("dry-run", false, "Validate and show plan without running the harness")
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, `Usage: harness deploy [flags]

Run the harness with a single input — non-interactive, suitable for CI/CD.

Flags:
  -c, --config <path>   Path to harness config (default: harness.md)
      --input <text>    Input prompt (reads from stdin if not provided)
      --dry-run         Validate and show plan without calling the LLM

Examples:
  harness deploy --input "summarize the repo"
  echo "list all tools" | harness deploy
  harness deploy --dry-run

Golden path:
  harness scaffold      — create project
  harness validate      — verify configuration
  harness deploy        ← you are here
  harness inspect       — observe runtime state

`)
	}
	fs.Parse(args)

	cfgPath := resolveConfig(*configPath)

	// Dry-run: validate only, show verbose summary
	if *dryRun {
		fmt.Printf("🔍 Dry-run: validating %s (no LLM call)\n\n", cfgPath)
		return cmdValidate([]string{"--verbose", "-c", cfgPath})
	}

	// Resolve prompt from --input or stdin
	prompt := strings.TrimSpace(*input)
	if prompt == "" {
		if isStdinPiped() {
			scanner := bufio.NewScanner(os.Stdin)
			var lines []string
			for scanner.Scan() {
				lines = append(lines, scanner.Text())
			}
			prompt = strings.TrimSpace(strings.Join(lines, "\n"))
		}
	}

	if prompt == "" {
		return fmt.Errorf("no input — use --input or pipe to stdin\n\nTip: harness deploy --dry-run to validate without running")
	}

	h, err := harness.New(cfgPath)
	if err != nil {
		return fmt.Errorf("loading harness from %s: %w", cfgPath, err)
	}

	ctx := context.Background()
	if err := h.RunSession(ctx); err != nil {
		return fmt.Errorf("starting session: %w", err)
	}
	defer h.EndSession(ctx)

	result, err := h.Run(ctx, prompt)
	if err != nil {
		return fmt.Errorf("deploy: %w", err)
	}

	fmt.Print(result.Response)

	if len(result.ToolCalls) > 0 || result.Usage.TotalTokens > 0 {
		fmt.Fprintf(os.Stderr, "\n[tool calls: %d | tokens: %d]\n",
			len(result.ToolCalls), result.Usage.TotalTokens)
	}

	return nil
}

// isStdinPiped returns true when stdin is not a terminal (i.e. data is being piped in).
func isStdinPiped() bool {
	fi, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	return (fi.Mode() & os.ModeCharDevice) == 0
}
