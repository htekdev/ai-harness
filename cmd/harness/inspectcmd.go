package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/htekdev/ai-harness/artifact"
	"github.com/htekdev/ai-harness/config"
)

func cmdInspect(args []string) error {
	fs := flag.NewFlagSet("inspect", flag.ExitOnError)
	configPath := fs.String("config", "", "Path to harness config file")
	fs.StringVar(configPath, "c", "", "Path to harness config file (shorthand)")
	dir := fs.String("dir", ".", "Project directory to inspect")
	verbose := fs.Bool("verbose", false, "Show detailed component information")
	fs.BoolVar(verbose, "v", false, "Show detailed component information (shorthand)")
	events := fs.Bool("events", false, "Show recent events (placeholder — requires runtime)")
	failures := fs.Bool("failures", false, "Show recent failures (placeholder — requires runtime)")
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, `Usage: harness inspect [flags]

Show a comprehensive snapshot of the harness: configuration, tools, hooks,
agents, and registered artifacts. Use after deploy to observe runtime state.

Flags:
  -c, --config <path>   Path to harness config (default: harness.md)
      --dir <path>      Project directory to scan (default: .)
  -v, --verbose         Show detailed information for each component
      --events          Show recent runtime events
      --failures        Show recent runtime failures

Examples:
  harness inspect
  harness inspect --verbose
  harness inspect --events
  harness inspect --failures

Golden path:
  harness deploy        — run the harness
  harness inspect       ← you are here

`)
	}
	fs.Parse(args)

	cfgPath := resolveConfigFromDir(*dir, *configPath)
	start := time.Now()

	// ── Configuration ─────────────────────────────────────────────────────────
	cfg, agents, err := config.LoadFull(cfgPath)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	fmt.Println("🔭 Harness Inspect")
	fmt.Println(strings.Repeat("─", 60))
	fmt.Printf("  Config:   %s\n", cfgPath)
	fmt.Printf("  Model:    %s (provider: %s)\n", cfg.Model.Name, cfg.Model.Provider)

	apiKey := cfg.ResolveAPIKey()
	if apiKey != "" {
		fmt.Println("  API key:  ✓ configured")
	} else {
		fmt.Println("  API key:  ⚠️  not configured — run 'harness validate' for details")
	}
	fmt.Println()

	// ── Tools ─────────────────────────────────────────────────────────────────
	toolCount := len(cfg.Tools)
	fmt.Printf("📦 Tools (%d)\n", toolCount)
	if toolCount > 0 {
		tools := make([]config.ToolConfig, len(cfg.Tools))
		copy(tools, cfg.Tools)
		sort.Slice(tools, func(i, j int) bool { return tools[i].Name < tools[j].Name })
		for _, t := range tools {
			if *verbose {
				desc := t.Description
				if desc == "" {
					desc = "(no description)"
				}
				fmt.Printf("  %-24s %s\n", t.Name, desc)
			} else {
				fmt.Printf("  • %s\n", t.Name)
			}
		}
	}
	fmt.Println()

	// ── Hooks ─────────────────────────────────────────────────────────────────
	hookCount := len(cfg.Hooks)
	fmt.Printf("🪝 Hooks (%d)\n", hookCount)
	if hookCount > 0 {
		hooks := make([]config.HookConfig, len(cfg.Hooks))
		copy(hooks, cfg.Hooks)
		sort.Slice(hooks, func(i, j int) bool {
			pi, pj := hooks[i].Priority, hooks[j].Priority
			if pi == 0 {
				pi = 100
			}
			if pj == 0 {
				pj = 100
			}
			return pi < pj
		})
		for _, h := range hooks {
			if *verbose {
				fmt.Printf("  %-24s event=%-20s priority=%d\n", h.Handler, h.Event, h.Priority)
			} else {
				fmt.Printf("  • %s\n", h.Handler)
			}
		}
	}
	fmt.Println()

	// ── Agents ────────────────────────────────────────────────────────────────
	agentCount := len(agents)
	fmt.Printf("🤖 Agents (%d)\n", agentCount)
	if agentCount > 0 {
		names := make([]string, 0, agentCount)
		for n := range agents {
			names = append(names, n)
		}
		sort.Strings(names)
		for _, name := range names {
			a := agents[name]
			model := a.Model
			if model == "" {
				model = "(default)"
			}
			if *verbose {
				fmt.Printf("  %-24s model=%s  tools=%d  hooks=%d\n",
					name, model, len(a.Tools), len(a.Hooks))
			} else {
				fmt.Printf("  • %s [%s]\n", name, model)
			}
		}
	}
	fmt.Println()

	// ── Artifacts ─────────────────────────────────────────────────────────────
	baseDir := filepath.Join(*dir, ".harness")
	reg, regErr := artifact.LoadAndRegister(baseDir)
	if regErr == nil && reg != nil {
		artCount := reg.Count()
		fmt.Printf("🗂  Artifacts (%d)\n", artCount)
		if artCount > 0 && *verbose {
			for _, a := range reg.All() {
				version := ""
				if a.Metadata.Version != "" {
					version = " v" + a.Metadata.Version
				}
				fmt.Printf("  [%-8s] %s%s\n", a.Metadata.Type, a.Metadata.Name, version)
			}
		}
		fmt.Println()
	}

	// ── Runtime state (future) ─────────────────────────────────────────────────
	if *events {
		fmt.Println("📡 Events")
		fmt.Println("  (runtime event log requires a running harness session)")
		fmt.Println()
	}

	if *failures {
		fmt.Println("💥 Failures")
		fmt.Println("  (failure log requires a running harness session)")
		fmt.Println()
	}

	// ── Summary ───────────────────────────────────────────────────────────────
	elapsed := time.Since(start)
	fmt.Println(strings.Repeat("─", 60))
	issues := 0
	if err := cfg.Validate(); err != nil {
		issues++
	}
	if cfg.ResolveAPIKey() == "" {
		issues++
	}
	if issues > 0 {
		fmt.Printf("⚠️  %d issue(s) detected — run 'harness validate' for details [%s]\n",
			issues, elapsed.Round(time.Millisecond))
	} else {
		fmt.Printf("✅ Harness looks healthy (%d tools, %d hooks, %d agents) [%s]\n",
			toolCount, hookCount, agentCount, elapsed.Round(time.Millisecond))
	}

	return nil
}
