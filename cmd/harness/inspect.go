package main

import (
	"flag"
	"fmt"
	"sort"

	"github.com/htekdev/ai-harness/config"
)

func cmdTools(args []string) error {
	fs := flag.NewFlagSet("tools", flag.ExitOnError)
	configPath := fs.String("config", "", "Path to harness config file")
	fs.StringVar(configPath, "c", "", "Path to harness config file (shorthand)")
	verbose := fs.Bool("verbose", false, "Show tool parameters")
	fs.BoolVar(verbose, "v", false, "Show tool parameters (shorthand)")
	fs.Parse(args)

	cfgPath := resolveConfig(*configPath)

	cfg, _, err := config.LoadFull(cfgPath)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	if len(cfg.Tools) == 0 {
		fmt.Println("No tools configured.")
		return nil
	}

	// Sort tools by name
	tools := make([]config.ToolConfig, len(cfg.Tools))
	copy(tools, cfg.Tools)
	sort.Slice(tools, func(i, j int) bool {
		return tools[i].Name < tools[j].Name
	})

	fmt.Printf("📦 Tools (%d):\n\n", len(tools))
	for _, t := range tools {
		if *verbose {
			fmt.Printf("  %s\n", t.Name)
			if t.Description != "" {
				fmt.Printf("    %s\n", t.Description)
			}
			if len(t.Parameters) > 0 {
				fmt.Println("    Parameters:")
				for name, p := range t.Parameters {
					req := ""
					if p.Required {
						req = " (required)"
					}
					fmt.Printf("      • %s: %s%s\n", name, p.Type, req)
				}
			}
			if t.TimeoutMS > 0 {
				fmt.Printf("    Timeout: %dms\n", t.TimeoutMS)
			}
			fmt.Println()
		} else {
			desc := t.Description
			if len(desc) > 60 {
				desc = desc[:57] + "..."
			}
			paramCount := len(t.Parameters)
			fmt.Printf("  %-24s %s (%d params)\n", t.Name, desc, paramCount)
		}
	}

	return nil
}

func cmdHooks(args []string) error {
	fs := flag.NewFlagSet("hooks", flag.ExitOnError)
	configPath := fs.String("config", "", "Path to harness config file")
	fs.StringVar(configPath, "c", "", "Path to harness config file (shorthand)")
	verbose := fs.Bool("verbose", false, "Show hook details")
	fs.BoolVar(verbose, "v", false, "Show hook details (shorthand)")
	fs.Parse(args)

	cfgPath := resolveConfig(*configPath)

	cfg, _, err := config.LoadFull(cfgPath)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	if len(cfg.Hooks) == 0 {
		fmt.Println("No hooks configured.")
		return nil
	}

	// Sort hooks by priority
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

	fmt.Printf("🪝 Hooks (%d):\n\n", len(hooks))
	for _, h := range hooks {
		priority := h.Priority
		if priority == 0 {
			priority = 100
		}
		if *verbose {
			fmt.Printf("  %s\n", h.Handler)
			fmt.Printf("    Event:    %s\n", h.Event)
			fmt.Printf("    Priority: %d\n", priority)
			if h.When != "" {
				fmt.Printf("    When:     %s\n", h.When)
			}
			fmt.Println()
		} else {
			fmt.Printf("  %-24s event=%-20s priority=%d\n", h.Handler, h.Event, priority)
		}
	}

	return nil
}

func cmdAgents(args []string) error {
	fs := flag.NewFlagSet("agents", flag.ExitOnError)
	configPath := fs.String("config", "", "Path to harness config file")
	fs.StringVar(configPath, "c", "", "Path to harness config file (shorthand)")
	verbose := fs.Bool("verbose", false, "Show agent details")
	fs.BoolVar(verbose, "v", false, "Show agent details (shorthand)")
	fs.Parse(args)

	cfgPath := resolveConfig(*configPath)

	_, agents, err := config.LoadFull(cfgPath)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	if len(agents) == 0 {
		fmt.Println("No agents configured.")
		return nil
	}

	// Sort agents by name
	names := make([]string, 0, len(agents))
	for name := range agents {
		names = append(names, name)
	}
	sort.Strings(names)

	fmt.Printf("🤖 Agents (%d):\n\n", len(agents))
	for _, name := range names {
		agentCfg := agents[name]
		if *verbose {
			fmt.Printf("  %s\n", name)
			if agentCfg.Model != "" {
				fmt.Printf("    Model: %s\n", agentCfg.Model)
			}
			toolCount := len(agentCfg.Tools)
			hookCount := len(agentCfg.Hooks)
			fmt.Printf("    Tools: %d, Hooks: %d\n", toolCount, hookCount)
			if agentCfg.SystemPrompt != "" {
				preview := agentCfg.SystemPrompt
				if len(preview) > 80 {
					preview = preview[:77] + "..."
				}
				fmt.Printf("    Prompt: %s\n", preview)
			}
			fmt.Println()
		} else {
			model := agentCfg.Model
			if model == "" {
				model = "(default)"
			}
			fmt.Printf("  %-24s model=%s\n", name, model)
		}
	}

	return nil
}
