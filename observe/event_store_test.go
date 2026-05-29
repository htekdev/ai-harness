package observe

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestStoreAppendReplaySequence(t *testing.T) {
	s := NewStore()

	e1, err := s.Append(Event{Stream: "session", Type: "session.start", Source: "agent/default"})
	if err != nil {
		t.Fatalf("append e1: %v", err)
	}
	e2, err := s.Append(Event{Stream: "session", Type: "tool.post", Source: "agent/default"})
	if err != nil {
		t.Fatalf("append e2: %v", err)
	}

	if e1.Sequence != 1 || e2.Sequence != 2 {
		t.Fatalf("unexpected sequence assignment: %d, %d", e1.Sequence, e2.Sequence)
	}
	if e1.ID == "" || e2.ID == "" {
		t.Fatal("expected generated IDs")
	}

	got := s.Replay("session", 1, 10)
	if len(got) != 2 {
		t.Fatalf("expected 2 events, got %d", len(got))
	}
	if got[0].Sequence != 1 || got[1].Sequence != 2 {
		t.Fatalf("unexpected replay order: %+v", got)
	}
}

func TestStoreReplayFiltersAndLimit(t *testing.T) {
	s := NewStore()
	mustAppend := func(stream string) {
		t.Helper()
		if _, err := s.Append(Event{Stream: stream, Type: "event", Source: "src"}); err != nil {
			t.Fatalf("append: %v", err)
		}
	}
	mustAppend("a")
	mustAppend("b")
	mustAppend("a")
	mustAppend("b")

	allFrom2 := s.Replay("", 2, 0)
	if len(allFrom2) != 3 {
		t.Fatalf("expected 3 events, got %d", len(allFrom2))
	}
	if allFrom2[0].Sequence != 2 || allFrom2[2].Sequence != 4 {
		t.Fatalf("unexpected all-stream replay: %+v", allFrom2)
	}

	aOnly := s.Replay("a", 1, 1)
	if len(aOnly) != 1 || aOnly[0].Stream != "a" || aOnly[0].Sequence != 1 {
		t.Fatalf("unexpected stream replay result: %+v", aOnly)
	}
}

func TestStoreSubscribeAndBackpressureDrop(t *testing.T) {
	s := NewStore()
	ch, unsub := s.Subscribe("session", 1)
	defer unsub()

	if _, err := s.Append(Event{Stream: "session", Type: "e1", Source: "src", Time: time.Unix(1, 0)}); err != nil {
		t.Fatalf("append e1: %v", err)
	}
	if _, err := s.Append(Event{Stream: "session", Type: "e2", Source: "src", Time: time.Unix(2, 0)}); err != nil {
		t.Fatalf("append e2: %v", err)
	}

	first := <-ch
	if first.Type != "e1" {
		t.Fatalf("expected first event, got %+v", first)
	}
	select {
	case dropped := <-ch:
		t.Fatalf("expected second event to drop under backpressure, got %+v", dropped)
	default:
	}
}

func TestStoreSubscribeAllStreams(t *testing.T) {
	s := NewStore()
	ch, unsub := s.Subscribe("", 4)
	defer unsub()

	if _, err := s.Append(Event{Stream: "a", Type: "a1", Source: "src"}); err != nil {
		t.Fatalf("append a: %v", err)
	}
	if _, err := s.Append(Event{Stream: "b", Type: "b1", Source: "src"}); err != nil {
		t.Fatalf("append b: %v", err)
	}

	got1 := <-ch
	got2 := <-ch
	if got1.Stream != "a" || got2.Stream != "b" {
		t.Fatalf("unexpected stream order: %+v %+v", got1, got2)
	}
}

func TestStoreAppendValidation(t *testing.T) {
	s := NewStore()
	if _, err := s.Append(Event{Type: "x", Source: "src"}); err == nil {
		t.Fatal("expected stream validation error")
	}
	if _, err := s.Append(Event{Stream: "s", Source: "src"}); err == nil {
		t.Fatal("expected type validation error")
	}
	if _, err := s.Append(Event{Stream: "s", Type: "x"}); err == nil {
		t.Fatal("expected source validation error")
	}
}

func TestAsJSONL(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	out, err := AsJSONL([]Event{
		{ID: "evt_1", Stream: "session", Type: "x", Source: "src", Time: now, Sequence: 1},
		{ID: "evt_2", Stream: "session", Type: "y", Source: "src", Time: now, Sequence: 2},
	})
	if err != nil {
		t.Fatalf("AsJSONL: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines, got %d", len(lines))
	}
	if !strings.Contains(lines[0], `"id":"evt_1"`) || !strings.Contains(lines[1], `"id":"evt_2"`) {
		t.Fatalf("unexpected JSONL output: %q", out)
	}
}

func TestNewFileStorePersistsAndReloads(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.jsonl")

	store, err := NewFileStore(path)
	if err != nil {
		t.Fatalf("NewFileStore: %v", err)
	}
	if _, err := store.Append(Event{Stream: "session", Type: "session.start", Source: "agent/default"}); err != nil {
		t.Fatalf("append 1: %v", err)
	}
	if _, err := store.Append(Event{Stream: "session", Type: "tool.post", Source: "agent/default"}); err != nil {
		t.Fatalf("append 2: %v", err)
	}

	reloaded, err := NewFileStore(path)
	if err != nil {
		t.Fatalf("reload store: %v", err)
	}
	got := reloaded.Replay("session", 1, 0)
	if len(got) != 2 {
		t.Fatalf("expected 2 events after reload, got %d", len(got))
	}
	if got[0].Sequence != 1 || got[1].Sequence != 2 {
		t.Fatalf("unexpected sequence after reload: %+v", got)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read persisted file: %v", err)
	}
	if lines := strings.Count(strings.TrimSpace(string(raw)), "\n") + 1; lines != 2 {
		t.Fatalf("expected 2 JSONL lines, got %d (%q)", lines, string(raw))
	}
}
