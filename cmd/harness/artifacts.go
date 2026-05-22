package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/htekdev/ai-harness/artifact"
)

func cmdArtifacts(args []string) error {
	fs := flag.NewFlagSet("artifacts", flag.ExitOnError)
	verbose := fs.Bool("verbose", false, "Show detailed artifact information")
	fs.BoolVar(verbose, "v", false, "Show detailed artifact information")
	dir := fs.String("dir", ".", "Project directory to scan")
	typeFilter := fs.String("type", "", "Filter by artifact type (override, harness, builtin, plugin, model)")

	if err := fs.Parse(args); err != nil {
		return err
	}

	baseDir := filepath.Join(*dir, ".harness")
	reg, err := artifact.LoadAndRegister(baseDir)
	if err != nil {
		// If no artifacts directory exists, show helpful message
		if os.IsNotExist(err) || reg == nil {
			fmt.Println("No artifacts found.")
			fmt.Println()
			fmt.Println("To get started, create artifact files in .harness/ with typed frontmatter:")
			fmt.Println("  .harness/identity.md       (type: harness)")
			fmt.Println("  .harness/builtins/*.md     (type: builtin)")
			fmt.Println("  .harness/plugins/*.md      (type: plugin)")
			fmt.Println("  .harness/models/*.md       (type: model)")
			fmt.Println("  .harness/overrides/*.md    (type: override)")
			return nil
		}
		return fmt.Errorf("load artifacts: %w", err)
	}

	var artifacts []*artifact.Artifact
	if *typeFilter != "" {
		t, err := artifact.ParseType(*typeFilter)
		if err != nil {
			return err
		}
		artifacts = reg.ByType(t)
	} else {
		artifacts = reg.All()
	}

	if len(artifacts) == 0 {
		if *typeFilter != "" {
			fmt.Printf("No artifacts of type %q found.\n", *typeFilter)
		} else {
			fmt.Println("No artifacts found.")
		}
		return nil
	}

	fmt.Printf("Artifacts: %d registered\n", reg.Count())
	fmt.Println(strings.Repeat("─", 60))

	for _, a := range artifacts {
		version := ""
		if a.Metadata.Version != "" {
			version = " v" + a.Metadata.Version
		}

		fmt.Printf("  [%s] %s%s\n", a.Metadata.Type, a.Metadata.Name, version)

		if *verbose {
			if a.Metadata.Description != "" {
				fmt.Printf("         %s\n", a.Metadata.Description)
			}
			if a.Source != "" {
				fmt.Printf("         source: %s\n", a.Source)
			}
			if len(a.Tools) > 0 {
				fmt.Printf("         tools: %s\n", toolNames(a.Tools))
			}
			if len(a.Hooks) > 0 {
				fmt.Printf("         hooks: %d\n", len(a.Hooks))
			}
			if len(a.Models) > 0 {
				fmt.Printf("         models: %s\n", modelNames(a.Models))
			}
			if len(a.Metadata.Tags) > 0 {
				fmt.Printf("         tags: %s\n", strings.Join(a.Metadata.Tags, ", "))
			}
			if len(a.Metadata.DependsOn) > 0 {
				fmt.Printf("         depends_on: %s\n", strings.Join(a.Metadata.DependsOn, ", "))
			}
			if a.Condition != "" {
				fmt.Printf("         condition: %s\n", a.Condition)
			}
			fmt.Printf("         priority: %d\n", a.EffectivePriority())
			fmt.Println()
		}
	}

	if !*verbose {
		fmt.Println()
		fmt.Println("Use --verbose for detailed information.")
	}

	return nil
}

func toolNames(tools []artifact.ToolDef) string {
	names := make([]string, 0, len(tools))
	for _, t := range tools {
		names = append(names, t.Name)
	}
	return strings.Join(names, ", ")
}

func modelNames(models []artifact.ModelDef) string {
	names := make([]string, 0, len(models))
	for _, m := range models {
		names = append(names, m.Name)
	}
	return strings.Join(names, ", ")
}
