package context

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// noopLoader returns empty content for any source (used when content is irrelevant).
func noopLoader(s Source) (string, error) {
	return "content of " + s.Name, nil
}

// errorLoader always returns an error (used to test load-failure paths).
func errorLoader(s Source) (string, error) {
	return "", os.ErrNotExist
}

// --- Add / Count -------------------------------------------------------

func TestSourceRegistry_Add_Dedup(t *testing.T) {
	reg := NewSourceRegistry()
	if err := reg.Add(Source{Name: "a", Kind: KindFile, Path: "a.md"}); err != nil {
		t.Fatalf("first add: %v", err)
	}
	if err := reg.Add(Source{Name: "a", Kind: KindFile, Path: "a2.md"}); err == nil {
		t.Fatal("expected error on duplicate name")
	}
	if reg.Count() != 1 {
		t.Fatalf("expected 1 source, got %d", reg.Count())
	}
}

func TestSourceRegistry_Add_DefaultKindAndScope(t *testing.T) {
	reg := NewSourceRegistry()
	_ = reg.Add(Source{Name: "x", Path: "x.md"})
	all := reg.All()
	if all[0].Source.Kind != KindFile {
		t.Errorf("expected default kind=file, got %s", all[0].Source.Kind)
	}
	if all[0].Source.Scope != ScopeSession {
		t.Errorf("expected default scope=session, got %s", all[0].Source.Scope)
	}
}

// --- Always-on sources -------------------------------------------------

func TestEvaluate_AlwaysOn(t *testing.T) {
	reg := NewSourceRegistry()
	_ = reg.Add(Source{Name: "always", Path: "always.md"})

	if err := reg.Evaluate(nil, ".", noopLoader, 1); err != nil {
		t.Fatalf("evaluate: %v", err)
	}

	active := reg.Active()
	if len(active) != 1 {
		t.Fatalf("expected 1 active source, got %d", len(active))
	}
	if active[0].Reason != "always-on" {
		t.Errorf("expected reason 'always-on', got %q", active[0].Reason)
	}
	if active[0].Content != "content of always" {
		t.Errorf("unexpected content: %q", active[0].Content)
	}
}

// --- Conditional sources -----------------------------------------------

func TestEvaluate_WhenTrue(t *testing.T) {
	reg := NewSourceRegistry()
	_ = reg.Add(Source{
		Name: "pr-rules",
		Path: "pr.md",
		When: `ctx.get("mode") == "pull_request"`,
	})

	values := map[string]interface{}{"mode": "pull_request"}
	if err := reg.Evaluate(values, ".", noopLoader, 1); err != nil {
		t.Fatalf("evaluate: %v", err)
	}

	if len(reg.Active()) != 1 {
		t.Fatal("expected source to be active when condition is true")
	}
}

func TestEvaluate_WhenFalse(t *testing.T) {
	reg := NewSourceRegistry()
	_ = reg.Add(Source{
		Name: "pr-rules",
		Path: "pr.md",
		When: `ctx.get("mode") == "pull_request"`,
	})

	values := map[string]interface{}{"mode": "chat"}
	if err := reg.Evaluate(values, ".", noopLoader, 1); err != nil {
		t.Fatalf("evaluate: %v", err)
	}

	active := reg.Active()
	if len(active) != 0 {
		t.Fatal("expected source to be inactive when condition is false")
	}
	all := reg.All()
	if !strings.Contains(all[0].Reason, "condition false") {
		t.Errorf("expected 'condition false' in reason, got %q", all[0].Reason)
	}
}

func TestEvaluate_WhenConditionToggle(t *testing.T) {
	reg := NewSourceRegistry()
	_ = reg.Add(Source{
		Name: "toggle",
		Path: "t.md",
		When: `ctx.get("flag", False)`,
	})

	// Turn 1: inactive
	if err := reg.Evaluate(map[string]interface{}{"flag": false}, ".", noopLoader, 1); err != nil {
		t.Fatalf("turn 1: %v", err)
	}
	if len(reg.Active()) != 0 {
		t.Fatal("expected inactive at turn 1")
	}

	// Turn 2: active
	if err := reg.Evaluate(map[string]interface{}{"flag": true}, ".", noopLoader, 2); err != nil {
		t.Fatalf("turn 2: %v", err)
	}
	if len(reg.Active()) != 1 {
		t.Fatal("expected active at turn 2")
	}
}

// --- Condition evaluation errors ---------------------------------------

func TestEvaluate_WhenConditionError(t *testing.T) {
	reg := NewSourceRegistry()
	_ = reg.Add(Source{
		Name: "bad-cond",
		Path: "x.md",
		When: `1 // 0`, // integer floor-divide-by-zero
	})

	if err := reg.Evaluate(nil, ".", noopLoader, 1); err != nil {
		t.Fatalf("evaluate returned error (should be non-fatal): %v", err)
	}

	active := reg.Active()
	if len(active) != 0 {
		t.Fatal("expected source inactive after condition error")
	}
	all := reg.All()
	if !strings.Contains(all[0].Reason, "condition error") {
		t.Errorf("expected 'condition error' in reason, got %q", all[0].Reason)
	}
}

