package main

// Phase 4 end-to-end runServe eval coverage.
//
// These tests exercise the multi-source select loop in runServe directly,
// using stub input.Source / input.Replier implementations and an injected
// turnRunner. They cover behaviours that per-source unit tests cannot reach:
//
//   - Multi-source coexistence: two sources feeding events into one harness.
//   - Replier routing: replies land back on the originating source instance,
//     not on a sibling source that happens to share a SessionKey.
//   - Per-SessionKey serialization: two messages from the same SessionKey
//     are processed in order, never interleaved.
//   - Different SessionKeys run concurrently (do not block each other).
//   - Source EOF: when one source exhausts, runServe keeps draining the rest
//     and only exits once every source is done.
//   - Context cancel: runServe returns promptly on ctx cancel.
//   - Non-Replier source (stdin shape): no panic on result delivery.
//   - Run errors are routed back to Replier as "error: ..." messages.

import (
	"context"
	"fmt"
	"io"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/htekdev/ai-harness/agent"
	"github.com/htekdev/ai-harness/input"
)

// fakeSource emits a fixed slice of Events then returns io.EOF. Each fakeSource
// tracks the replies routed back to it so tests can assert per-source delivery.
type fakeSource struct {
	name    string
	events  []input.Event
	cursor  int
	mu      sync.Mutex
	replies []replyRecord
	// gate, if non-nil, blocks Read until each successive value is closed.
	// gate[i] gates the i-th Read call (gate[0] for the first Read, etc.).
	// Useful for forcing concurrent ordering in tests.
	gate []chan struct{}
}

type replyRecord struct {
	sessionKey string
	text       string
}

func (f *fakeSource) Name() string { return f.name }

func (f *fakeSource) Read(ctx context.Context) (input.Event, error) {
	if f.cursor < len(f.gate) {
		select {
		case <-f.gate[f.cursor]:
		case <-ctx.Done():
			return input.Event{}, ctx.Err()
		}
	}
	if f.cursor >= len(f.events) {
		return input.Event{}, io.EOF
	}
	ev := f.events[f.cursor]
	f.cursor++
	return ev, nil
}

func (f *fakeSource) Close() error { return nil }

func (f *fakeSource) Reply(ctx context.Context, ev input.Event, text string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.replies = append(f.replies, replyRecord{sessionKey: ev.SessionKey, text: text})
	return nil
}

func (f *fakeSource) seenReplies() []replyRecord {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]replyRecord, len(f.replies))
	copy(out, f.replies)
	return out
}

// nonReplierSource is a Source that does NOT implement Replier — equivalent
// to the stdin source from runServe's perspective. We use it to verify that
// runServe handles missing Replier without panicking.
type nonReplierSource struct {
	name   string
	events []input.Event
	cursor int
}

func (n *nonReplierSource) Name() string { return n.name }
func (n *nonReplierSource) Read(ctx context.Context) (input.Event, error) {
	if n.cursor >= len(n.events) {
		return input.Event{}, io.EOF
	}
	ev := n.events[n.cursor]
	n.cursor++
	return ev, nil
}
func (n *nonReplierSource) Close() error { return nil }

