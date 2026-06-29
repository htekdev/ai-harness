package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/htekdev/ai-harness/artifact"
	"github.com/htekdev/ai-harness/compose"
	agentctx "github.com/htekdev/ai-harness/context"
	"github.com/htekdev/ai-harness/observe"
)

func cmdContext(args []string) error {
	// Dispatch sub-commands before parsing the default flag set.
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		switch args[0] {
		case "list":
			return cmdContextList(args[1:])
		default:
			return fmt.Errorf("unknown context sub-command %q (try: harness context list --active)", args[0])
		}
	}

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
	runtimeValues := map[string]any{}
	if *agent != "" {
		runtimeValues["agent.name"] = *agent
	}
	composed, err := composer.Compose(func(condition string) (bool, error) {
		return compose.EvaluateCondition(condition, compose.ConditionContext{Values: runtimeValues})
	})
	if err != nil {
		return fmt.Errorf("compose: %w", err)
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

// cmdContextList implements "harness context list [--active] [--dir <dir>]
// [--ctx key=value ...]".
//
// It loads the context.sources block from identity.md, evaluates each source
// condition against any provided --ctx values, and prints the resulting source
// status table.
func cmdContextList(args []string) error {
	fs := flag.NewFlagSet("context list", flag.ExitOnError)
	dir := fs.String("dir", ".", "Project directory")
	activeOnly := fs.Bool("active", false, "Show only active sources")
	var ctxPairs []string
	fs.Func("ctx", "Set a context value (key=value); repeatable", func(s string) error {
		ctxPairs = append(ctxPairs, s)
		return nil
	})

	if err := fs.Parse(args); err != nil {
		return err
	}

	// Load policy to get context.sources.
	policy, err := compose.LoadPolicy(*dir)
	if err != nil {
		return fmt.Errorf("load policy: %w", err)
	}

	if len(policy.Context.Sources) == 0 {
		fmt.Println("No context sources configured.")
		fmt.Println()
		fmt.Println("Add a context.sources block to .harness/identity.md to declare sources.")
		return nil
	}

	// Build the source registry.
	reg, err := agentctx.SourcesFromDefs(sourceDefsFromPolicy(policy.Context.Sources))
	if err != nil {
		return fmt.Errorf("build source registry: %w", err)
	}

	// Parse --ctx key=value pairs.
	values := make(map[string]interface{}, len(ctxPairs))
	for _, pair := range ctxPairs {
		idx := strings.IndexByte(pair, '=')
		if idx < 0 {
			return fmt.Errorf("--ctx value must be key=value, got %q", pair)
		}
		values[pair[:idx]] = pair[idx+1:]
	}

	// Evaluate all sources.
	loader := agentctx.FileLoader(*dir)
	_ = reg.Evaluate(values, *dir, loader, 1)

	// Print results.
	printSourceList(reg, *activeOnly)
	return nil
}

// sourceDefsFromPolicy converts compose.ContextSourceDef slices to
// agentctx.ContextSourceDef slices. Both types carry the same fields;
// the conversion keeps the packages decoupled.
func sourceDefsFromPolicy(in []compose.ContextSourceDef) []agentctx.ContextSourceDef {
	out := make([]agentctx.ContextSourceDef, len(in))
	for i, d := range in {
		out[i] = agentctx.ContextSourceDef{
			Name:     d.Name,
			Type:     d.Type,
			Path:     d.Path,
			When:     d.When,
			Trigger:  d.Trigger,
			Priority: d.Priority,
			Scope:    d.Scope,
			TTL:      d.TTL,
		}
	}
	return out
}

func printSourceList(reg *agentctx.SourceRegistry, activeOnly bool) {
	all := reg.All()

	activeEntries := make([]*agentctx.SourceEntry, 0, len(all))
	inactiveEntries := make([]*agentctx.SourceEntry, 0, len(all))

	for _, e := range all {
		if e.Active {
			activeEntries = append(activeEntries, e)
		} else {
			inactiveEntries = append(inactiveEntries, e)
		}
	}

	if len(activeEntries) == 0 && (activeOnly || len(inactiveEntries) == 0) {
		fmt.Println("No active context sources.")
		return
	}

	fmt.Printf("Context Sources  (%d total, %d active)\n", reg.Count(), len(activeEntries))
	fmt.Println(strings.Repeat("─", 60))

	if len(activeEntries) > 0 {
		fmt.Println()
		fmt.Println("ACTIVE:")
		for _, e := range activeEntries {
			kind := string(e.Source.Kind)
			if kind == "" {
				kind = "file"
			}
			fmt.Printf("  ✓  %-22s  [%s] %s\n", e.Source.Name, kind, e.Source.Path)
			fmt.Printf("     %s\n", e.Reason)
		}
	}

	if !activeOnly && len(inactiveEntries) > 0 {
		fmt.Println()
		fmt.Println("INACTIVE:")
		for _, e := range inactiveEntries {
			kind := string(e.Source.Kind)
			if kind == "" {
				kind = "file"
			}
			fmt.Printf("  ✗  %-22s  [%s] %s\n", e.Source.Name, kind, e.Source.Path)
			fmt.Printf("     %s\n", e.Reason)
		}
	}

	fmt.Println()
}