// --- Trigger-based sources --------------------------------------------

func TestActivateTrigger(t *testing.T) {
	reg := NewSourceRegistry()
	_ = reg.Add(Source{
		Name:    "error-recovery",
		Path:    "err.md",
		Trigger: "error",
	})

	// Evaluate without trigger: source should remain inactive.
	if err := reg.Evaluate(nil, ".", noopLoader, 1); err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	if len(reg.Active()) != 0 {
		t.Fatal("trigger source should not activate through per-turn evaluate")
	}

	// Fire the trigger.
	if err := reg.ActivateTrigger("error", noopLoader, 1); err != nil {
		t.Fatalf("activate trigger: %v", err)
	}
	active := reg.Active()
	if len(active) != 1 {
		t.Fatalf("expected 1 active source after trigger, got %d", len(active))
	}
	if !strings.Contains(active[0].Reason, "trigger:") {
		t.Errorf("expected trigger reason, got %q", active[0].Reason)
	}
}

func TestActivateTrigger_UnknownEventNoOp(t *testing.T) {
	reg := NewSourceRegistry()
	_ = reg.Add(Source{Name: "s", Path: "s.md", Trigger: "error"})

	if err := reg.ActivateTrigger("something-else", noopLoader, 1); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(reg.Active()) != 0 {
		t.Fatal("unexpected activation for non-matching trigger")
	}
}

// --- TTL expiry -------------------------------------------------------

func TestEvaluate_TTLExpiry(t *testing.T) {
	reg := NewSourceRegistry()
	_ = reg.Add(Source{
		Name: "ttl-source",
		Path: "ttl.md",
		TTL:  2, // active for 2 turns
	})

	// Turn 1: activate
	if err := reg.Evaluate(nil, ".", noopLoader, 1); err != nil {
		t.Fatalf("turn 1: %v", err)
	}
	if len(reg.Active()) != 1 {
		t.Fatal("expected active at turn 1")
	}

	// Force activatedAt to be 1 (Evaluate sets it to 0→1 on first activation)
	// and re-evaluate at turn 3 (= activatedAt + TTL).
	if err := reg.Evaluate(nil, ".", noopLoader, 3); err != nil {
		t.Fatalf("turn 3: %v", err)
	}
	if len(reg.Active()) != 0 {
		t.Fatal("expected TTL expiry at turn 3")
	}
	all := reg.All()
	if !strings.Contains(all[0].Reason, "TTL expired") {
		t.Errorf("expected TTL expiry reason, got %q", all[0].Reason)
	}
}

// --- Priority ordering ------------------------------------------------

func TestActive_PriorityOrder(t *testing.T) {
	reg := NewSourceRegistry()
	_ = reg.Add(Source{Name: "high", Path: "h.md", Priority: 100})
	_ = reg.Add(Source{Name: "low", Path: "l.md", Priority: 10})
	_ = reg.Add(Source{Name: "mid", Path: "m.md", Priority: 50})

	if err := reg.Evaluate(nil, ".", noopLoader, 1); err != nil {
		t.Fatalf("evaluate: %v", err)
	}

	active := reg.Active()
	if len(active) != 3 {
		t.Fatalf("expected 3 active sources, got %d", len(active))
	}

	names := make([]string, len(active))
	for i, e := range active {
		names[i] = e.Source.Name
	}
	expected := []string{"low", "mid", "high"}
	for i, want := range expected {
		if names[i] != want {
			t.Errorf("position %d: want %q, got %q", i, want, names[i])
		}
	}
}

// --- Load error handling ----------------------------------------------

func TestEvaluate_LoadError(t *testing.T) {
	reg := NewSourceRegistry()
	_ = reg.Add(Source{Name: "missing", Path: "nowhere.md"})

	if err := reg.Evaluate(nil, ".", errorLoader, 1); err != nil {
		t.Fatalf("evaluate should be non-fatal, got: %v", err)
	}
	if len(reg.Active()) != 0 {
		t.Fatal("source with load error should be inactive")
	}
	all := reg.All()
	if !strings.Contains(all[0].Reason, "load error") {
		t.Errorf("expected 'load error' in reason, got %q", all[0].Reason)
	}
}

// --- Session-scoped caching -------------------------------------------

func TestEvaluate_SessionScopeCached(t *testing.T) {
	calls := 0
	countingLoader := func(s Source) (string, error) {
		calls++
		return "cached-content", nil
	}

	reg := NewSourceRegistry()
	_ = reg.Add(Source{Name: "sess", Path: "s.md", Scope: ScopeSession})

	_ = reg.Evaluate(nil, ".", countingLoader, 1)
	_ = reg.Evaluate(nil, ".", countingLoader, 2)
	_ = reg.Evaluate(nil, ".", countingLoader, 3)

	if calls != 1 {
		t.Errorf("session-scoped source should be loaded once; got %d calls", calls)
	}
}

