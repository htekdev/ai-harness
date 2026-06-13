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
//
// If no --source flag is given, defaults to stdin (drop-in for harness run).
func cmdServe(args []string) error {
	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	configPath := fs.String("config", "", "Path to harness config file")
	fs.StringVar(configPath, "c", "", "Path to harness config file (shorthand)")
	var sources multiFlag
	fs.Var(&sources, "source", "Input source to enable (stdin, telegram). Repeatable.")
	var telegramChats multiFlag
	fs.Var(&telegramChats, "telegram-chat", "Allowlisted Telegram chat ID (repeatable). Required when --source telegram.")
	telegramPoll := fs.Int("telegram-poll", 25, "Telegram long-poll timeout in seconds (max 50)")
	fs.Parse(args)

	if len(sources) == 0 {
		sources = []string{"stdin"}
	}

	cfgPath := resolveConfig(*configPath)
	h, err := harness.New(cfgPath)
	if err != nil {
		return fmt.Errorf("loading harness from %s: %w", cfgPath, err)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	if err := h.RunSession(ctx); err != nil {
		return fmt.Errorf("starting session: %w", err)
	}
	defer h.EndSession(ctx)

	// Construct sources.
	srcs, err := buildSources(sources, telegramChats, *telegramPoll)
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
	fmt.Printf("   sources: %s\n", strings.Join(sources, ", "))
	fmt.Println("   (Ctrl-C to stop)")
	fmt.Println("---")

	return runServe(ctx, h, srcs)
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
func buildSources(names []string, telegramChats []string, telegramPoll int) ([]input.Source, error) {
	out := make([]input.Source, 0, len(names))
	for _, n := range names {
		switch strings.ToLower(strings.TrimSpace(n)) {
		case "stdin":
			out = append(out, input.NewStdinSource(os.Stdin, func() { fmt.Print("\n> ") }))
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
			return nil, fmt.Errorf("unknown source %q (supported: stdin, telegram)", n)
		}
	}
	return out, nil
}

// multiFlag is a flag.Value that accumulates repeated --flag values.
type multiFlag []string

func (m *multiFlag) String() string     { return strings.Join(*m, ",") }
func (m *multiFlag) Set(v string) error { *m = append(*m, v); return nil }
