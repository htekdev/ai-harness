package events

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"
)

func TestFileStoreAppendAndReplayPersistsAcrossReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.jsonl")

	store, err := OpenFileStore(path)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}

	first, err := store.Append(context.Background(), Event{
		Stream: "runtime/demo",
		Type:   "runtime.started",
		Source: "test",
		Data:   json.RawMessage(`{"kind":"watcher"}`),
	})
	if err != nil {
		t.Fatalf("append first: %v", err)
	}
	second, err := store.Append(context.Background(), Event{
		Stream: "runtime/demo",
		Type:   "runtime.heartbeat",
		Source: "test",
	})
	if err != nil {
		t.Fatalf("append second: %v", err)
	}
	if first.Sequence != 1 || second.Sequence != 2 {
		t.Fatalf("unexpected sequences: %d, %d", first.Sequence, second.Sequence)
	}

	reopened, err := OpenFileStore(path)
	if err != nil {
		t.Fatalf("reopen store: %v", err)
	}

	var replayed []Event
	if err := reopened.Replay(context.Background(), "runtime/demo", func(event Event) error {
		replayed = append(replayed, event)
		return nil
	}); err != nil {
		t.Fatalf("replay: %v", err)
	}

	if len(replayed) != 2 {
		t.Fatalf("expected 2 replayed events, got %d", len(replayed))
	}
	if replayed[0].Type != "runtime.started" || replayed[1].Type != "runtime.heartbeat" {
		t.Fatalf("unexpected replay order: %+v", replayed)
	}
	if replayed[0].ID == "" || replayed[0].Time.IsZero() {
		t.Fatalf("expected generated metadata, got %+v", replayed[0])
	}
}

func TestFileStoreSubscribeFiltersByStream(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.jsonl")
	store, err := OpenFileStore(path)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	eventsCh, closeSub := store.Subscribe(ctx, "artifact/doc-1")
	defer closeSub()

	if _, err := store.Append(context.Background(), Event{Stream: "artifact/doc-2", Type: "artifact.updated"}); err != nil {
		t.Fatalf("append other stream: %v", err)
	}
	expected, err := store.Append(context.Background(), Event{Stream: "artifact/doc-1", Type: "artifact.updated"})
	if err != nil {
		t.Fatalf("append expected stream: %v", err)
	}

	select {
	case got := <-eventsCh:
		if got.Stream != expected.Stream || got.Sequence != expected.Sequence {
			t.Fatalf("unexpected event: %+v", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for subscribed event")
	}
}

func TestFileStoreRejectsInvalidEvents(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.jsonl")
	store, err := OpenFileStore(path)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}

	if _, err := store.Append(context.Background(), Event{Type: "runtime.started"}); err == nil {
		t.Fatal("expected missing stream error")
	}
	if _, err := store.Append(context.Background(), Event{Stream: "runtime/demo"}); err == nil {
		t.Fatal("expected missing type error")
	}
}
