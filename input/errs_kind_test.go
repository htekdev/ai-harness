package input

import (
	"os"
	"testing"

	"github.com/htekdev/ai-harness/harness/errs"
)

// TestNewTelegramSource_KindSource ensures construction-time validation
// surfaces a KindSource typed error so operators can classify config
// vs source vs persistence failures without string-matching.
func TestNewTelegramSource_KindSource(t *testing.T) {
	cases := []struct {
		name string
		cfg  TelegramConfig
	}{
		{"missing token", TelegramConfig{ChatAllowlist: []int64{1}}},
		{"empty allowlist", TelegramConfig{Token: "x"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := NewTelegramSource(tc.cfg)
			if err == nil {
				t.Fatalf("expected error, got nil")
			}
			if got := errs.KindOf(err); got != errs.KindSource {
				t.Fatalf("expected KindSource, got %v", got)
			}
		})
	}
}

// TestNewMeshWireSource_KindSource ensures meshwire constructor errors
// are classified as KindSource.
func TestNewMeshWireSource_KindSource(t *testing.T) {
	cases := []struct {
		name string
		cfg  MeshWireConfig
	}{
		{"missing token", MeshWireConfig{MeshID: "m", AgentID: "a", SenderAllowlist: []string{"s"}}},
		{"missing mesh", MeshWireConfig{Token: "t", AgentID: "a", SenderAllowlist: []string{"s"}}},
		{"missing agent", MeshWireConfig{Token: "t", MeshID: "m", SenderAllowlist: []string{"s"}}},
		{"empty allowlist", MeshWireConfig{Token: "t", MeshID: "m", AgentID: "a"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := NewMeshWireSource(tc.cfg)
			if err == nil {
				t.Fatalf("expected error, got nil")
			}
			if got := errs.KindOf(err); got != errs.KindSource {
				t.Fatalf("expected KindSource, got %v", got)
			}
		})
	}
}

// TestFileOffsetStore_KindPersistence ensures offset store errors are
// classified as KindPersistence — the layer below input sources that
// owns durable state.
func TestFileOffsetStore_KindPersistence(t *testing.T) {
	// Empty path → KindPersistence on Load and Save
	store := NewFileOffsetStore("")
	if _, err := store.Load(); err == nil {
		t.Fatalf("expected Load error, got nil")
	} else if got := errs.KindOf(err); got != errs.KindPersistence {
		t.Fatalf("Load: expected KindPersistence, got %v", got)
	}
	if err := store.Save(42); err == nil {
		t.Fatalf("expected Save error, got nil")
	} else if got := errs.KindOf(err); got != errs.KindPersistence {
		t.Fatalf("Save: expected KindPersistence, got %v", got)
	}
}

// TestFileOffsetStore_CorruptFile_KindPersistence ensures a corrupt
// store file surfaces a KindPersistence error instead of an opaque
// "does not contain a valid offset" string.
func TestFileOffsetStore_CorruptFile_KindPersistence(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/offset"
	// Write an unparseable payload (not JSON, not int).
	if err := os.WriteFile(path, []byte("garbage-not-an-int"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	store := NewFileOffsetStore(path)
	_, err := store.Load()
	if err == nil {
		t.Fatalf("expected Load error for corrupt file, got nil")
	}
	if got := errs.KindOf(err); got != errs.KindPersistence {
		t.Fatalf("expected KindPersistence, got %v", got)
	}
}
