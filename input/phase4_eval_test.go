package input

// Phase 4 primitive eval coverage.
//
// These tests beef up the Phase 4 (Event Sources / Watcher Adapters) eval
// surface beyond per-method unit checks. They exercise full round-trip
// behaviours that the existing per-source tests don't reach:
//
//   - MeshWire bidirectional multi-turn loop (parallel to TelegramSource_MultiTurnLoop).
//   - MeshWire restart-resume: a pre-existing offset prevents re-delivery.
//   - MeshWire reply failure must not roll back the offset (mirrors Telegram invariant).
//   - Telegram getUpdates non-2xx response surfaces as an error from Read.
//   - MeshWire getMessages non-2xx response surfaces as an error from Read.
//   - Telegram offset advances across messages from non-allowlisted chats
//     (filtered messages are still acked so they aren't re-delivered).
//
// All tests use httptest fakes — no real Telegram or MeshWire traffic — so
// they are safe to run in CI without secrets.

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
)

// TestMeshWireSource_MultiTurnLoop exercises the full bidirectional MeshWire
// Source + Replier contract end-to-end, the way runServe will drive it in
// production: each inbound message produces an Event that the caller "answers"
// via Reply, and the next poll observes the advanced offset (proving the
// previous turn was acked) before delivering the next turn.
//
// Mirrors TestTelegramSource_MultiTurnLoop so any future regression in the
// shared Source/Replier contract trips both tests in parallel.
func TestMeshWireSource_MultiTurnLoop(t *testing.T) {
	const peerID = "trusted-peer"
	const myID = "ai-harness"
	const meshID = "family-mesh"

	type batch struct {
		expectOffset string
		messages     []mwMessage
	}
	batches := []batch{
		{
			expectOffset: "1",
			messages: []mwMessage{
				{MessageID: 100, MessageUID: "u100", SenderID: peerID, RecipientID: myID, Content: "first turn", Priority: "normal"},
			},
		},
		{
			expectOffset: "101",
			messages: []mwMessage{
				{MessageID: 200, MessageUID: "u200", SenderID: peerID, RecipientID: myID, Content: "second turn", Priority: "high"},
			},
		},
		{
			expectOffset: "201",
			messages:     nil,
		},
	}

	type sent struct {
		path string
		body map[string]string
	}

	var (
		mu      sync.Mutex
		pollIdx int
		replies []sent
	)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/messages"):
			mu.Lock()
			defer mu.Unlock()
			if pollIdx >= len(batches) {
				_ = json.NewEncoder(w).Encode(mwMessagesResponse{OK: true})
				return
			}
			b := batches[pollIdx]
			if got := r.URL.Query().Get("offset"); got != b.expectOffset {
				http.Error(w, fmt.Sprintf("unexpected offset: got %q want %q", got, b.expectOffset), http.StatusBadRequest)
				return
			}
			pollIdx++
			_ = json.NewEncoder(w).Encode(mwMessagesResponse{OK: true, Messages: b.messages, Count: len(b.messages)})
		case r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/reply"):
			raw, _ := io.ReadAll(r.Body)
			var body map[string]string
			_ = json.Unmarshal(raw, &body)
			mu.Lock()
			replies = append(replies, sent{path: r.URL.Path, body: body})
			mu.Unlock()
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"ok":true}`))
		default:
			http.Error(w, "unexpected "+r.Method+" "+r.URL.Path, http.StatusNotFound)
		}
	}))
	defer srv.Close()

	store := NewMemoryOffsetStore()
	src, err := NewMeshWireSource(MeshWireConfig{
		Token:           "mw_secret",
		MeshID:          meshID,
		AgentID:         myID,
		SenderAllowlist: []string{peerID},
		APIBase:         srv.URL,
		OffsetStore:     store,
	})
	if err != nil {
		t.Fatalf("NewMeshWireSource: %v", err)
	}
	defer src.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	answer := func(n int, in string) string {
		return fmt.Sprintf("turn-%d ack: %s", n, in)
	}

	// Turn 1.
	ev1, err := src.Read(ctx)
	if err != nil {
		t.Fatalf("Read turn 1: %v", err)
	}
	if ev1.Text != "first turn" {
		t.Errorf("turn 1 text = %q", ev1.Text)
	}
	if ev1.SessionKey != peerID {
		t.Errorf("turn 1 session key = %q, want %q", ev1.SessionKey, peerID)
	}
	if got, _ := store.Load(); got != 100 {
		t.Errorf("offset after turn 1 = %d, want 100", got)
	}
	if err := src.Reply(ctx, ev1, answer(1, ev1.Text)); err != nil {
		t.Fatalf("Reply 1: %v", err)
	}

	// Turn 2.
	ev2, err := src.Read(ctx)
	if err != nil {
		t.Fatalf("Read turn 2: %v", err)
	}
	if ev2.Text != "second turn" {
		t.Errorf("turn 2 text = %q", ev2.Text)
	}
	if got, _ := store.Load(); got != 200 {
		t.Errorf("offset after turn 2 = %d, want 200", got)
	}
	if err := src.Reply(ctx, ev2, answer(2, ev2.Text)); err != nil {
		t.Fatalf("Reply 2: %v", err)
	}

	mu.Lock()
	got := append([]sent(nil), replies...)
	mu.Unlock()

	want := []sent{
		{path: "/mesh/family-mesh/messages/100/reply", body: map[string]string{"sender_id": myID, "content": "turn-1 ack: first turn"}},
		{path: "/mesh/family-mesh/messages/200/reply", body: map[string]string{"sender_id": myID, "content": "turn-2 ack: second turn"}},
	}
	if len(got) != len(want) {
		t.Fatalf("reply count = %d, want %d (got=%+v)", len(got), len(want), got)
	}
	for i, w := range want {
		if got[i].path != w.path {
			t.Errorf("reply[%d] path = %q, want %q", i, got[i].path, w.path)
		}
		if got[i].body["sender_id"] != w.body["sender_id"] || got[i].body["content"] != w.body["content"] {
			t.Errorf("reply[%d] body = %+v, want %+v", i, got[i].body, w.body)
		}
	}
}

// TestMeshWireSource_RestoresOffsetFromStore proves the bot does not
// re-process previously acked messages after a restart, the MeshWire analogue
// of TestTelegramSource_RestoresOffsetFromStore.
func TestMeshWireSource_RestoresOffsetFromStore(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mw.offset")
	store := NewFileOffsetStore(path)
	if err := store.Save(500); err != nil {
		t.Fatalf("seed Save: %v", err)
	}

	var capturedOffset string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedOffset = r.URL.Query().Get("offset")
		_ = json.NewEncoder(w).Encode(mwMessagesResponse{
			OK: true,
			Messages: []mwMessage{
				{MessageID: 600, SenderID: "peer", RecipientID: "ai-harness", Content: "post-restart"},
			},
		})
	}))
	defer srv.Close()

	src, err := NewMeshWireSource(MeshWireConfig{
		Token:           "mw_secret",
		MeshID:          "m1",
		AgentID:         "ai-harness",
		SenderAllowlist: []string{"peer"},
		APIBase:         srv.URL,
		OffsetStore:     NewFileOffsetStore(path),
	})
	if err != nil {
		t.Fatalf("NewMeshWireSource: %v", err)
	}
	if src.lastSeenID != 500 {
		t.Errorf("lastSeenID after construct = %d, want 500", src.lastSeenID)
	}
	if _, err := src.Read(context.Background()); err != nil {
		t.Fatalf("Read: %v", err)
	}
	if capturedOffset != "501" {
		t.Errorf("getMessages offset = %q, want 501 (lastSeenID+1)", capturedOffset)
	}
	persisted, err := NewFileOffsetStore(path).Load()
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if persisted != 600 {
		t.Errorf("persisted offset = %d, want 600", persisted)
	}
}

// TestMeshWireSource_ReplyFailureDoesNotRollBackOffset locks in the same
// invariant the Telegram suite asserts: a transient reply failure must NOT
// roll back the input offset, because the message has already been read.
// Re-reading would double-process the user's turn.
func TestMeshWireSource_ReplyFailureDoesNotRollBackOffset(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet:
			_ = json.NewEncoder(w).Encode(mwMessagesResponse{
				OK: true,
				Messages: []mwMessage{
					{MessageID: 900, SenderID: "peer", RecipientID: "ai-harness", Content: "hello"},
				},
			})
		case r.Method == http.MethodPost:
			http.Error(w, `{"ok":false,"description":"rate limited"}`, http.StatusTooManyRequests)
		}
	}))
	defer srv.Close()

	store := NewMemoryOffsetStore()
	src, err := NewMeshWireSource(MeshWireConfig{
		Token:           "mw_secret",
		MeshID:          "m1",
		AgentID:         "ai-harness",
		SenderAllowlist: []string{"peer"},
		APIBase:         srv.URL,
		OffsetStore:     store,
	})
	if err != nil {
		t.Fatalf("NewMeshWireSource: %v", err)
	}

	ev, err := src.Read(context.Background())
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if got, _ := store.Load(); got != 900 {
		t.Errorf("offset after Read = %d, want 900", got)
	}
	// Reply must populate message_id so the source can build the reply URL.
	if ev.Metadata["message_id"] != "900" {
		t.Fatalf("event metadata message_id = %q, want 900", ev.Metadata["message_id"])
	}
	if err := src.Reply(context.Background(), ev, "won't make it"); err == nil {
		t.Error("expected Reply error from 429, got nil")
	}
	if got, _ := store.Load(); got != 900 {
		t.Errorf("offset after failed Reply = %d, want 900 (must not roll back)", got)
	}
}

// TestTelegramSource_PollErrorSurfaces verifies that a non-2xx response from
// /getUpdates is surfaced as an error from Read rather than silently swallowed.
// Without this, a misconfigured token would loop forever returning the same
// error to the serve loop without any visible signal.
func TestTelegramSource_PollErrorSurfaces(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"ok":false,"error_code":401,"description":"Unauthorized"}`, http.StatusUnauthorized)
	}))
	defer srv.Close()

	src, err := NewTelegramSource(TelegramConfig{
		Token:         "bad-token",
		ChatAllowlist: []int64{7},
		APIBase:       srv.URL,
	})
	if err != nil {
		t.Fatalf("NewTelegramSource: %v", err)
	}
	_, err = src.Read(context.Background())
	if err == nil {
		t.Fatal("expected error from Read when getUpdates returns 401, got nil")
	}
	if !strings.Contains(err.Error(), "401") && !strings.Contains(err.Error(), "Unauthorized") {
		t.Errorf("error should mention 401/Unauthorized, got: %v", err)
	}
}

