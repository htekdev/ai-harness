package context

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/htekdev/ai-harness/config"
)

// ── Registry tests ─────────────────────────────────────────────────────────

func TestRegistry_Active_NoWhen(t *testing.T) {
	reg := NewRegistry()
	for _, src := range []ContextSource{
		{Name: "a", Type: "file", Path: "a.md"},
		{Name: "b", Type: "file", Path: "b.md"},
	} {
		if err := reg.Register(src); err != nil {
			t.Fatalf("Register: %v", err)
		}
	}

	active, err := reg.Active(nil)
	if err != nil {
		t.Fatalf("Active: %v", err)
	}
	if len(active) != 2 {
		t.Fatalf("expected 2 active sources, got %d", len(active))
	}
}

func TestRegistry_Active_WithWhen(t *testing.T) {
	reg := NewRegistry()
	_ = reg.Register(ContextSource{
		Name: "pr",
		Type: "file",
		Path: "pr.md",
		When: `ctx.get("mode") == "pull_request"`,
	})
	_ = reg.Register(ContextSource{Name: "base", Type: "file", Path: "base.md"})

	// No turn state — only "base" should be active.
	active, err := reg.Active(nil)
	if err != nil {
		t.Fatalf("Active(nil): %v", err)
	}
	if len(active) != 1 || active[0].Name != "base" {
		t.Fatalf("expected only 'base' active without turn state, got %v", active)
	}

	// With mode=pull_request — both should be active.
	active, err = reg.Active(map[string]interface{}{"mode": "pull_request"})
	if err != nil {
		t.Fatalf("Active(pr): %v", err)
	}
	if len(active) != 2 {
		t.Fatalf("expected 2 active sources with mode=pull_request, got %d", len(active))
	}
}

func TestRegistry_Active_Priority(t *testing.T) {
	reg := NewRegistry()
	_ = reg.Register(ContextSource{Name: "z", Priority: 10})
	_ = reg.Register(ContextSource{Name: "a", Priority: 1})
	_ = reg.Register(ContextSource{Name: "m", Priority: 5})

	active, err := reg.Active(nil)
	if err != nil {
		t.Fatalf("Active: %v", err)
	}
	if len(active) != 3 {
		t.Fatalf("expected 3 sources, got %d", len(active))
	}
	if active[0].Name != "a" || active[1].Name != "m" || active[2].Name != "z" {
		t.Fatalf("wrong priority order: got %v %v %v", active[0].Name, active[1].Name, active[2].Name)
	}
}

func TestRegistry_Register_EmptyName(t *testing.T) {
	reg := NewRegistry()
	if err := reg.Register(ContextSource{Name: ""}); err == nil {
		t.Fatal("expected error for empty name, got nil")
	}
}

func TestRegistry_LoadFromConfig(t *testing.T) {
	reg := NewRegistry()
	cfgSources := []config.ContextSourceConfig{
		{Name: "pr-workflow", Type: "file", Path: ".harness/pr.md", When: `ctx.get("mode") == "pull_request"`},
		{Name: "base", Type: "file", Path: ".harness/base.md"},
	}
	if err := reg.LoadFromConfig(cfgSources); err != nil {
		t.Fatalf("LoadFromConfig: %v", err)
	}
	if reg.Count() != 2 {
		t.Fatalf("expected 2 sources, got %d", reg.Count())
	}
}

func TestRegistry_All_Ordered(t *testing.T) {
	reg := NewRegistry()
	_ = reg.Register(ContextSource{Name: "high", Priority: 100})
	_ = reg.Register(ContextSource{Name: "low", Priority: 1})
	_ = reg.Register(ContextSource{Name: "mid", Priority: 50})

	all := reg.All()
	if len(all) != 3 {
		t.Fatalf("expected 3, got %d", len(all))
	}
	if all[0].Name != "low" || all[1].Name != "mid" || all[2].Name != "high" {
		t.Fatalf("unexpected order: %v %v %v", all[0].Name, all[1].Name, all[2].Name)
	}
}

// ── Loader tests ────────────────────────────────────────────────────────────

