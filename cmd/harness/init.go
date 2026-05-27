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

var defaultToolsTemplate = `---
name: default-tools
type: plugin
version: "1.0.0"
description: Default file system tools for working with files and directories
author: AI Harness
tags: [filesystem, core]
---

# Default Tools

Core file system tools that work out of the box.

## Tools

` + "```yaml" + `
- name: read_file
  description: "Read the contents of a file"
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

- name: write_file
  description: "Write content to a file"
  parameters:
    path:
      type: string
      required: true
      description: "Path to the file to write"
    content:
      type: string
      required: true
      description: "Content to write"
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

- name: list_files
  description: "List files in a directory"
  parameters:
    path:
      type: string
      required: false
      description: "Directory path (defaults to current directory)"
  timeout_ms: 5000
  script: |
    def run(args):
        path = args.get("path", ".")
        if not fs.exists(path):
            return {"error": "Directory not found: " + path}
        files = fs.list(path)
        return {
            "success": True,
            "path": path,
            "files": files
        }

- name: get_current_folder
  description: "Get the absolute path of the current working directory"
  timeout_ms: 1000
  script: |
    def run(args):
        result = exec.run("pwd", [])
        if result["exit_code"] != 0:
            return {"error": "Failed to get current directory"}
        cwd = string.trim(result["stdout"])
        ctx.set("current_folder", cwd)
        return {
            "success": True,
            "folder": cwd
        }
` + "```" + `
`

var defaultHooksTemplate = `---
name: default-hooks
type: plugin
version: "1.0.0"
description: Default safety hooks for command and secret protection
author: AI Harness
tags: [security, safety, governance]
---

# Default Hooks

Safety hooks that protect against dangerous operations.

## Hooks

` + "```yaml" + `
- event: tool.pre
  handler: block_dangerous_commands
  priority: 10
  when: 'tool_name == "exec"'
  script: |
    def handle(event, payload):
        args = payload.get("arguments", {})
        cmd = args.get("cmd", "")
        
        # Dangerous patterns
        dangerous = [
            "rm -rf",
            "dd if=",
            "> /dev/",
            "mkfs",
            "format c:",
        ]
        
        for pattern in dangerous:
            if pattern in cmd:
                return block("Refusing dangerous command: " + cmd)
        
        return allow()

- event: tool.post
  handler: detect_secrets
  priority: 5
  script: |
    def handle(event, payload):
        result = payload.get("result", "")
        if not isinstance(result, str):
            result = json.encode(result)
        
        # Secret patterns
        if "-----BEGIN PRIVATE KEY-----" in result:
            return block("Output contains private key")
        if "api_key=" in result or "password=" in result:
            return block("Output may contain secrets")
        
        return allow()
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

	// Write default tools
	if err := os.WriteFile(".harness/tools/default-tools.md", []byte(defaultToolsTemplate), 0644); err != nil {
		return fmt.Errorf("writing default tools: %w", err)
	}

	// Write default hooks
	if err := os.WriteFile(".harness/hooks/default-hooks.md", []byte(defaultHooksTemplate), 0644); err != nil {
		return fmt.Errorf("writing default hooks: %w", err)
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
