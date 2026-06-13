package input

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func TestNewMeshWireSource_RequiresToken(t *testing.T) {
	_, err := NewMeshWireSource(MeshWireConfig{
		MeshID: "m1", AgentID: "ai-harness", SenderAllowlist: []string{"peer"},
	})
	if err == nil {
		t.Fatal("expected error when token missing")
	}
}

func TestNewMeshWireSource_RequiresMeshID(t *testing.T) {
	_, err := NewMeshWireSource(MeshWireConfig{
		Token: "mw_x", AgentID: "ai-harness", SenderAllowlist: []string{"peer"},
	})
	if err == nil {
		t.Fatal("expected error when mesh_id missing")
	}
}

func TestNewMeshWireSource_RequiresAgentID(t *testing.T) {
	_, err := NewMeshWireSource(MeshWireConfig{
		Token: "mw_x", MeshID: "m1", SenderAllowlist: []string{"peer"},
	})
	if err == nil {
		t.Fatal("expected error when agent_id missing")
	}
}

func TestNewMeshWireSource_RequiresAllowlist(t *testing.T) {
	_, err := NewMeshWireSource(MeshWireConfig{
		Token: "mw_x", MeshID: "m1", AgentID: "ai-harness",
	})
	if err == nil {
		t.Fatal("expected error when sender_allowlist empty (no wildcard in v1)")
	}
}

func TestNewMeshWireSource_DefaultsTimeout(t *testing.T) {
	src, err := NewMeshWireSource(MeshWireConfig{
		Token: "mw_x", MeshID: "m1", AgentID: "ai-harness", SenderAllowlist: []string{"peer"},
	})
	if err != nil {
		t.Fatalf("constructor: %v", err)
	}
	if src.pollTimeoutS != 30 {
		t.Errorf("default timeout = %d, want 30", src.pollTimeoutS)
	}
}

func TestNewMeshWireSource_CapsTimeout(t *testing.T) {
	src, err := NewMeshWireSource(MeshWireConfig{
		Token: "mw_x", MeshID: "m1", AgentID: "ai-harness",
		SenderAllowlist: []string{"peer"}, PollTimeoutSeconds: 999,
	})
	if err != nil {
		t.Fatalf("constructor: %v", err)
	}
	if src.pollTimeoutS != 60 {
		t.Errorf("timeout = %d, want 60 (capped)", src.pollTimeoutS)
	}
}

// TestMeshWireSource_ReadFiltersAllowlistAndOffset exercises the full Read path
// with a fake MeshWire API and verifies that:
//   - messages from non-allowlisted senders are skipped
//   - messages with message_id <= lastSeenID are skipped (client-side dedupe)
//   - the returned Event uses sender_id as SessionKey and carries metadata
//   - lastSeenID advances to the highest seen message_id
//   - the request includes the right Authorization header and recipient filter
func TestMeshWireSource_ReadFiltersAllowlistAndOffset(t *testing.T) {
	var captured struct {
		auth      string
		recipient string
		offset    string
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured.auth = r.Header.Get("Authorization")
		captured.recipient = r.URL.Query().Get("recipient")
		captured.offset = r.URL.Query().Get("offset")

		if !strings.Contains(r.URL.Path, "/mesh/m1/messages") {
			http.Error(w, "unexpected path "+r.URL.Path, http.StatusNotFound)
			return
		}
		_ = json.NewEncoder(w).Encode(mwMessagesResponse{
			OK: true,
			Messages: []mwMessage{
				{MessageID: 100, MessageUID: "u100", SenderID: "blocked-peer", RecipientID: "ai-harness", Content: "should be filtered", Priority: "normal"},
				{MessageID: 101, MessageUID: "u101", SenderID: "trusted-peer", RecipientID: "ai-harness", Content: "hello harness", Priority: "high"},
			},
			Count: 2,
		})
	}))
	defer srv.Close()

	src, err := NewMeshWireSource(MeshWireConfig{
		Token:           "mw_secret",
		MeshID:          "m1",
		AgentID:         "ai-harness",
		SenderAllowlist: []string{"trusted-peer"},
		APIBase:         srv.URL,
	})
	if err != nil {
		t.Fatalf("constructor: %v", err)
	}

	ev, err := src.Read(context.Background())
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if ev.SourceName != "meshwire" {
		t.Errorf("source = %q, want meshwire", ev.SourceName)
	}
	if ev.Text != "hello harness" {
		t.Errorf("text = %q, want %q", ev.Text, "hello harness")
	}
	if ev.SessionKey != "trusted-peer" {
		t.Errorf("session key = %q, want trusted-peer", ev.SessionKey)
	}
	if ev.Metadata["message_id"] != "101" {
		t.Errorf("message_id = %q, want 101", ev.Metadata["message_id"])
	}
	if ev.Metadata["message_uid"] != "u101" {
		t.Errorf("message_uid = %q, want u101", ev.Metadata["message_uid"])
	}
	if ev.Metadata["mesh_id"] != "m1" {
		t.Errorf("mesh_id = %q, want m1", ev.Metadata["mesh_id"])
	}
	if ev.Metadata["priority"] != "high" {
		t.Errorf("priority = %q, want high", ev.Metadata["priority"])
	}
	if src.lastSeenID != 101 {
		t.Errorf("lastSeenID = %d, want 101", src.lastSeenID)
	}
	if captured.auth != "Bearer mw_secret" {
		t.Errorf("auth header = %q, want %q", captured.auth, "Bearer mw_secret")
	}
	if captured.recipient != "ai-harness" {
		t.Errorf("recipient query = %q, want ai-harness", captured.recipient)
	}
	if captured.offset != "1" {
		t.Errorf("offset query = %q, want 1", captured.offset)
	}
}

