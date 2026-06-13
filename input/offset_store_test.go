package input

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestMemoryOffsetStore_LoadSave(t *testing.T) {
	s := NewMemoryOffsetStore()
	got, err := s.Load()
	if err != nil {
		t.Fatalf("initial Load: %v", err)
	}
	if got != 0 {
		t.Errorf("initial offset = %d, want 0", got)
	}
	if err := s.Save(42); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err = s.Load()
	if err != nil {
		t.Fatalf("Load after Save: %v", err)
	}
	if got != 42 {
		t.Errorf("offset = %d, want 42", got)
	}
}

func TestFileOffsetStore_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nested", "offset.json")
	store := NewFileOffsetStore(path)

	// Missing file => 0, no error.
	got, err := store.Load()
	if err != nil {
		t.Fatalf("Load missing: %v", err)
	}
	if got != 0 {
		t.Errorf("missing offset = %d, want 0", got)
	}

	if err := store.Save(123); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// Re-load from a fresh instance to prove durability.
	got, err = NewFileOffsetStore(path).Load()
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if got != 123 {
		t.Errorf("reloaded offset = %d, want 123", got)
	}

	// Overwrite with a higher value, confirm atomic replace.
	if err := store.Save(456); err != nil {
		t.Fatalf("Save 2: %v", err)
	}
	got, err = NewFileOffsetStore(path).Load()
	if err != nil {
		t.Fatalf("reload 2: %v", err)
	}
	if got != 456 {
		t.Errorf("reloaded offset = %d, want 456", got)
	}
}

func TestFileOffsetStore_AcceptsPlainInteger(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "offset.txt")
	if err := os.WriteFile(path, []byte("789\n"), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	got, err := NewFileOffsetStore(path).Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got != 789 {
		t.Errorf("offset = %d, want 789", got)
	}
}

func TestFileOffsetStore_RejectsGarbage(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "offset.txt")
	if err := os.WriteFile(path, []byte("not a number"), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if _, err := NewFileOffsetStore(path).Load(); err == nil {
		t.Errorf("expected error for garbage payload")
	}
}

// TestTelegramSource_RestoresOffsetFromStore proves the bot does not
// re-process previously acked updates after a restart.
func TestTelegramSource_RestoresOffsetFromStore(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "offset.json")
	store := NewFileOffsetStore(path)
	if err := store.Save(50); err != nil {
		t.Fatalf("seed Save: %v", err)
	}

	var capturedOffset string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedOffset = r.URL.Query().Get("offset")
		_ = json.NewEncoder(w).Encode(tgUpdatesResponse{
			OK: true,
			Result: []tgUpdate{
				{UpdateID: 60, Message: &tgMessage{MessageID: 1, From: tgUser{ID: 1, Username: "h"}, Chat: tgChat{ID: 7}, Text: "hi"}},
			},
		})
	}))
	defer srv.Close()

	src, err := NewTelegramSource(TelegramConfig{
		Token:         "fake",
		ChatAllowlist: []int64{7},
		APIBase:       srv.URL,
		OffsetStore:   NewFileOffsetStore(path),
	})
	if err != nil {
		t.Fatalf("NewTelegramSource: %v", err)
	}
	if src.offset != 50 {
		t.Errorf("offset after construct = %d, want 50", src.offset)
	}
	if _, err := src.Read(context.Background()); err != nil {
		t.Fatalf("Read: %v", err)
	}
	if capturedOffset != "50" {
		t.Errorf("getUpdates offset = %q, want 50", capturedOffset)
	}
	got, err := NewFileOffsetStore(path).Load()
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if got != 61 {
		t.Errorf("persisted offset = %d, want 61", got)
	}
}

// TestTelegramSource_PersistsAfterEachAdvance ensures every Read advance
// hits the OffsetStore so a mid-batch crash doesn't lose progress.
func TestTelegramSource_PersistsAfterEachAdvance(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(tgUpdatesResponse{
			OK: true,
			Result: []tgUpdate{
				{UpdateID: 1, Message: &tgMessage{MessageID: 1, From: tgUser{ID: 1, Username: "h"}, Chat: tgChat{ID: 7}, Text: "one"}},
				{UpdateID: 2, Message: &tgMessage{MessageID: 2, From: tgUser{ID: 1, Username: "h"}, Chat: tgChat{ID: 7}, Text: "two"}},
			},
		})
	}))
	defer srv.Close()

	store := NewMemoryOffsetStore()
	src, err := NewTelegramSource(TelegramConfig{
		Token:         "fake",
		ChatAllowlist: []int64{7},
		APIBase:       srv.URL,
		OffsetStore:   store,
	})
	if err != nil {
		t.Fatalf("NewTelegramSource: %v", err)
	}

	if _, err := src.Read(context.Background()); err != nil {
		t.Fatalf("Read 1: %v", err)
	}
	mid, _ := store.Load()
	if mid != 2 {
		t.Errorf("offset after first read = %d, want 2", mid)
	}
	if _, err := src.Read(context.Background()); err != nil {
		t.Fatalf("Read 2: %v", err)
	}
	end, _ := store.Load()
	if end != 3 {
		t.Errorf("offset after second read = %d, want 3", end)
	}
}
