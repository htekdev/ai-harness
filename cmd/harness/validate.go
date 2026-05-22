package main

import (
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/htekdev/ai-harness/config"
)

func cmdValidate(args []string) error {
	fs := flag.NewFlagSet("validate", flag.ExitOnError)
	configPath := fs.String("config", "", "Path to harness config file")
	fs.StringVar(configPath, "c", "", "Path to harness config file (shorthand)")
	verbose := fs.Bool("verbose", false, "Show detailed validation output")
	fs.BoolVar(verbose, "v", false, "Show detailed validation output (shorthand)")
	fs.Parse(args)

	cfgPath := resolveConfig(*configPath)

	start := time.Now()

	cfg, agents, err := config.LoadFull(cfgPath)
	if err != nil {
		return fmt.Errorf("failed to load %s: %w", cfgPath, err)
	}

	var issues []string

	// Validate base config
	if err := cfg.Validate(); err != nil {
		issues = append(issues, fmt.Sprintf("config validation: %v", err))
	}

	// Check API key env
	apiKey := cfg.ResolveAPIKey()
	if apiKey == "" {
		issues = append(issues, fmt.Sprintf("API key env %q is not set", cfg.Model.APIKeyEnv))
	}

	// Validate model configuration
	if cfg.Model.Name == "" {
		issues = append(issues, "model.name is required")
	}

	// Validate tools
	toolCount := 0
	for _, t := range cfg.Tools {
		toolCount++
		if t.Name == "" {
			issues = append(issues, "tool with empty name found")
		}
		if t.Script == "" {
			issues = append(issues, fmt.Sprintf("tool %q has no script handler", t.Name))
		}
	}

	// Validate hooks
	hookCount := 0
	for _, h := range cfg.Hooks {
		hookCount++
		if h.Handler == "" {
			issues = append(issues, "hook with empty handler name found")
		}
		if h.Event == "" {
			issues = append(issues, fmt.Sprintf("hook %q has no event type", h.Handler))
		}
	}

	// Validate agents
	agentCount := len(agents)
	for name, agentCfg := range agents {
		if agentCfg.SystemPrompt == "" {
			issues = append(issues, fmt.Sprintf("agent %q has no system prompt", name))
		}
	}

	elapsed := time.Since(start)

	if *verbose || len(issues) > 0 {
		fmt.Printf("Harness: %s\n", cfgPath)
		fmt.Printf("Model:   %s (provider: %s)\n", cfg.Model.Name, cfg.Model.Provider)
		fmt.Printf("Tools:   %d registered\n", toolCount)
		fmt.Printf("Hooks:   %d registered\n", hookCount)
		fmt.Printf("Agents:  %d configured\n", agentCount)
		fmt.Println()
	}

	if len(issues) > 0 {
		fmt.Fprintf(os.Stderr, "❌ Validation failed (%d issue%s):\n", len(issues), pluralize(len(issues)))
		for _, issue := range issues {
			fmt.Fprintf(os.Stderr, "   • %s\n", issue)
		}
		return fmt.Errorf("validation failed with %d issues", len(issues))
	}

	if *verbose {
		fmt.Printf("✅ Valid (%s)\n", elapsed.Round(time.Millisecond))
	} else {
		fmt.Printf("✅ %s — valid (%d tools, %d hooks, %d agents) [%s]\n",
			cfgPath, toolCount, hookCount, agentCount, elapsed.Round(time.Millisecond))
	}

	return nil
}

func pluralize(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

// resolveConfig finds the harness config file.
func resolveConfig(explicit string) string {
	if explicit != "" {
		return explicit
	}
	// Look for harness.md first, then harness.yaml
	candidates := []string{"harness.md", "harness.yaml"}
	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			return c
		}
	}
	// Default to harness.md (will error on load)
	return "harness.md"
}

// resolveConfigFromDir finds the config relative to a directory.
func resolveConfigFromDir(dir, explicit string) string {
	if explicit != "" {
		return explicit
	}
	candidates := []string{
		dir + "/harness.md",
		dir + "/harness.yaml",
	}
	for _, c := range candidates {
		// Normalize path separators
		c = strings.ReplaceAll(c, "\\", "/")
		if _, err := os.Stat(c); err == nil {
			return c
		}
	}
	return dir + "/harness.md"
}
