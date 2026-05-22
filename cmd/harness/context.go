package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/htekdev/ai-harness/artifact"
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
	return nil
}