// TestMeshWireSource_PollErrorSurfaces is the MeshWire analogue of the
// Telegram poll-error test. A 500 from /messages must surface as an error.
func TestMeshWireSource_PollErrorSurfaces(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"ok":false,"error":"internal"}`, http.StatusInternalServerError)
	}))
	defer srv.Close()

	src, err := NewMeshWireSource(MeshWireConfig{
		Token:           "mw_x",
		MeshID:          "m1",
		AgentID:         "ai-harness",
		SenderAllowlist: []string{"peer"},
		APIBase:         srv.URL,
	})
	if err != nil {
		t.Fatalf("NewMeshWireSource: %v", err)
	}
	_, err = src.Read(context.Background())
	if err == nil {
		t.Fatal("expected error from Read when getMessages returns 500, got nil")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("error should mention 500, got: %v", err)
	}
}

// TestTelegramSource_OffsetAdvancesAcrossFilteredChats locks in a subtle but
// critical invariant: messages from non-allowlisted chats must still advance
// the offset. Otherwise Telegram would keep redelivering them on every poll
// and the bot would spend all its quota rejecting the same noise forever.
func TestTelegramSource_OffsetAdvancesAcrossFilteredChats(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(tgUpdatesResponse{
			OK: true,
			Result: []tgUpdate{
				{UpdateID: 10, Message: &tgMessage{MessageID: 1, From: tgUser{ID: 1, Username: "spammer"}, Chat: tgChat{ID: 999}, Text: "spam 1"}},
				{UpdateID: 11, Message: &tgMessage{MessageID: 2, From: tgUser{ID: 1, Username: "spammer"}, Chat: tgChat{ID: 998}, Text: "spam 2"}},
				{UpdateID: 12, Message: &tgMessage{MessageID: 3, From: tgUser{ID: 42, Username: "hector"}, Chat: tgChat{ID: 7}, Text: "real msg"}},
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

	ev, err := src.Read(context.Background())
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if ev.Text != "real msg" {
		t.Errorf("text = %q, want 'real msg'", ev.Text)
	}
	// All three updates (incl. the two filtered) must have been acked.
	got, _ := store.Load()
	if got != 13 {
		t.Errorf("persisted offset = %d, want 13 (must advance past filtered chats too)", got)
	}
}

// TestMeshWireSource_OffsetAdvancesAcrossFilteredSenders mirrors the Telegram
// invariant for MeshWire: messages from non-allowlisted senders must still
// advance lastSeenID so they are not re-delivered indefinitely.
func TestMeshWireSource_OffsetAdvancesAcrossFilteredSenders(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(mwMessagesResponse{
			OK: true,
			Messages: []mwMessage{
				{MessageID: 50, SenderID: "spammer-a", Content: "spam 1"},
				{MessageID: 51, SenderID: "spammer-b", Content: "spam 2"},
				{MessageID: 52, SenderID: "trusted", Content: "real msg"},
			},
		})
	}))
	defer srv.Close()

	store := NewMemoryOffsetStore()
	src, err := NewMeshWireSource(MeshWireConfig{
		Token:           "mw_secret",
		MeshID:          "m1",
		AgentID:         "ai-harness",
		SenderAllowlist: []string{"trusted"},
		APIBase:         srv.URL,
		OffsetStore:     store,
	})
	if err != nil {
		t.Fatalf("NewMeshWireSource: %v", err)
	}

	ev, err := src.Read(context.Background())
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if ev.Text != "real msg" {
		t.Errorf("text = %q, want 'real msg'", ev.Text)
	}
	got, _ := store.Load()
	if got != 52 {
		t.Errorf("persisted offset = %d, want 52 (must advance past filtered senders)", got)
	}
}

// TestTelegramSource_GetUpdatesPassesPollTimeout sanity-checks that the
// configured PollTimeoutSeconds value reaches the wire as `timeout=`. Without
// this, a misread default would silently turn long-polling into a tight loop
// against the Telegram API.
func TestTelegramSource_GetUpdatesPassesPollTimeout(t *testing.T) {
	var capturedTimeout string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedTimeout = r.URL.Query().Get("timeout")
		_ = json.NewEncoder(w).Encode(tgUpdatesResponse{
			OK: true,
			Result: []tgUpdate{
				{UpdateID: 1, Message: &tgMessage{MessageID: 1, From: tgUser{ID: 1, Username: "h"}, Chat: tgChat{ID: 1}, Text: "hi"}},
			},
		})
	}))
	defer srv.Close()

	src, err := NewTelegramSource(TelegramConfig{
		Token:              "fake",
		ChatAllowlist:      []int64{1},
		APIBase:            srv.URL,
		PollTimeoutSeconds: 17,
	})
	if err != nil {
		t.Fatalf("NewTelegramSource: %v", err)
	}

	if _, err := src.Read(context.Background()); err != nil {
		t.Fatalf("Read: %v", err)
	}

	if capturedTimeout != strconv.Itoa(17) {
		t.Errorf("timeout query = %q, want 17", capturedTimeout)
	}
}