func TestLoader_LoadContent_File(t *testing.T) {
	dir := t.TempDir()
	want := "# PR Rules\n\nFollow these rules."
	if err := os.WriteFile(filepath.Join(dir, "pr.md"), []byte(want), 0o644); err != nil {
		t.Fatal(err)
	}

	loader := NewLoader()
	got, err := loader.LoadContent(ContextSource{Name: "pr", Type: "file", Path: "pr.md"}, dir)
	if err != nil {
		t.Fatalf("LoadContent: %v", err)
	}
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestLoader_LoadContent_FileCache(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cached.md")
	_ = os.WriteFile(path, []byte("cached content"), 0o644)

	loader := NewLoader()
	src := ContextSource{Name: "x", Type: "file", Path: "cached.md"}
	_, _ = loader.LoadContent(src, dir) // first load — caches

	// Modify file on disk; cache should still return original.
	_ = os.WriteFile(path, []byte("CHANGED"), 0o644)

	got, err := loader.LoadContent(src, dir)
	if err != nil {
		t.Fatalf("LoadContent: %v", err)
	}
	if got != "cached content" {
		t.Fatalf("expected cached content, got %q", got)
	}
}

func TestLoader_LoadContent_MissingFile(t *testing.T) {
	loader := NewLoader()
	_, err := loader.LoadContent(ContextSource{Name: "x", Type: "file", Path: "no-such-file.md"}, t.TempDir())
	if err == nil {
		t.Fatal("expected error for missing file, got nil")
	}
}

// ── Injector tests ──────────────────────────────────────────────────────────

func TestInjector_Inject_Empty(t *testing.T) {
	inj := NewInjector(NewLoader())
	got, err := inj.Inject(nil, "system prompt", "/root")
	if err != nil {
		t.Fatalf("Inject: %v", err)
	}
	if got != "system prompt" {
		t.Fatalf("expected unchanged prompt, got %q", got)
	}
}

func TestInjector_Inject_Single(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "rules.md"), []byte("## Rules\nBe helpful."), 0o644)

	inj := NewInjector(NewLoader())
	src := ContextSource{Name: "rules", Type: "file", Path: "rules.md"}
	got, err := inj.Inject([]ContextSource{src}, "original prompt", dir)
	if err != nil {
		t.Fatalf("Inject: %v", err)
	}
	if !strings.Contains(got, "<!-- context: rules -->") {
		t.Fatalf("missing observability comment in: %q", got)
	}
	if !strings.Contains(got, "## Rules") {
		t.Fatalf("missing file content in: %q", got)
	}
	if !strings.HasSuffix(got, "original prompt") {
		t.Fatalf("original prompt should be at end, got: %q", got)
	}
}

func TestInjector_Inject_MultiPriority(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "first.md"), []byte("FIRST"), 0o644)
	_ = os.WriteFile(filepath.Join(dir, "second.md"), []byte("SECOND"), 0o644)

	inj := NewInjector(NewLoader())
	// Registry sorts ascending by priority, so priority=1 (first) comes before priority=10 (second).
	sources := []ContextSource{
		{Name: "first", Type: "file", Path: "first.md", Priority: 1},
		{Name: "second", Type: "file", Path: "second.md", Priority: 10},
	}
	got, err := inj.Inject(sources, "base", dir)
	if err != nil {
		t.Fatalf("Inject: %v", err)
	}
	firstPos := strings.Index(got, "FIRST")
	secondPos := strings.Index(got, "SECOND")
	if firstPos == -1 || secondPos == -1 {
		t.Fatalf("missing content in result: %q", got)
	}
	// Lower priority (first=1) injected first → FIRST appears before SECOND.
	if firstPos > secondPos {
		t.Fatalf("expected FIRST before SECOND: firstPos=%d secondPos=%d", firstPos, secondPos)
	}
}

// ── Integration test ────────────────────────────────────────────────────────

func TestIntegration_ConfigToInject(t *testing.T) {
	// Set up temp dir with context files.
	dir := t.TempDir()
	ctxDir := filepath.Join(dir, ".harness", "context")
	if err := os.MkdirAll(ctxDir, 0o755); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(ctxDir, "base.md"), []byte("## Base Context\nThis is always loaded."), 0o644)
	_ = os.WriteFile(filepath.Join(ctxDir, "pr-rules.md"), []byte("## PR Rules\nReview carefully."), 0o644)

	// Load from config.
	reg := NewRegistry()
	cfgSources := []config.ContextSourceConfig{
		{Name: "base", Type: "file", Path: ".harness/context/base.md", Priority: 0},
		{
			Name:     "pr-workflow",
			Type:     "file",
			Path:     ".harness/context/pr-rules.md",
			When:     `ctx.get("mode") == "pull_request"`,
			Priority: 10,
		},
	}
	if err := reg.LoadFromConfig(cfgSources); err != nil {
		t.Fatalf("LoadFromConfig: %v", err)
	}

	// Evaluate with no mode set — only "base" should be active.
	active, err := reg.Active(nil)
	if err != nil {
		t.Fatalf("Active(nil): %v", err)
	}
	if len(active) != 1 || active[0].Name != "base" {
		t.Fatalf("expected only 'base', got %v", active)
	}

	// Inject.
	inj := NewInjector(NewLoader())
	result, err := inj.Inject(active, "system: you are helpful", dir)
	if err != nil {
		t.Fatalf("Inject: %v", err)
	}
	if !strings.Contains(result, "<!-- context: base -->") {
		t.Fatalf("missing base comment in %q", result)
	}
	if !strings.Contains(result, "Base Context") {
		t.Fatalf("missing base content in %q", result)
	}
	if !strings.HasSuffix(result, "system: you are helpful") {
		t.Fatalf("system prompt not at end: %q", result)
	}

	// Evaluate with mode=pull_request — both should be active.
	active, err = reg.Active(map[string]interface{}{"mode": "pull_request"})
	if err != nil {
		t.Fatalf("Active(pr): %v", err)
	}
	if len(active) != 2 {
		t.Fatalf("expected 2 active sources, got %d", len(active))
	}

	// Inject both.
	result, err = inj.Inject(active, "base prompt", dir)
	if err != nil {
		t.Fatalf("Inject(pr): %v", err)
	}
	if !strings.Contains(result, "<!-- context: base -->") {
		t.Fatalf("missing base comment in pr result: %q", result)
	}
	if !strings.Contains(result, "<!-- context: pr-workflow -->") {
		t.Fatalf("missing pr-workflow comment in pr result: %q", result)
	}
	if !strings.Contains(result, "PR Rules") {
		t.Fatalf("missing PR rules in %q", result)
	}
}
