package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Templates for harness init scaffolding.
var harnessTemplate = `---
model:
  provider: copilot
  name: gpt-4o
  max_tokens: 4096
  temperature: 0.7
  api_key_env: GH_TOKEN

context:
  max_history: 50
  max_tokens: 128000

delegation:
  max_depth: 3
  max_concurrent: 5
---

# {{NAME}} Agent

You are a helpful AI assistant powered by the AI Harness framework.

## Guidelines

- Be concise and helpful
- Use tools proactively
- Delegate complex multi-step tasks to specialized agents
`

var toolTemplate = `---
name: example_tool
description: An example tool — replace with your own
parameters:
  input:
    type: string
    description: The input to process
    required: true
---

` + "```starlark" + `
result = "Processed: " + args["input"]
` + "```" + `
`

var hookTemplate = `---
handler: example_hook
event: on_turn_start
priority: 100
---

` + "```starlark" + `
log("Turn started")
` + "```" + `
`

func cmdInit(args []string) error {
	fs := flag.NewFlagSet("init", flag.ExitOnError)
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, `Usage: harness init [name]

Scaffold a new harness project with the standard directory structure.

Arguments:
  name    Project name (default: current directory name)

Creates:
  harness.md              Main harness configuration
  .harness/tools/         Tool definitions
  .harness/hooks/         Hook definitions
  .harness/agents/        Agent definitions

`)
	}
	fs.Parse(args)

	name := fs.Arg(0)
	if name == "" {
		// Use current directory name
		cwd, err := os.Getwd()
		if err != nil {
			return err
		}
		name = filepath.Base(cwd)
	}

	// Check if harness.md already exists
	if _, err := os.Stat("harness.md"); err == nil {
		return fmt.Errorf("harness.md already exists — aborting to avoid overwriting")
	}
	if _, err := os.Stat("harness.yaml"); err == nil {
		return fmt.Errorf("harness.yaml already exists — aborting to avoid overwriting")
	}

	// Create directory structure
	dirs := []string{
		".harness/tools",
		".harness/hooks",
		".harness/agents",
	}
	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("creating %s: %w", dir, err)
		}
	}

	// Write harness.md
	content := strings.Replace(harnessTemplate, "{{NAME}}", name, 1)
	if err := os.WriteFile("harness.md", []byte(content), 0644); err != nil {
		return fmt.Errorf("writing harness.md: %w", err)
	}

	// Write example tool
	if err := os.WriteFile(".harness/tools/example.md", []byte(toolTemplate), 0644); err != nil {
		return fmt.Errorf("writing example tool: %w", err)
	}

	// Write example hook
	if err := os.WriteFile(".harness/hooks/example.md", []byte(hookTemplate), 0644); err != nil {
		return fmt.Errorf("writing example hook: %w", err)
	}

	fmt.Printf("✅ Initialized harness project: %s\n\n", name)
	fmt.Println("Created:")
	fmt.Println("  harness.md              Main configuration")
	fmt.Println("  .harness/tools/         Tool definitions")
	fmt.Println("  .harness/hooks/         Hook definitions")
	fmt.Println("  .harness/agents/        Agent definitions")
	fmt.Println()
	fmt.Println("Next steps:")
	fmt.Println("  1. Edit harness.md to configure your model and system prompt")
	fmt.Println("  2. Add tools in .harness/tools/*.md")
	fmt.Println("  3. Run: harness validate")
	fmt.Println("  4. Run: harness run")

	return nil
}
