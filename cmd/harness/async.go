package main

import (
	"flag"
	"fmt"

	"github.com/htekdev/ai-harness/config"
)

// cmdAsync dispatches async subcommands.
func cmdAsync(args []string) error {
	if len(args) == 0 {
		printAsyncUsage()
		return nil
	}

	sub := args[0]
	rest := args[1:]

	switch sub {
	case "list":
		return cmdAsyncList(rest)
	case "help", "--help", "-h":
		printAsyncUsage()
		return nil
	default:
		return fmt.Errorf("unknown async subcommand %q\n\n%s", sub, asyncUsageText)
	}
}

const asyncUsageText = `Usage: harness async <subcommand> [flags]

Subcommands:
  list   Show async configuration and available async.* primitives

Flags:
  -c, --config <path>   Path to harness config (default: harness.md or harness.yaml)
`

func printAsyncUsage() {
	fmt.Print(asyncUsageText)
}

// cmdAsyncList shows the async configuration and a summary of async.* primitives.
func cmdAsyncList(args []string) error {
	fs := flag.NewFlagSet("async list", flag.ExitOnError)
	configPath := fs.String("config", "", "Path to harness config file")
	fs.StringVar(configPath, "c", "", "Path to harness config file (shorthand)")
	if err := fs.Parse(args); err != nil {
		return err
	}

	cfgPath := resolveConfig(*configPath)
	cfg, _, err := config.LoadFull(cfgPath)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	// Async configuration summary.
	maxConcurrent := 64 // executor default
	if cfg.Async != nil && cfg.Async.MaxConcurrent > 0 {
		maxConcurrent = cfg.Async.MaxConcurrent
	}

	fmt.Println("⚡ Async Configuration:")
	fmt.Println()
	fmt.Printf("  max_concurrent: %d\n", maxConcurrent)
	if cfg.Async == nil {
		fmt.Printf("  (default — add 'async:' block to harness config to customize)\n")
	}
	fmt.Println()

	// Show available async primitives.
	fmt.Println("📚 Available parallel.* Starlark Primitives:")
	fmt.Println()
	fmt.Println("  parallel.launch(tool, args, depends_on=[])  → placeholder")
	fmt.Println("    Dispatch a tool call asynchronously. Returns a placeholder ref")
	fmt.Println("    immediately. depends_on=[ref] chains execution after dependencies.")
	fmt.Println()
	fmt.Println("  parallel.wait_all([ref1, ref2, ...])  → list of results")
	fmt.Println("    Block until ALL placeholder refs resolve. Fan-out/fan-in pattern.")
	fmt.Println()
	fmt.Println("  parallel.wait_any([ref1, ref2, ...])  → result")
	fmt.Println("    Block until the FIRST placeholder resolves. Others keep running.")
	fmt.Println()
	fmt.Println("  parallel.race([ref1, ref2, ...])  → result")
	fmt.Println("    Block until the FIRST placeholder resolves; cancel the losers.")
	fmt.Println("    Speculative execution pattern.")
	fmt.Println()
	fmt.Println("  Note: 'parallel' is used instead of 'async' because 'async' is")
	fmt.Println("  a reserved keyword in the Starlark language specification.")
	fmt.Println()
	fmt.Println("Result struct fields: id (string), tool (string), result (string), is_error (bool)")
	fmt.Println()

	// Show async hook events.
	fmt.Println("🪝 Async Hook Events:")
	fmt.Println()
	fmt.Println("  async.launch    — fires when parallel.launch() dispatches a tool call")
	fmt.Println("  async.complete  — fires when a placeholder resolves (complete/error/cancelled)")
	fmt.Println("  async.barrier   — fires at the loop-boundary barrier (start and end)")
	fmt.Println()

	// Show eval patterns.
	fmt.Println("🧪 Eval Patterns (see evals/testdata/):")
	fmt.Println()
	fmt.Println("  51_async_fanout.yaml       — fan-out 5 parallel calls, fan-in")
	fmt.Println("  52_async_dependency.yaml   — A → B → C dependency chain")
	fmt.Println("  53_async_race.yaml         — speculative execution, cancel loser")
	fmt.Println()

	return nil
}
