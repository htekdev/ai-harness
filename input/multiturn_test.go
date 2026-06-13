package input

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"
)

// TestTelegramSource_MultiTurnLoop exercises the full bidirectional Source +
// Replier contract end-to-end: each inbound user turn produces an Event that the
// caller "answers" via Reply, and the next /getUpdates poll observes the new
// offset (proving the previous turn was acked) before delivering the next turn.
//
// This is the closest unit-level test we can write to the real serve loop
// without binding a live LLM: it simulates the round trip
//
//	Telegram → TelegramSource.Read → caller (would call h.Run) → TelegramSource.Reply → Telegram
//
// across multiple turns, and verifies:
//   - Read returns turns in order with correct chat_id routing
//   - Each Reply hits sendMessage with the originating chat_id and response body
//   - Successive /getUpdates calls advance the offset (no re-delivery)
//   - The OffsetStore reflects the latest acked update_id at every step
//
// If this test ever flakes, the bidirectional Telegram contract is broken —
// look here first before chasing serve-cmd or harness regressions.
func TestTelegramSource_MultiTurnLoop(t *testing.T) {
	const chatID int64 = 7729308746

	// Scripted server state: each call to /getUpdates returns the batch
	// scheduled for the *current* offset, then waits for the offset to advance.
	type batch struct {
		expectOffset string
		updates      []tgUpdate
	}
	batches := []batch{
		{
			expectOffset: "0",
			updates: []tgUpdate{
				{UpdateID: 100, Message: &tgMessage{
					MessageID: 1,
					From:      tgUser{ID: 42, Username: "hector"},
					Chat:      tgChat{ID: chatID},
					Text:      "what's the weather?",
				}},
			},
		},
		{
			expectOffset: "101",
			updates: []tgUpdate{
				{UpdateID: 200, Message: &tgMessage{
					MessageID: 2,
					From:      tgUser{ID: 42, Username: "hector"},
					Chat:      tgChat{ID: chatID},
					Text:      "thanks — and tomorrow?",
				}},
			},
		},
		{
			expectOffset: "201",
			updates:      nil, // empty long-poll once both turns are consumed
		},
	}

	type sentMessage struct {
		chatID string
		text   string
	}

	var (
		mu       sync.Mutex
		pollIdx  int
		sent     []sentMessage
		lastSeen string
	)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/getUpdates"):
			mu.Lock()
			defer mu.Unlock()
			if pollIdx >= len(batches) {
				_ = json.NewEncoder(w).Encode(tgUpdatesResponse{OK: true})
				return
			}
			b := batches[pollIdx]
			lastSeen = r.URL.Query().Get("offset")
			if lastSeen != b.expectOffset {
				http.Error(w, fmt.Sprintf("unexpected offset: got %q want %q", lastSeen, b.expectOffset), http.StatusBadRequest)
				return
			}
			pollIdx++
			_ = json.NewEncoder(w).Encode(tgUpdatesResponse{OK: true, Result: b.updates})
		case strings.Contains(r.URL.Path, "/sendMessage"):
			body, _ := io.ReadAll(r.Body)
			form, err := url.ParseQuery(string(body))
			if err != nil {
				http.Error(w, "bad form", http.StatusBadRequest)
				return
			}
			mu.Lock()
			sent = append(sent, sentMessage{
				chatID: form.Get("chat_id"),
				text:   form.Get("text"),
			})
			mu.Unlock()
			_, _ = w.Write([]byte(`{"ok":true}`))
		default:
			http.Error(w, "unexpected path "+r.URL.Path, http.StatusNotFound)
		}
	}))
	defer srv.Close()

	store := NewMemoryOffsetStore()
	src, err := NewTelegramSource(TelegramConfig{
		Token:         "fake",
		ChatAllowlist: []int64{chatID},
		APIBase:       srv.URL,
		OffsetStore:   store,
	})
	if err != nil {
		t.Fatalf("NewTelegramSource: %v", err)
	}
	defer src.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Stand-in for harness.Harness.Run — deterministic responder so we can
	// assert exactly what reaches sendMessage. A real harness would call an
	// LLM here; the bidirectional contract under test is identical either way.
	answer := func(turn int, in string) string {
		return fmt.Sprintf("turn-%d ack: %s", turn, in)
	}

	// Turn 1
	ev1, err := src.Read(ctx)
	if err != nil {
		t.Fatalf("Read turn 1: %v", err)
	}
	if ev1.Text != "what's the weather?" {
		t.Errorf("turn 1 text = %q", ev1.Text)
	}
	if ev1.SessionKey != strconv.FormatInt(chatID, 10) {
		t.Errorf("turn 1 session key = %q", ev1.SessionKey)
	}
	if got, _ := store.Load(); got != 101 {
		t.Errorf("offset after turn 1 = %d, want 101", got)
	}
	if err := src.Reply(ctx, ev1, answer(1, ev1.Text)); err != nil {
		t.Fatalf("Reply 1: %v", err)
	}

	// Turn 2
	ev2, err := src.Read(ctx)
	if err != nil {
		t.Fatalf("Read turn 2: %v", err)
	}
	if ev2.Text != "thanks — and tomorrow?" {
		t.Errorf("turn 2 text = %q", ev2.Text)
	}
	if got, _ := store.Load(); got != 201 {
		t.Errorf("offset after turn 2 = %d, want 201", got)
	}
	if err := src.Reply(ctx, ev2, answer(2, ev2.Text)); err != nil {
		t.Fatalf("Reply 2: %v", err)
	}

	// Verify every reply was routed to the originating chat in order.
	mu.Lock()
	gotSent := append([]sentMessage(nil), sent...)
	mu.Unlock()

	wantSent := []sentMessage{
		{chatID: strconv.FormatInt(chatID, 10), text: "turn-1 ack: what's the weather?"},
		{chatID: strconv.FormatInt(chatID, 10), text: "turn-2 ack: thanks — and tomorrow?"},
	}
	if len(gotSent) != len(wantSent) {
		t.Fatalf("sent count = %d, want %d (got=%+v)", len(gotSent), len(wantSent), gotSent)
	}
	for i, s := range wantSent {
		if gotSent[i] != s {
			t.Errorf("sent[%d] = %+v, want %+v", i, gotSent[i], s)
		}
	}
}