// TestRunServe_MultiSourceDispatch wires two fake sources into runServe and
// verifies that:
//   - Every Event reaches the injected runner exactly once.
//   - Each reply is routed to the originating source (not the sibling).
//   - runServe exits cleanly once both sources return io.EOF.
func TestRunServe_MultiSourceDispatch(t *testing.T) {
	srcA := &fakeSource{
		name: "telegram",
		events: []input.Event{
			{SourceName: "telegram", SessionKey: "chat-A", Text: "hi from A"},
		},
	}
	srcB := &fakeSource{
		name: "meshwire",
		events: []input.Event{
			{SourceName: "meshwire", SessionKey: "peer-B", Text: "hi from B"},
		},
	}

	var runs int32
	run := func(ctx context.Context, text string) (*agent.TurnResult, error) {
		atomic.AddInt32(&runs, 1)
		return &agent.TurnResult{Response: "echo: " + text}, nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	if err := runServe(ctx, run, []input.Source{srcA, srcB}); err != nil {
		t.Fatalf("runServe: %v", err)
	}

	if got := atomic.LoadInt32(&runs); got != 2 {
		t.Errorf("run invocations = %d, want 2", got)
	}

	repA := srcA.seenReplies()
	if len(repA) != 1 || repA[0].text != "echo: hi from A" || repA[0].sessionKey != "chat-A" {
		t.Errorf("srcA replies = %+v, want one echo for chat-A", repA)
	}
	repB := srcB.seenReplies()
	if len(repB) != 1 || repB[0].text != "echo: hi from B" || repB[0].sessionKey != "peer-B" {
		t.Errorf("srcB replies = %+v, want one echo for peer-B", repB)
	}
}

// TestRunServe_PerSessionSerialization verifies that two events sharing a
// SessionKey are processed in order: the second Run does not begin until the
// first Run returns. This is the contract sessionWorker promises so concurrent
// turns from the same chat cannot interleave Run() against a non-concurrent
// harness.
func TestRunServe_PerSessionSerialization(t *testing.T) {
	src := &fakeSource{
		name: "telegram",
		events: []input.Event{
			{SourceName: "telegram", SessionKey: "chat-1", Text: "first"},
			{SourceName: "telegram", SessionKey: "chat-1", Text: "second"},
		},
	}

	var (
		mu      sync.Mutex
		active  int
		maxSeen int
		order   []string
	)

	run := func(ctx context.Context, text string) (*agent.TurnResult, error) {
		mu.Lock()
		active++
		if active > maxSeen {
			maxSeen = active
		}
		order = append(order, "start:"+text)
		mu.Unlock()

		time.Sleep(50 * time.Millisecond)

		mu.Lock()
		active--
		order = append(order, "end:"+text)
		mu.Unlock()
		return &agent.TurnResult{Response: "ok"}, nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	if err := runServe(ctx, run, []input.Source{src}); err != nil {
		t.Fatalf("runServe: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if maxSeen != 1 {
		t.Errorf("max concurrent runs for same SessionKey = %d, want 1 (serialized)", maxSeen)
	}
	want := []string{"start:first", "end:first", "start:second", "end:second"}
	if len(order) != len(want) {
		t.Fatalf("order = %v, want %v", order, want)
	}
	for i := range want {
		if order[i] != want[i] {
			t.Errorf("order[%d] = %q, want %q", i, order[i], want[i])
		}
	}
}

// TestRunServe_DifferentSessionsRunConcurrently verifies the inverse of the
// serialization test: two events with *different* SessionKeys may execute
// concurrently. Without this, a slow turn from one chat would head-of-line
// block every other chat connected to the same serve process.
func TestRunServe_DifferentSessionsRunConcurrently(t *testing.T) {
	src := &fakeSource{
		name: "telegram",
		events: []input.Event{
			{SourceName: "telegram", SessionKey: "chat-A", Text: "msg A"},
			{SourceName: "telegram", SessionKey: "chat-B", Text: "msg B"},
		},
	}

	started := make(chan string, 2)
	release := make(chan struct{})

	run := func(ctx context.Context, text string) (*agent.TurnResult, error) {
		started <- text
		<-release
		return &agent.TurnResult{Response: "ok"}, nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	done := make(chan error, 1)
	go func() { done <- runServe(ctx, run, []input.Source{src}) }()

	// Both Run calls must start before either is allowed to finish.
	seen := map[string]bool{}
	for i := 0; i < 2; i++ {
		select {
		case s := <-started:
			seen[s] = true
		case <-time.After(2 * time.Second):
			t.Fatalf("only %d Run calls started; expected concurrent start for different SessionKeys", i)
		}
	}
	if !seen["msg A"] || !seen["msg B"] {
		t.Errorf("expected both msgs to start; got %v", seen)
	}
	close(release)

	if err := <-done; err != nil {
		t.Fatalf("runServe: %v", err)
	}
}

// TestRunServe_ExitsWhenAllSourcesEOF verifies that runServe drops out of the
// loop once every source has returned io.EOF, even without ctx cancel. This
// is the piped-stdin "process exits when input closes" contract.
func TestRunServe_ExitsWhenAllSourcesEOF(t *testing.T) {
	src := &nonReplierSource{
		name: "stdin",
		events: []input.Event{
			{SourceName: "stdin", SessionKey: "", Text: "one and done"},
		},
	}
	run := func(ctx context.Context, text string) (*agent.TurnResult, error) {
		return &agent.TurnResult{Response: "ok"}, nil
	}

	done := make(chan error, 1)
	go func() { done <- runServe(context.Background(), run, []input.Source{src}) }()

	select {
	case err := <-done:
		if err != nil {
			t.Errorf("runServe returned %v, want nil", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("runServe did not exit after all sources EOF")
	}
}

// TestRunServe_ContextCancelExits verifies that runServe returns promptly
// when its context is cancelled, even if sources are still blocked in Read.
func TestRunServe_ContextCancelExits(t *testing.T) {
	src := &fakeSource{
		name: "telegram",
		// One never-arriving event: gate[0] is never closed.
		events: []input.Event{{SourceName: "telegram", SessionKey: "x", Text: "blocked"}},
		gate:   []chan struct{}{make(chan struct{})},
	}
	run := func(ctx context.Context, text string) (*agent.TurnResult, error) {
		return &agent.TurnResult{Response: "ok"}, nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- runServe(ctx, run, []input.Source{src}) }()

	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Errorf("runServe returned %v, want nil", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("runServe did not return after ctx cancel")
	}
}

// TestRunServe_RunErrorRoutedToReplier verifies that when the harness Run
// returns an error, the Replier gets an "error: ..." message back so the
// originating chat sees something concrete instead of silent failure.
func TestRunServe_RunErrorRoutedToReplier(t *testing.T) {
	src := &fakeSource{
		name: "telegram",
		events: []input.Event{
			{SourceName: "telegram", SessionKey: "chat-1", Text: "trigger"},
		},
	}
	run := func(ctx context.Context, text string) (*agent.TurnResult, error) {
		return nil, fmt.Errorf("boom")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := runServe(ctx, run, []input.Source{src}); err != nil {
		t.Fatalf("runServe: %v", err)
	}
	reps := src.seenReplies()
	if len(reps) != 1 {
		t.Fatalf("reply count = %d, want 1", len(reps))
	}
	if got := reps[0].text; got != "error: boom" {
		t.Errorf("reply text = %q, want %q", got, "error: boom")
	}
}

// TestRunServe_NonReplierSourceNoPanic verifies that a Source which doesn't
// implement Replier (the stdin shape) does not cause runServe to panic when
// it tries to route a result back. handleResult's type assertion must guard.
func TestRunServe_NonReplierSourceNoPanic(t *testing.T) {
	src := &nonReplierSource{
		name: "custom-source",
		events: []input.Event{
			{SourceName: "custom-source", SessionKey: "k", Text: "hello"},
		},
	}
	run := func(ctx context.Context, text string) (*agent.TurnResult, error) {
		return &agent.TurnResult{Response: "ok"}, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := runServe(ctx, run, []input.Source{src}); err != nil {
		t.Fatalf("runServe: %v", err)
	}
	// No panic, no reply attempted — that's the contract.
}
