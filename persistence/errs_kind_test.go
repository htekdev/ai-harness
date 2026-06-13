package persistence

import (
	"testing"

	harnesserrs "github.com/htekdev/ai-harness/harness/errs"
)

// TestEventLog_KindPersistence ensures append/replay errors are typed
// as KindPersistence — the persistence layer is the one runtime tier
// where a typed classification matters most for retry/alert logic.
func TestEventLog_KindPersistence(t *testing.T) {
	log := NewEventLog()

	// Empty event id → KindPersistence
	if _, err := log.Append(Event{}); err == nil {
		t.Fatal("expected error for empty id, got nil")
	} else if got := harnesserrs.KindOf(err); got != harnesserrs.KindPersistence {
		t.Fatalf("empty id: expected KindPersistence, got %v", got)
	}

	first, err := log.Append(Event{ID: "e1", Kind: KindArtifactUpsert})
	if err != nil {
		t.Fatalf("first append: %v", err)
	}
	_ = first

	// Duplicate id → KindPersistence
	if _, err := log.Append(Event{ID: "e1"}); err == nil {
		t.Fatal("expected error for duplicate id, got nil")
	} else if got := harnesserrs.KindOf(err); got != harnesserrs.KindPersistence {
		t.Fatalf("duplicate id: expected KindPersistence, got %v", got)
	}

	// Unknown parent → KindPersistence
	if _, err := log.Append(Event{ID: "e2", ParentID: "missing"}); err == nil {
		t.Fatal("expected error for missing parent, got nil")
	} else if got := harnesserrs.KindOf(err); got != harnesserrs.KindPersistence {
		t.Fatalf("missing parent: expected KindPersistence, got %v", got)
	}

	// Replay unknown branch → KindPersistence
	if _, err := log.Replay("nonexistent"); err == nil {
		t.Fatal("expected error for missing branch, got nil")
	} else if got := harnesserrs.KindOf(err); got != harnesserrs.KindPersistence {
		t.Fatalf("missing branch: expected KindPersistence, got %v", got)
	}
}
