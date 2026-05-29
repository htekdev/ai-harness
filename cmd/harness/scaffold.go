package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// starterHarnessMD is the default harness.md written by `harness scaffold`.
const starterHarnessMD = `---
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

## Core Capabilities

You have access to file system tools and safety hooks.
Extend your harness by adding artifacts in .harness/:

- .harness/tools/     — define new tools
- .harness/hooks/     — add lifecycle hooks
- .harness/agents/    — configure sub-agents

## Guidelines

- Be concise and helpful
- Use tools proactively
- Validate changes with: harness validate
- Inspect registered components with: harness artifacts
`

// starterDefaultTools is the default tool definition written by `harness scaffold`.
const starterDefaultTools = `---
parameters:
  path:
    type: string
    required: true
    description: "Path to the file to read"
timeout_ms: 5000
script: |
  def run(args):
      path = args.get("path", "")
      if not path:
          return {"error": "Path is required"}
      if not fs.exists(path):
          return {"error": "File not found: " + path}
      content = fs.read(path)
      return {
          "success": True,
          "path": path,
          "content": content
      }
---

Read the contents of a file at the specified path.
`

// starterWriteFileTool is the write_file tool definition written by `harness scaffold`.
const starterWriteFileTool = `---
parameters:
  path:
    type: string
    required: true
    description: "Path to the file to write"
  content:
    type: string
    required: true
    description: "Content to write to the file"
timeout_ms: 5000
script: |
  def run(args):
      path = args.get("path", "")
      content = args.get("content", "")
      if not path:
          return {"error": "Path is required"}
      fs.write(path, content)
      return {
          "success": True,
          "path": path,
          "bytes_written": len(content)
      }
---

Write content to a file at the specified path. Creates or overwrites the file.
`

// starterDefaultHook is the default safety hook written by `harness scaffold`.
const starterDefaultHook = `---
event: tool.pre
priority: 100
when: "tool.name == 'exec' and ('rm -rf' in tool.args.command or 'dd if=' in tool.args.command or 'mkfs' in tool.args.command)"
script: |
  def run(event, payload):
      command = payload.get("tool", {}).get("args", {}).get("command", "")
      log("BLOCKED: Dangerous command detected: " + command)
      return {
          "block": True,
          "reason": "Command contains potentially dangerous operations. Please review before executing."
      }
---

Safety hook that blocks potentially dangerous shell commands before execution.
`

func cmdScaffold(args []string) error {
	fs := flag.NewFlagSet("scaffold", flag.ExitOnError)
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, `Usage: harness scaffold <name>

Create a new harness project in a new directory.

Arguments:
  name    Project name and directory to create

Creates:
  <name>/harness.md                        Main harness configuration
  <name>/.harness/tools/read_file.md       Starter tool: read_file
  <name>/.harness/tools/write_file.md      Starter tool: write_file
  <name>/.harness/hooks/safety.md          Starter safety hook
  <name>/.harness/agents/                  Agent definitions (empty)

Golden path:
  harness scaffold <name>   ← you are here
  harness init              — generate a self-defining harness
  harness validate          — verify configuration
  harness run               — start interactive session
  harness deploy            — non-interactive / CI run
  harness inspect           — snapshot of runtime state

`)
	}
	fs.Parse(args)

	name := fs.Arg(0)
	if name == "" {
		fs.Usage()
		return fmt.Errorf("name is required")
	}

	// Refuse to overwrite an existing directory
	if _, err := os.Stat(name); err == nil {
		return fmt.Errorf("directory %q already exists — use 'harness init' to initialize in an existing directory", name)
	}

	// Create project directory and .harness subdirectories
	for _, sub := range []string{
		filepath.Join(name, ".harness", "tools"),
		filepath.Join(name, ".harness", "hooks"),
		filepath.Join(name, ".harness", "agents"),
	} {
		if err := os.MkdirAll(sub, 0755); err != nil {
			return fmt.Errorf("creating directory %s: %w", sub, err)
		}
	}

	// Write harness.md
	harnessContent := strings.ReplaceAll(starterHarnessMD, "{{NAME}}", toTitleCase(name))
	if err := os.WriteFile(filepath.Join(name, "harness.md"), []byte(harnessContent), 0644); err != nil {
		return fmt.Errorf("writing harness.md: %w", err)
	}

	// Write starter tools
	if err := os.WriteFile(filepath.Join(name, ".harness", "tools", "read_file.md"), []byte(starterDefaultTools), 0644); err != nil {
		return fmt.Errorf("writing read_file tool: %w", err)
	}
	if err := os.WriteFile(filepath.Join(name, ".harness", "tools", "write_file.md"), []byte(starterWriteFileTool), 0644); err != nil {
		return fmt.Errorf("writing write_file tool: %w", err)
	}

	// Write starter hook
	if err := os.WriteFile(filepath.Join(name, ".harness", "hooks", "safety.md"), []byte(starterDefaultHook), 0644); err != nil {
		return fmt.Errorf("writing safety hook: %w", err)
	}

	fmt.Printf("✅ Scaffolded harness project: %s/\n\n", name)
	fmt.Println("Created:")
	fmt.Printf("  %s/harness.md\n", name)
	fmt.Printf("  %s/.harness/tools/read_file.md\n", name)
	fmt.Printf("  %s/.harness/tools/write_file.md\n", name)
	fmt.Printf("  %s/.harness/hooks/safety.md\n", name)
	fmt.Printf("  %s/.harness/agents/\n", name)
	fmt.Println()
	fmt.Println("Next steps:")
	fmt.Printf("  cd %s\n", name)
	fmt.Printf("  harness validate           # verify configuration\n")
	fmt.Printf("  harness artifacts          # inspect registered artifacts\n")
	fmt.Printf("  harness run                # start interactive session\n")
	fmt.Printf("  harness deploy --dry-run   # simulate a deploy (no LLM call)\n")

	return nil
}

// toTitleCase converts a name like "my-project" or "my_project" to "My Project".
func toTitleCase(s string) string {
	s = strings.ReplaceAll(s, "-", " ")
	s = strings.ReplaceAll(s, "_", " ")
	words := strings.Fields(s)
	for i, w := range words {
		if len(w) > 0 {
			words[i] = strings.ToUpper(w[:1]) + w[1:]
		}
	}
	return strings.Join(words, " ")
}
