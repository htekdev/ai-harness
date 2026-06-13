package input

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestStdinSource_ReadsLines(t *testing.T) {
	src := NewStdinSource(strings.NewReader("hello\n\n  world  \nbye\n"), nil)
	defer src.Close()

	want := []string{"hello", "world", "bye"}
	for _, w := range want {
		ev, err := src.Read(context.Background())
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if ev.Text != w {
			t.Errorf("got %q, want %q", ev.Text, w)
		}
		if ev.SourceName != "stdin" {
			t.Errorf("source name = %q, want %q", ev.SourceName, "stdin")
		}
		if ev.SessionKey != "" {
			t.Errorf("session key = %q, want empty", ev.SessionKey)
		}
	}
	if _, err := src.Read(context.Background()); !errors.Is(err, io.EOF) {
		t.Errorf("expected io.EOF after exhausting reader, got %v", err)
	}
}

func TestStdinSource_RespectsContextCancel(t *testing.T) {
	src := NewStdinSource(strings.NewReader("hello\n"), nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := src.Read(ctx); !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got %v", err)
	}
}

func TestNewTelegramSource_RequiresToken(t *testing.T) {
	_, err := NewTelegramSource(TelegramConfig{Token: "", ChatAllowlist: []int64{1}})
	if err == nil {
		t.Fatal("expected error when token missing")
	}
}

func TestNewTelegramSource_RequiresAllowlist(t *testing.T) {
	_, err := NewTelegramSource(TelegramConfig{Token: "abc", ChatAllowlist: nil})
	if err == nil {
		t.Fatal("expected error when allowlist empty (no wildcard in v1)")
	}
}

func TestNewTelegramSource_DefaultsTimeout(t *testing.T) {
	src, err := NewTelegramSource(TelegramConfig{Token: "abc", ChatAllowlist: []int64{1}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if src.pollTimeoutS != 25 {
		t.Errorf("default timeout = %d, want 25", src.pollTimeoutS)
	}
}

func TestNewTelegramSource_CapsTimeout(t *testing.T) {
	src, err := NewTelegramSource(TelegramConfig{Token: "abc", ChatAllowlist: []int64{1}, PollTimeoutSeconds: 999})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if src.pollTimeoutS != 50 {
		t.Errorf("timeout = %d, want 50 (capped)", src.pollTimeoutS)
	}
}

// TestTelegramSource_ReadFiltersAllowlist exercises the full Read path with a
// fake Telegram API and verifies that allowlisted messages produce events while
// non-allowlisted chats are skipped.
func TestTelegramSource_ReadFiltersAllowlist(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/getUpdates") {
			http.Error(w, "unexpected path", http.StatusNotFound)
			return
		}
		_ = json.NewEncoder(w).Encode(tgUpdatesResponse{
			OK: true,
			Result: []tgUpdate{
				{UpdateID: 1, Message: &tgMessage{MessageID: 10, From: tgUser{ID: 100, Username: "blocked"}, Chat: tgChat{ID: 999}, Text: "should be filtered"}},
				{UpdateID: 2, Message: &tgMessage{MessageID: 11, From: tgUser{ID: 200, Username: "hector"}, Chat: tgChat{ID: 7729308746}, Text: "hello harness"}},
			},
		})
	}))
	defer srv.Close()

	src, err := NewTelegramSource(TelegramConfig{
		Token:         "fake",
		ChatAllowlist: []int64{7729308746},
		APIBase:       srv.URL,
	})
	if err != nil {
		t.Fatalf("constructor: %v", err)
	}

	ev, err := src.Read(context.Background())
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if ev.Text != "hello harness" {
		t.Errorf("text = %q, want %q", ev.Text, "hello harness")
	}
	if ev.SessionKey != "7729308746" {
		t.Errorf("session key = %q, want %q", ev.SessionKey, "7729308746")
	}
	if ev.Metadata["username"] != "hector" {
		t.Errorf("username = %q, want hector", ev.Metadata["username"])
	}
	if src.offset != 3 {
		t.Errorf("offset advanced to %d, want 3", src.offset)
	}
}

// TestTelegramSource_Reply sends a reply via the fake API and verifies the request body.
func TestTelegramSource_Reply(t *testing.T) {
	var captured struct {
		path string
		form string
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured.path = r.URL.Path
		body, _ := io.ReadAll(r.Body)
		captured.form = string(body)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	src, err := NewTelegramSource(TelegramConfig{
		Token:         "fake",
		ChatAllowlist: []int64{7729308746},
		APIBase:       srv.URL,
	})
	if err != nil {
		t.Fatalf("constructor: %v", err)
	}

	ev := Event{
		SourceName: "telegram",
		SessionKey: "7729308746",
		Metadata:   map[string]string{"chat_id": "7729308746"},
	}
	if err := src.Reply(context.Background(), ev, "pong"); err != nil {
		t.Fatalf("Reply: %v", err)
	}
	if !strings.HasSuffix(captured.path, "/sendMessage") {
		t.Errorf("path = %q, want suffix /sendMessage", captured.path)
	}
	if !strings.Contains(captured.form, "chat_id=7729308746") {
		t.Errorf("form missing chat_id: %q", captured.form)
	}
	if !strings.Contains(captured.form, "text=pong") {
		t.Errorf("form missing text: %q", captured.form)
	}
}