// TestMeshWireSource_ReadDedupesAlreadySeen confirms that messages with
// message_id <= lastSeenID (e.g. after a restart with a persisted offset)
// do not produce duplicate Events, even though the server may re-deliver them.
func TestMeshWireSource_ReadDedupesAlreadySeen(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(mwMessagesResponse{
			OK: true,
			Messages: []mwMessage{
				{MessageID: 50, SenderID: "peer", Content: "old"},   // <= persisted lastSeen
				{MessageID: 75, SenderID: "peer", Content: "older"}, // <= persisted lastSeen
				{MessageID: 200, SenderID: "peer", Content: "new"},
			},
		})
	}))
	defer srv.Close()

	store := NewFileOffsetStore(filepath.Join(t.TempDir(), "mw.offset"))
	if err := store.Save(150); err != nil {
		t.Fatalf("seed offset: %v", err)
	}

	src, err := NewMeshWireSource(MeshWireConfig{
		Token:           "mw_secret",
		MeshID:          "m1",
		AgentID:         "ai-harness",
		SenderAllowlist: []string{"peer"},
		APIBase:         srv.URL,
		OffsetStore:     store,
	})
	if err != nil {
		t.Fatalf("constructor: %v", err)
	}
	if src.lastSeenID != 150 {
		t.Fatalf("lastSeenID after load = %d, want 150", src.lastSeenID)
	}

	ev, err := src.Read(context.Background())
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if ev.Text != "new" {
		t.Errorf("text = %q, want new (dedupe should have skipped 50 and 75)", ev.Text)
	}
	if src.lastSeenID != 200 {
		t.Errorf("lastSeenID = %d, want 200", src.lastSeenID)
	}
	loaded, err := store.Load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if loaded != 200 {
		t.Errorf("persisted offset = %d, want 200", loaded)
	}
}

// TestMeshWireSource_Reply posts a reply via the fake API and verifies the
// path, auth header, and JSON body shape.
func TestMeshWireSource_Reply(t *testing.T) {
	var captured struct {
		path string
		auth string
		body map[string]string
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured.path = r.URL.Path
		captured.auth = r.Header.Get("Authorization")
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &captured.body)
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	src, err := NewMeshWireSource(MeshWireConfig{
		Token:           "mw_secret",
		MeshID:          "m1",
		AgentID:         "ai-harness",
		SenderAllowlist: []string{"peer"},
		APIBase:         srv.URL,
	})
	if err != nil {
		t.Fatalf("constructor: %v", err)
	}

	ev := Event{
		SourceName: "meshwire",
		SessionKey: "peer",
		Metadata: map[string]string{
			"mesh_id":    "m1",
			"message_id": "101",
		},
	}
	if err := src.Reply(context.Background(), ev, "pong"); err != nil {
		t.Fatalf("Reply: %v", err)
	}
	if captured.path != "/mesh/m1/messages/101/reply" {
		t.Errorf("path = %q, want /mesh/m1/messages/101/reply", captured.path)
	}
	if captured.auth != "Bearer mw_secret" {
		t.Errorf("auth = %q, want Bearer mw_secret", captured.auth)
	}
	if captured.body["sender_id"] != "ai-harness" {
		t.Errorf("sender_id = %q, want ai-harness", captured.body["sender_id"])
	}
	if captured.body["content"] != "pong" {
		t.Errorf("content = %q, want pong", captured.body["content"])
	}
}

// TestMeshWireSource_ReplyMissingMessageID guards the contract that callers
// must pass the original message_id in metadata to enable threaded replies.
func TestMeshWireSource_ReplyMissingMessageID(t *testing.T) {
	src, err := NewMeshWireSource(MeshWireConfig{
		Token: "mw_x", MeshID: "m1", AgentID: "ai-harness",
		SenderAllowlist: []string{"peer"},
	})
	if err != nil {
		t.Fatalf("constructor: %v", err)
	}
	err = src.Reply(context.Background(), Event{Metadata: map[string]string{}}, "pong")
	if err == nil {
		t.Fatal("expected error when message_id is absent from metadata")
	}
}
