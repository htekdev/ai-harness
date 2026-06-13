package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"

	"github.com/htekdev/ai-harness/agent"
	"github.com/htekdev/ai-harness/config"
	"github.com/htekdev/ai-harness/harness"
	"github.com/htekdev/ai-harness/input"
)

// cmdServe runs a harness in non-REPL mode that consumes input from one or more
// configured Sources (stdin, telegram, ...) and routes responses back via Replier
// sources where supported.
//
// This is the Phase 4 entry point: a single serve process can host multiple
// concurrent chat sessions keyed by SessionKey (chat_id) and accept input from
// any registered Source without touching the existing harness run REPL.
//
// v1 flags:
//
//	--config <path>          Path to harness.md / harness.yaml
//	--source stdin           Add the stdin source (REPL-equivalent)
//	--source telegram        Add the Telegram source (requires TELEGRAM_BOT_TOKEN env)
//	--telegram-chat <id>     Repeatable allowlist of chat IDs (REQUIRED for telegram source)
//	--telegram-poll <secs>   Long-poll timeout (default 25, max 50)
//	--source meshwire        Add the MeshWire source (requires MESHWIRE_TOKEN env)
//	--meshwire-mesh <id>     MeshWire mesh ID (REQUIRED for meshwire source)
//	--meshwire-agent <id>    This harness's agent_id in the mesh (REQUIRED for meshwire source)
//	--meshwire-sender <id>   Repeatable allowlist of peer agent IDs (REQUIRED for meshwire source)
//	--meshwire-poll <secs>   MeshWire long-poll timeout (default 30, max 60)
//	--meshwire-base <url>    Override MeshWire API base URL (default https://meshwire.io)
//
// If neither --source flags nor a `serve:` block in harness.md frontmatter is
// present, defaults to stdin (drop-in for harness run). When `serve:` is
// declared in the config, it provides the source set unless overridden by
// --source flags on the command line.
func cmdServe(args []string) error {
	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	configPath := fs.String("config", "", "Path to harness config file")
	fs.StringVar(configPath, "c", "", "Path to harness config file (shorthand)")
	var sources multiFlag
	fs.Var(&sources, "source", "Input source to enable (stdin, telegram, meshwire). Repeatable.")
	var telegramChats multiFlag
	fs.Var(&telegramChats, "telegram-chat", "Allowlisted Telegram chat ID (repeatable). Required when --source telegram.")
	telegramPoll := fs.Int("telegram-poll", 25, "Telegram long-poll timeout in seconds (max 50)")
	meshwireMesh := fs.String("meshwire-mesh", "", "MeshWire mesh ID. Required when --source meshwire.")
	meshwireAgent := fs.String("meshwire-agent", "", "This harness's agent_id in the mesh. Required when --source meshwire.")
	var meshwireSenders multiFlag
	fs.Var(&meshwireSenders, "meshwire-sender", "Allowlisted peer agent_id (repeatable). Required when --source meshwire.")
	meshwirePoll := fs.Int("meshwire-poll", 30, "MeshWire long-poll timeout in seconds (max 60)")
	meshwireBase := fs.String("meshwire-base", "", "Override MeshWire API base URL (default https://meshwire.io)")
	fs.Parse(args)

	cfgPath := resolveConfig(*configPath)
	h, err := harness.New(cfgPath)
	if err != nil {
		return fmt.Errorf("loading harness from %s: %w", cfgPath, err)
	}

	// Re-read the raw config to pick up the `serve:` block (Harness doesn't
	// expose its config struct directly). LoadFull is what Harness used too,
	// so this is consistent and cheap.
	rawCfg, _, err := config.LoadFull(cfgPath)
	if err != nil {
		return fmt.Errorf("loading serve config from %s: %w", cfgPath, err)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	if err := h.RunSession(ctx); err != nil {
		return fmt.Errorf("starting session: %w", err)
	}
	defer h.EndSession(ctx)

	// Resolve sources: CLI flags win > serve: config block > stdin default.
	mwCLI := meshwireCLI{
		Mesh:    *meshwireMesh,
		Agent:   *meshwireAgent,
		Senders: meshwireSenders,
		Poll:    *meshwirePoll,
		Base:    *meshwireBase,
	}
	srcs, sourceLabels, err := resolveSources(sources, telegramChats, *telegramPoll, mwCLI, rawCfg.Serve)
	if err != nil {
		return err
	}
	defer func() {
		for _, s := range srcs {
			_ = s.Close()
		}
	}()

	fmt.Println("🤖 AI Harness — Serve Mode")
	fmt.Printf("   config:  %s\n", cfgPath)
	fmt.Printf("   sources: %s\n", strings.Join(sourceLabels, ", "))
	fmt.Println("   (Ctrl-C to stop)")
	fmt.Println("---")

	return runServe(ctx, h, srcs)
}

// resolveSources picks the source set with this precedence:
//  1. --source CLI flags (telegramChats / telegramPoll apply)
//  2. serve.sources in harness.md frontmatter
//  3. stdin default
//
// It returns the constructed input.Source slice and a parallel label slice
// (e.g. ["stdin", "telegram"]) for human-friendly logging.
// meshwireCLI groups the MeshWire-specific CLI flags so resolveSources keeps a
// stable signature even as new per-source flag sets are added.
type meshwireCLI struct {
	Mesh    string
	Agent   string
	Senders []string
	Poll    int
	Base    string
}

func resolveSources(cliSources, telegramChats []string, telegramPoll int, mw meshwireCLI, serveCfg *config.ServeConfig) ([]input.Source, []string, error) {
	// Path 1: CLI flags present.
	if len(cliSources) > 0 {
		srcs, err := buildSources(cliSources, telegramChats, telegramPoll,
			mw.Mesh, mw.Agent, mw.Senders, mw.Poll, mw.Base)
		if err != nil {
			return nil, nil, err
		}
		return srcs, append([]string(nil), cliSources...), nil
	}

	// Path 2: serve: config block.
	if serveCfg != nil && len(serveCfg.Sources) > 0 {
		srcs, labels, err := buildSourcesFromConfig(serveCfg.Sources)
		if err != nil {
			return nil, nil, err
		}
		return srcs, labels, nil
	}

	// Path 3: stdin default.
	srcs, err := buildSources([]string{"stdin"}, nil, 25,
		"", "", nil, 0, "")
	if err != nil {
		return nil, nil, err
	}
	return srcs, []string{"stdin"}, nil
}

// serveJob couples an Event with the Source that produced it so the per-session
// worker can route replies back to origin via Replier when supported.
type serveJob struct {
	ev     input.Event
	source input.Source
}

// runServe is the multi-source select loop. Exported via package-internal helpers
// so the serve subcommand and future tests can share the routing logic.
//
// Per-SessionKey serialization: each unique SessionKey gets its own goroutine
// that consumes turn requests for that session in order. This prevents two
// concurrent messages from the same chat from interleaving Run() calls on the
// same harness instance (which is not yet documented as concurrent-safe).
func runServe(ctx context.Context, h *harness.Harness, srcs []input.Source) error {
	events := make(chan serveJob, 16)

	// Pump each source on its own goroutine.
	var wg sync.WaitGroup
	for _, s := range srcs {
		wg.Add(1)
		go func(s input.Source) {
			defer wg.Done()
			for {
				ev, err := s.Read(ctx)
				if err != nil {
					if err == io.EOF || ctx.Err() != nil {
						return
					}
					fmt.Fprintf(os.Stderr, "[%s] read error: %v\n", s.Name(), err)
					return
				}
				select {
				case events <- serveJob{ev: ev, source: s}:
				case <-ctx.Done():
					return
				}
			}
		}(s)
	}

	// Per-session worker pool: SessionKey -> chan serveJob.
	workers := map[string]chan serveJob{}
	var mu sync.Mutex

	dispatch := func(j serveJob) {
		key := j.ev.SessionKey
		mu.Lock()
		ch, ok := workers[key]
		if !ok {
			ch = make(chan serveJob, 8)
			workers[key] = ch
			go sessionWorker(ctx, h, key, ch)
		}
		mu.Unlock()
		select {
		case ch <- j:
		case <-ctx.Done():
		}
	}

	// Drain events until ctx is done or all sources have closed.
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()

	for {
		select {
		case <-ctx.Done():
			// Closing source channels happens via the deferred Close in the caller.
			return nil
		case <-done:
			// All sources exhausted (e.g. stdin EOF in piped mode).
			return nil
		case j := <-events:
			dispatch(j)
		}
	}
}

// sessionWorker drains turn jobs for a single SessionKey serially.
func sessionWorker(ctx context.Context, h *harness.Harness, key string, ch <-chan serveJob) {
	for {
		select {
		case <-ctx.Done():
			return
		case j, ok := <-ch:
			if !ok {
				return
			}
			result, err := h.Run(ctx, j.ev.Text)
			handleResult(ctx, j, result, err)
		}
	}
}

// handleResult prints results for stdin and routes via Replier for sources that support it.
func handleResult(ctx context.Context, j serveJob, result *agent.TurnResult, err error) {
	if err != nil {
		fmt.Fprintf(os.Stderr, "[%s] error: %v\n", j.source.Name(), err)
		if r, ok := j.source.(input.Replier); ok {
			_ = r.Reply(ctx, j.ev, fmt.Sprintf("error: %v", err))
		}
		return
	}
	if result == nil {
		return
	}

	// Print to stdout for stdin source (REPL-style feedback).
	if j.source.Name() == "stdin" {
		if len(result.ToolCalls) > 0 {
			fmt.Printf("\n📎 Tool calls: %d\n", len(result.ToolCalls))
			for _, tc := range result.ToolCalls {
				fmt.Printf("   → %s(%s)\n", tc.Name, string(tc.Arguments))
			}
		}
		fmt.Printf("\n%s\n", result.Response)
		fmt.Printf("\n[tokens: %d prompt + %d completion = %d total]\n",
			result.Usage.PromptTokens, result.Usage.CompletionTokens, result.Usage.TotalTokens)
		return
	}

	// Route via Replier for sources that support it (telegram, slack, ...).
	if r, ok := j.source.(input.Replier); ok {
		if err := r.Reply(ctx, j.ev, result.Response); err != nil {
			fmt.Fprintf(os.Stderr, "[%s] reply error: %v\n", j.source.Name(), err)
		}
	}
}

// buildSources constructs the requested input sources from CLI flags.
func buildSources(names []string, telegramChats []string, telegramPoll int,
	meshwireMesh, meshwireAgent string, meshwireSenders []string, meshwirePoll int, meshwireBase string,
) ([]input.Source, error) {
	out := make([]input.Source, 0, len(names))
	for _, n := range names {
		switch strings.ToLower(strings.TrimSpace(n)) {
		case "stdin":
			out = append(out, input.NewStdinSource(os.Stdin, func() { fmt.Print("\n> ") }))
		case "meshwire":
			token := os.Getenv("MESHWIRE_TOKEN")
			if token == "" {
				return nil, fmt.Errorf("meshwire source requires MESHWIRE_TOKEN env var")
			}
			if meshwireMesh == "" {
				return nil, fmt.Errorf("meshwire source requires --meshwire-mesh <id>")
			}
			if meshwireAgent == "" {
				return nil, fmt.Errorf("meshwire source requires --meshwire-agent <id>")
			}
			if len(meshwireSenders) == 0 {
				return nil, fmt.Errorf("meshwire source requires at least one --meshwire-sender <id> (no wildcard in v1)")
			}
			src, err := input.NewMeshWireSource(input.MeshWireConfig{
				Token:              token,
				MeshID:             meshwireMesh,
				AgentID:            meshwireAgent,
				SenderAllowlist:    meshwireSenders,
				PollTimeoutSeconds: meshwirePoll,
				APIBase:            meshwireBase,
			})
			if err != nil {
				return nil, err
			}
			out = append(out, src)
		case "telegram":
			token := os.Getenv("TELEGRAM_BOT_TOKEN")
			if token == "" {
				return nil, fmt.Errorf("telegram source requires TELEGRAM_BOT_TOKEN env var")
			}
			ids := make([]int64, 0, len(telegramChats))
			for _, raw := range telegramChats {
				id, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
				if err != nil {
					return nil, fmt.Errorf("invalid --telegram-chat %q: %w", raw, err)
				}
				ids = append(ids, id)
			}
			src, err := input.NewTelegramSource(input.TelegramConfig{
				Token:              token,
				ChatAllowlist:      ids,
				PollTimeoutSeconds: telegramPoll,
			})
			if err != nil {
				return nil, err
			}
			out = append(out, src)
		default:
			return nil, fmt.Errorf("unknown source %q (supported: stdin, telegram, meshwire)", n)
		}
	}
	return out, nil
}

// buildSourcesFromConfig is the declarative counterpart to buildSources: it
// accepts a parsed `serve.sources` list from harness.md frontmatter and
// constructs the matching input.Source instances. Secrets are read from the
// env var named by each entry's `token_env` — never from the config itself.
//
// Returns the constructed sources alongside a parallel label slice for logging.
func buildSourcesFromConfig(srcs []config.ServeSourceConfig) ([]input.Source, []string, error) {
	out := make([]input.Source, 0, len(srcs))
	labels := make([]string, 0, len(srcs))
	for i, sc := range srcs {
		t := sc.NormalizedType()
		switch t {
		case "stdin":
			out = append(out, input.NewStdinSource(os.Stdin, func() { fmt.Print("\n> ") }))
			labels = append(labels, "stdin")
		case "telegram":
			token := os.Getenv(sc.TokenEnv)
			if token == "" {
				return nil, nil, fmt.Errorf("serve.sources[%d] (telegram): env var %s is empty or unset", i, sc.TokenEnv)
			}
			poll := sc.PollTimeoutSeconds
			if poll == 0 {
				poll = 25
			}
			cfg := input.TelegramConfig{
				Token:              token,
				ChatAllowlist:      append([]int64(nil), sc.ChatAllowlist...),
				PollTimeoutSeconds: poll,
			}
			if sc.OffsetPath != "" {
				cfg.OffsetStore = input.NewFileOffsetStore(sc.OffsetPath)
			}
			src, err := input.NewTelegramSource(cfg)
			if err != nil {
				return nil, nil, fmt.Errorf("serve.sources[%d] (telegram): %w", i, err)
			}
			out = append(out, src)
			labels = append(labels, "telegram")
		case "meshwire":
			token := os.Getenv(sc.TokenEnv)
			if token == "" {
				return nil, nil, fmt.Errorf("serve.sources[%d] (meshwire): env var %s is empty or unset", i, sc.TokenEnv)
			}
			poll := sc.PollTimeoutSeconds
			if poll == 0 {
				poll = 30
			}
			src, err := input.NewMeshWireSource(input.MeshWireConfig{
				Token:              token,
				MeshID:             sc.MeshID,
				AgentID:            sc.AgentID,
				SenderAllowlist:    append([]string(nil), sc.SenderAllowlist...),
				PollTimeoutSeconds: poll,
				APIBase:            sc.BaseURL,
			})
			if err != nil {
				return nil, nil, fmt.Errorf("serve.sources[%d] (meshwire): %w", i, err)
			}
			out = append(out, src)
			labels = append(labels, "meshwire")
		default:
			return nil, nil, fmt.Errorf("serve.sources[%d]: unknown type %q", i, sc.Type)
		}
	}
	return out, labels, nil
}

type multiFlag []string

func (m *multiFlag) String() string     { return strings.Join(*m, ",") }
func (m *multiFlag) Set(v string) error { *m = append(*m, v); return nil }