// TestTelegramSource_MultiTurnLoop_ReplyFailureDoesNotAdvanceOffset locks in a
// subtle invariant: a transient sendMessage failure must NOT roll back the
// offset. The update has been read and acked at the Telegram layer the moment
// /getUpdates returns it with offset=N+1; failing to deliver the harness reply
// is a *delivery* problem, not an *input* problem, and re-reading the same
// update on retry would double-process the user's turn.
//
// This guards against future "helpful" code that tries to undo the offset
// advance when Reply fails.
func TestTelegramSource_MultiTurnLoop_ReplyFailureDoesNotAdvanceOffset(t *testing.T) {
	const chatID int64 = 7729308746

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/getUpdates"):
			_ = json.NewEncoder(w).Encode(tgUpdatesResponse{
				OK: true,
				Result: []tgUpdate{
					{UpdateID: 500, Message: &tgMessage{
						MessageID: 1,
						From:      tgUser{ID: 1, Username: "h"},
						Chat:      tgChat{ID: chatID},
						Text:      "hello",
					}},
				},
			})
		case strings.Contains(r.URL.Path, "/sendMessage"):
			http.Error(w, `{"ok":false,"description":"rate limited"}`, http.StatusTooManyRequests)
		}
	}))
	defer srv.Close()

	store := NewMemoryOffsetStore()
	src, err := NewTelegramSource(TelegramConfig{
		Token:         "fake",
		ChatAllowlist: []int64{chatID},
		APIBase:       srv.URL,
		OffsetStore:   store,
	})
	if err != nil {
		t.Fatalf("NewTelegramSource: %v", err)
	}
	defer src.Close()

	ev, err := src.Read(context.Background())
	if err != nil {
		t.Fatalf("Read: %v", err)
	}

	if got, _ := store.Load(); got != 501 {
		t.Errorf("offset after Read = %d, want 501", got)
	}

	if err := src.Reply(context.Background(), ev, "won't make it"); err == nil {
		t.Error("expected Reply error from 429 sendMessage, got nil")
	}

	// Critical invariant: failed reply did not roll back the offset.
	if got, _ := store.Load(); got != 501 {
		t.Errorf("offset after failed Reply = %d, want 501 (must not roll back)", got)
	}
}
