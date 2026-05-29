package persistence

import "testing"

func TestEventLogRebuildMainBranch(t *testing.T) {
	log := NewEventLog()

	_, err := log.Append(Event{
		ID:   "e1",
		Kind: KindArtifactUpsert,
		Artifact: &ArtifactChange{
			Name:   "base",
			Type:   "plugin",
			Source: ".harness/artifacts/base.md",
			Active: true,
		},
	})
	if err != nil {
		t.Fatalf("append e1: %v", err)
	}
	_, err = log.Append(Event{
		ID:   "e2",
		Kind: KindRuntimeTurnStart,
		Runtime: &RuntimeChange{
			SessionID: "s1",
			Turn:      1,
		},
	})
	if err != nil {
		t.Fatalf("append e2: %v", err)
	}
	_, err = log.Append(Event{
		ID:   "e3",
		Kind: KindRuntimeHookDispatch,
		Runtime: &RuntimeChange{
			SessionID: "s1",
			Turn:      1,
			HookEvent: "tool.pre",
		},
	})
	if err != nil {
		t.Fatalf("append e3: %v", err)
	}

	state, err := log.Rebuild("main")
	if err != nil {
		t.Fatalf("rebuild: %v", err)
	}

	if state.Runtime.SessionID != "s1" {
		t.Fatalf("runtime session_id = %q, want %q", state.Runtime.SessionID, "s1")
	}
	if state.Runtime.Turn != 1 {
		t.Fatalf("runtime turn = %d, want %d", state.Runtime.Turn, 1)
	}
	if state.Runtime.LastHookEvent != "tool.pre" {
		t.Fatalf("runtime last_hook_event = %q, want %q", state.Runtime.LastHookEvent, "tool.pre")
	}
	if _, ok := state.Artifacts["base"]; !ok {
		t.Fatalf("expected artifact to exist")
	}
}

func TestEventLogBranchReplayUsesLineage(t *testing.T) {
	log := NewEventLog()

	_, err := log.Append(Event{
		ID:   "root",
		Kind: KindArtifactUpsert,
		Artifact: &ArtifactChange{
			Name:   "base",
			Type:   "plugin",
			Source: ".harness/artifacts/base.md",
			Active: true,
		},
	})
	if err != nil {
		t.Fatalf("append root: %v", err)
	}
	_, err = log.Append(Event{
		ID:       "main-2",
		ParentID: "root",
		Branch:   "main",
		Kind:     KindArtifactRemove,
		Artifact: &ArtifactChange{Name: "base"},
	})
	if err != nil {
		t.Fatalf("append main-2: %v", err)
	}
	_, err = log.Append(Event{
		ID:       "feature-2",
		ParentID: "root",
		Branch:   "feature/audit",
		Kind:     KindArtifactUpsert,
		Artifact: &ArtifactChange{Name: "audit", Type: "plugin", Active: true},
	})
	if err != nil {
		t.Fatalf("append feature-2: %v", err)
	}

	mainState, err := log.Rebuild("main")
	if err != nil {
		t.Fatalf("rebuild main: %v", err)
	}
	if _, ok := mainState.Artifacts["base"]; ok {
		t.Fatalf("main branch should have removed base artifact")
	}

	featureState, err := log.Rebuild("feature/audit")
	if err != nil {
		t.Fatalf("rebuild feature: %v", err)
	}
	if _, ok := featureState.Artifacts["base"]; !ok {
		t.Fatalf("feature branch should still include root base artifact")
	}
	if _, ok := featureState.Artifacts["audit"]; !ok {
		t.Fatalf("feature branch should include audit artifact")
	}
}