func TestEvaluate_TurnScopeReloads(t *testing.T) {
	calls := 0
	countingLoader := func(s Source) (string, error) {
		calls++
		return "fresh-content", nil
	}

	reg := NewSourceRegistry()
	_ = reg.Add(Source{Name: "turn-src", Path: "t.md", Scope: ScopeTurn})

	_ = reg.Evaluate(nil, ".", countingLoader, 1)
	_ = reg.Evaluate(nil, ".", countingLoader, 2)
	_ = reg.Evaluate(nil, ".", countingLoader, 3)

	if calls != 3 {
		t.Errorf("turn-scoped source should reload every turn; got %d calls", calls)
	}
}

// --- File loader ------------------------------------------------------

func TestLoadContent_File(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "rules.md")
	if err := os.WriteFile(path, []byte("# Rules\n\nDo good."), 0o644); err != nil {
		t.Fatal(err)
	}

	content, err := LoadContent(Source{Name: "rules", Kind: KindFile, Path: path}, dir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if !strings.Contains(content, "Do good") {
		t.Errorf("unexpected content: %q", content)
	}
}

func TestLoadContent_FileRelative(t *testing.T) {
	dir := t.TempDir()
	subdir := filepath.Join(dir, ".harness", "context")
	if err := os.MkdirAll(subdir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(subdir, "pr.md"), []byte("PR rules"), 0o644); err != nil {
		t.Fatal(err)
	}

	content, err := LoadContent(Source{
		Name: "pr",
		Kind: KindFile,
		Path: ".harness/context/pr.md",
	}, dir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if content != "PR rules" {
		t.Errorf("unexpected content: %q", content)
	}
}

func TestLoadContent_FileMissing(t *testing.T) {
	_, err := LoadContent(Source{Name: "x", Kind: KindFile, Path: "does-not-exist.md"}, ".")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

// --- Injector ---------------------------------------------------------

func TestBuildInjection_Empty(t *testing.T) {
	reg := NewSourceRegistry()
	result := BuildInjection(reg)
	if result.Content != "" {
		t.Errorf("expected empty injection, got %q", result.Content)
	}
	if len(result.Sources) != 0 {
		t.Errorf("expected no sources, got %d", len(result.Sources))
	}
}

func TestBuildInjection_MultipleActive(t *testing.T) {
	reg := NewSourceRegistry()
	_ = reg.Add(Source{Name: "first", Path: "f.md", Priority: 1})
	_ = reg.Add(Source{Name: "second", Path: "s.md", Priority: 2})
	_ = reg.Evaluate(nil, ".", noopLoader, 1)

	result := BuildInjection(reg)
	if !strings.Contains(result.Content, "content of first") {
		t.Errorf("expected first source content, got: %q", result.Content)
	}
	if !strings.Contains(result.Content, "content of second") {
		t.Errorf("expected second source content, got: %q", result.Content)
	}
	// First should appear before second
	posFirst := strings.Index(result.Content, "content of first")
	posSecond := strings.Index(result.Content, "content of second")
	if posFirst >= posSecond {
		t.Error("expected lower-priority source first in output")
	}
}

func TestInjectIntoPrompt_Empty(t *testing.T) {
	result := InjectIntoPrompt("You are helpful.", InjectionResult{})
	if result != "You are helpful." {
		t.Errorf("prompt should be unchanged, got %q", result)
	}
}

func TestInjectIntoPrompt_WithContent(t *testing.T) {
	result := InjectIntoPrompt("Base prompt.", InjectionResult{Content: "Injected context."})
	if !strings.HasPrefix(result, "Injected context.") {
		t.Errorf("injected content should be prepended, got: %q", result)
	}
	if !strings.Contains(result, "Base prompt.") {
		t.Errorf("base prompt should be preserved, got: %q", result)
	}
}

// --- SourcesFromDefs --------------------------------------------------

func TestSourcesFromDefs(t *testing.T) {
	defs := []ContextSourceDef{
		{Name: "a", Type: "file", Path: "a.md"},
		{Name: "b", Type: "file", Path: "b.md", When: `ctx.get("x") == "y"`},
	}
	reg, err := SourcesFromDefs(defs)
	if err != nil {
		t.Fatalf("SourcesFromDefs: %v", err)
	}
	if reg.Count() != 2 {
		t.Fatalf("expected 2 sources, got %d", reg.Count())
	}
}

func TestSourcesFromDefs_DuplicateError(t *testing.T) {
	defs := []ContextSourceDef{
		{Name: "dup", Type: "file", Path: "a.md"},
		{Name: "dup", Type: "file", Path: "b.md"},
	}
	_, err := SourcesFromDefs(defs)
	if err == nil {
		t.Fatal("expected error for duplicate source names")
	}
}
