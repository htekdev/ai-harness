// Command harness is the CLI entry point for AI Harness — a minimal,
// composable agent harness defined as code.
//
// Usage:
//
//	harness <command> [flags]
//
// Golden path (install → scaffold → init → develop → validate → deploy → inspect):
//
//	scaffold   Create a new harness project in a new directory
//	init       Initialize a harness in the current directory
//	validate   Validate harness configuration (supports --dry-run via deploy)
//	run        Start an interactive harness session
//	deploy     Run the harness non-interactively (CI/CD, single prompt in/out)
//	inspect    Snapshot of harness state: tools, hooks, agents, artifacts
//
// Develop commands:
//
//	tools      List registered tools
//	hooks      List registered hooks
//	agents     List configured agents
//	artifacts  List typed artifacts in the registry
//	context    Show context window observability (what's active and why)
//
// Other:
//
//	version    Print version information
package main

import (
	"fmt"
	"os"

	"github.com/htekdev/ai-harness/harness"
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

	// Strip global logging flags from os.Args before subcommand dispatch so
	// every subcommand (run/serve/deploy/...) gets the same knob without
	// having to wire it into its own flag set.
	args, logFormat, logLevel, err := extractLogFlags(os.Args[1:])
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(2)
	}
	args, otelEndpoint, otelSample, otelService, err := extractOtelFlags(args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(2)
	}
	if err := harness.ConfigureLoggerFromFlags(logFormat, logLevel); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(2)
	}
	if err := harness.ConfigureTracerFromFlags(otelEndpoint, otelSample, otelService); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(2)
	}
	defer func() {
		// Best-effort flush; never block exit longer than ~5s.
		ctx, cancel := contextWithTimeout5s()
		defer cancel()
		_ = harness.ShutdownTracer(ctx)
	}()
	if len(args) == 0 {
		printUsage()
		os.Exit(1)
	}

	cmd := args[0]
	args = args[1:]

	switch cmd {
	case "scaffold":
		if err := cmdScaffold(args); err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
	case "run":
		if err := cmdRun(args); err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
	case "serve":
		if err := cmdServe(args); err != nil {
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
	case "deploy":
		if err := cmdDeploy(args); err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
	case "inspect":
		if err := cmdInspect(args); err != nil {
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

Golden path  (install → scaffold → init → develop → validate → deploy → inspect):
  scaffold   Create a new harness project in a new directory
  init       Initialize a harness in the current directory
  validate   Validate harness configuration
  run        Start an interactive harness session (stdin REPL)
  serve      Multi-source session: stdin + telegram + future input sources
  deploy     Run the harness non-interactively (CI/CD, single prompt in/out)
  inspect    Snapshot of runtime state: tools, hooks, agents, artifacts

Develop:
  tools      List registered tools
  hooks      List registered hooks
  agents     List configured agents
  artifacts  List typed artifacts in the registry
  context    Show context window observability snapshot

Other:
  version    Print version information

Flags:
  -c, --config <path>          Path to harness config (default: harness.md or harness.yaml)
  --log-level <level>          debug | info (default) | warn | error
                               (env: HARNESS_LOG_LEVEL)
  --log-format <format>        text (default) | json
                               (env: HARNESS_LOG_FORMAT)
  --otel-endpoint <url>        OTLP/HTTP traces endpoint (e.g. http://localhost:4318)
                               (env: HARNESS_OTEL_ENDPOINT; unset = tracing disabled)
  --otel-sample <ratio>        Trace sample ratio in [0,1] (default 1.0)
                               (env: HARNESS_OTEL_SAMPLE_RATIO)
  --otel-service <name>        service.name resource attr (default ai-harness)
                               (env: HARNESS_OTEL_SERVICE_NAME)

Examples:
  harness scaffold my-agent            # create project
  harness init                         # initialize in-place
  harness validate                     # verify configuration
  harness run                          # interactive session
  harness deploy --input "say hello"   # single-shot run
  harness deploy --dry-run             # validate without LLM call
  harness inspect                      # state snapshot
  harness inspect --verbose            # detailed snapshot
  harness context --verbose            # context window breakdown

Learn more: https://github.com/htekdev/ai-harness
`)
}
