package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/htekdev/ai-harness/artifact"
	"github.com/htekdev/ai-harness/config"
	"github.com/htekdev/ai-harness/observe"
)

func cmdContext(args []string) error {
	fs := flag.NewFlagSet("context", flag.ExitOnError)
	dir := fs.String("dir", ".", "Project directory to scan")
	verbose := fs.Bool("verbose", false, "Show detailed provenance for each section")
	fs.BoolVar(verbose, "v", false, "Show detailed provenance (shorthand)")
	json := fs.Bool("json", false, "Output as JSON")
	budget := fs.Int("budget", 128000, "Token budget for the context window")
	agent := fs.String("agent", "", "Resolve context for a specific agent")

	if err := fs.Parse(args); err != nil {
		return err
	}

	baseDir := filepath.Join(*dir, ".harness")

	// Load artifacts from the project
	reg, err := artifact.LoadAndRegister(baseDir)
	if err != nil {
		if os.IsNotExist(err) || reg == nil {
			fmt.Println("No artifacts found — nothing to observe.")
			fmt.Println()
			fmt.Println("Run 'harness init' to scaffold a project with artifacts,")
			fmt.Println("or create artifact files in .harness/ to get started.")
			return nil
		}
		return fmt.Errorf("load artifacts: %w", err)
	}

	// Compose artifacts (evaluate conditions)
	composer := artifact.NewComposer(reg)
	composed, err := composer.Compose(nil) // nil = all active (no runtime conditions yet)
	if err != nil {
		return fmt.Errorf("compose: %w", err)
	}

	// If agent-specific resolution is requested, filter further
	if *agent != "" {
		// For now, note the agent name in output — full agent-specific resolution
		// will come when agent-scoped artifacts are supported.
		fmt.Fprintf(os.Stderr, "note: resolving context for agent %q (base composition)\n", *agent)
	}

	// Build the observability snapshot
	builder := observe.NewBuilder(*budget)
	snap := builder.Build(reg, composed)

	// Output
	var mode observe.FormatMode
	switch {
	case *json:
		mode = observe.FormatJSON
	case *verbose:
		mode = observe.FormatDetailed
	default:
		mode = observe.FormatSummary
	}

	fmt.Print(snap.Format(mode))

	// Show declarative context sources from harness.md (if any)
	showContextSources(*dir)

	return nil
}

// showContextSources prints declared context sources from harness.md (if any).
// Non-fatal: silently skips if harness.md is missing or has no sources.
func showContextSources(dir string) {
	cfgPath := filepath.Join(dir, "harness.md")
	cfg, err := config.Load(cfgPath)
	if err != nil || len(cfg.Context.Sources) == 0 {
		return
	}

	sources := cfg.Context.Sources
	fmt.Printf("\nContext Sources (%d)\n", len(sources))
	fmt.Println("─────────────────────────────────────────────────────────────")
	for _, s := range sources {
		loc := s.Path
		if s.Type == "url" {
			loc = s.URL
		}
		fmt.Printf("  • %s [%s] %s\n", s.Name, s.Type, loc)
		if s.When != "" {
			fmt.Printf("      when: %s\n", s.When)
		} else {
			fmt.Printf("      always active\n")
		}
		if s.Trigger != "" {
			fmt.Printf("      trigger: %s\n", s.Trigger)
		}
	}
}
