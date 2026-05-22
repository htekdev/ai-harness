// Command harness is the CLI entry point for AI Harness — a minimal,
// composable agent harness defined as code.
//
// Usage:
//
//	harness <command> [flags]
//
// Commands:
//
//	run        Start an interactive harness session
//	init       Scaffold a new harness project
//	validate   Validate harness configuration
//	tools      List registered tools
//	hooks      List registered hooks
//	agents     List configured agents
//	artifacts  List typed artifacts in the registry
//	context    Show context window observability (what's active and why)
//	version    Print version information
package main

import (
	"fmt"
	"os"
)

// Set at build time via -ldflags.
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	cmd := os.Args[1]
	args := os.Args[2:]

	switch cmd {
	case "run":
		if err := cmdRun(args); err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
	case "init":
		if err := cmdInit(args); err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
	case "validate":
		if err := cmdValidate(args); err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
	case "tools":
		if err := cmdTools(args); err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
	case "hooks":
		if err := cmdHooks(args); err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
	case "agents":
		if err := cmdAgents(args); err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
	case "artifacts":
		if err := cmdArtifacts(args); err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
	case "context":
		if err := cmdContext(args); err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
	case "version":
		fmt.Printf("harness %s (commit: %s, built: %s)\n", version, commit, date)
	case "help", "--help", "-h":
		printUsage()
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n\n", cmd)
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Fprintf(os.Stderr, `harness — Harness as Code CLI

Usage:
  harness <command> [flags]

Commands:
  run        Start an interactive harness session
  init       Scaffold a new harness project
  validate   Validate harness configuration
  tools      List registered tools
  hooks      List registered hooks
  agents     List configured agents
  artifacts  List typed artifacts in the registry
  context    Show context window observability snapshot
  version    Print version information

Flags:
  -c, --config <path>   Path to harness config (default: harness.md or harness.yaml)

Examples:
  harness init my-agent
  harness run
  harness validate
  harness context --verbose
  harness tools
  harness hooks --verbose

Learn more: https://github.com/htekdev/ai-harness
`)
}
