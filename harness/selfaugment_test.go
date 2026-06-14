package harness

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// newTestHarness builds a Harness rooted at a temp dir, with the
// minimum config needed for the self-augment tools to run. It does
// NOT require a real API key — the agent loop never starts.
func newTestHarness(t *testing.T) (*Harness, string) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("TEST_KEY", "x")

	configMD := `---
model:
  name: gpt-4o-mini
  provider: openai
  api_key_env: TEST_KEY
  max_tokens: 1024
context:
  max_history: 10
  max_tokens: 8000
---

# Test harness

Plain test harness for the self-augment suite.
`
	configPath := filepath.Join(dir, "harness.md")
	if err := os.WriteFile(configPath, []byte(configMD), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	h, err := New(configPath)
	if err != nil {
		t.Fatalf("New harness: %v", err)
	}
	return h, dir
}

func TestSelfAugment_BuiltinsRegistered(t *testing.T) {
	h, _ := newTestHarness(t)

	for _, name := range []string{
		"harness_create_tool",
		"harness_create_hook",
		"harness_list_artifacts",
		"harness_remove_artifact",
	} {
		if !h.registry.Has(name) {
			t.Errorf("expected built-in %q to be registered", name)
		}
	}
}

func TestSelfAugment_CreateTool_PersistsAndIsCallableNextTurn(t *testing.T) {
	h, dir := newTestHarness(t)

	args := createToolArgs{
		Name:        "weather_now",
		Description: "Return a fake weather string for any city. Test fixture only.",
		ParametersJSON: `{
			"city": {"type": "string", "description": "City name", "required": true}
		}`,
		Script: "def run(args):\n    return \"sunny 75F in \" + args[\"city\"]\n",
	}
	raw, _ := json.Marshal(args)
	out, err := h.handleCreateTool(context.Background(), raw)
	if err != nil {
		t.Fatalf("create_tool: %v", err)
	}
	if !strings.Contains(out, "\"created\": \"weather_now\"") {
		t.Fatalf("response missing created marker: %s", out)
	}
	// New API: response advertises that the tool will be callable on
	// the next turn (no implicit reload inside the handler).
	if !strings.Contains(out, "\"next_turn\": true") {
		t.Fatalf("response missing next_turn marker (per-turn discovery): %s", out)
	}

	// The file must already be on disk — write happens synchronously.
	path := filepath.Join(dir, ".harness", "tools", "weather_now.md")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected artifact at %s, got %v", path, err)
	}

	// Crucially: the tool is NOT yet in the registry. Per-turn
	// discovery only triggers between turns (via agent.OnTurnStart).
	if h.registry.Has("weather_now") {
		t.Fatalf("per-turn discovery should NOT register the tool until the next turn; got it in registry already")
	}

	// Simulate the next turn arriving — onTurnStart fires.
	if err := h.onTurnStart(context.Background()); err != nil {
		t.Fatalf("onTurnStart: %v", err)
	}
	if !h.registry.Has("weather_now") {
		t.Fatalf("after the next turn fires onTurnStart, weather_now must be in the registry")
	}
}

func TestSelfAugment_CreateTool_RejectsBadInputs(t *testing.T) {
	h, _ := newTestHarness(t)

	cases := []struct {
		name string
		args createToolArgs
		want string
	}{
		{"bad-name-uppercase", createToolArgs{Name: "BadName", Description: "d", Script: "def run(args):\n    return ''\n"}, "invalid tool name"},
		{"bad-name-slash", createToolArgs{Name: "../etc", Description: "d", Script: "def run(args):\n    return ''\n"}, "invalid tool name"},
		{"reserved", createToolArgs{Name: "harness_create_tool", Description: "d", Script: "def run(args):\n    return ''\n"}, "reserved"},
		{"empty-desc", createToolArgs{Name: "ok_tool", Description: "  ", Script: "def run(args):\n    return ''\n"}, "description"},
		{"missing-run", createToolArgs{Name: "ok_tool2", Description: "d", Script: "x = 1\n"}, "def run"},
		{"bad-params-json", createToolArgs{Name: "ok_tool3", Description: "d", ParametersJSON: "not json", Script: "def run(args):\n    return ''\n"}, "parameters_json"},
		// The bug Hector hit: model wrote Python `with open(...)` instead
		// of Starlark `fs.read/write`. Compile-check must catch it before
		// the artifact ever lands on disk.
		{"python-with", createToolArgs{Name: "py_with", Description: "d", Script: "def run(args):\n    with open(args['p']) as f:\n        return f.read()\n"}, "does not compile"},
		{"python-fstring", createToolArgs{Name: "py_fstr", Description: "d", Script: "def run(args):\n    return f'hi {args[\"name\"]}'\n"}, "does not compile"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			raw, _ := json.Marshal(tc.args)
			_, err := h.handleCreateTool(context.Background(), raw)
			if err == nil {
				t.Fatalf("expected error for %s", tc.name)
			}
			if !strings.Contains(strings.ToLower(err.Error()), strings.ToLower(tc.want)) {
				t.Errorf("error %q missing substring %q", err.Error(), tc.want)
			}
		})
	}
}

// TestSelfAugment_CreateTool_BrokenScriptNotWritten verifies that a
// failed compile-check does NOT leave a broken artifact on disk. This
// is the regression that bricked the live Telegram bot — every restart
// re-loaded the broken script and crashed at startup.
func TestSelfAugment_CreateTool_BrokenScriptNotWritten(t *testing.T) {
	h, dir := newTestHarness(t)

	raw, _ := json.Marshal(createToolArgs{
		Name:        "broken",
		Description: "uses Python idioms",
		Script:      "def run(args):\n    with open('x') as f:\n        return f.read()\n",
	})
	_, err := h.handleCreateTool(context.Background(), raw)
	if err == nil {
		t.Fatalf("expected compile error, got nil")
	}
	path := filepath.Join(dir, ".harness", "tools", "broken.md")
	if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
		t.Fatalf("broken artifact must NOT exist on disk after compile-check failure, but stat says: %v", statErr)
	}
}

func TestSelfAugment_CreateHook_PersistsAndActivatesNextTurn(t *testing.T) {
	h, dir := newTestHarness(t)

	args := createHookArgs{
		Name:   "log_every_tool",
		Event:  "tool.pre",
		Script: "def handle(event):\n    log(\"tool: \" + str(event))\n    return allow()\n",
	}
	raw, _ := json.Marshal(args)
	out, err := h.handleCreateHook(context.Background(), raw)
	if err != nil {
		t.Fatalf("create_hook: %v", err)
	}
	if !strings.Contains(out, "\"created\": \"log_every_tool\"") {
		t.Fatalf("response missing created marker: %s", out)
	}
	if !strings.Contains(out, "\"next_turn\": true") {
		t.Fatalf("response missing next_turn marker: %s", out)
	}
	path := filepath.Join(dir, ".harness", "hooks", "log_every_tool.md")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected hook artifact at %s, got %v", path, err)
	}
	// Before next-turn discovery, fileHooks is not yet populated.
	if _, ok := h.fileHooks["log_every_tool"]; ok {
		t.Fatalf("hook should not be tracked in fileHooks until next turn")
	}
	if err := h.onTurnStart(context.Background()); err != nil {
		t.Fatalf("onTurnStart: %v", err)
	}
	if _, ok := h.fileHooks["log_every_tool"]; !ok {
		t.Fatalf("hook not tracked in fileHooks after next-turn discovery")
	}
}

func TestSelfAugment_CreateHook_RejectsInvalidEvent(t *testing.T) {
	h, _ := newTestHarness(t)
	raw, _ := json.Marshal(createHookArgs{
		Name:   "bad",
		Event:  "not.a.real.event",
		Script: "def handle(event):\n    return allow()\n",
	})
	_, err := h.handleCreateHook(context.Background(), raw)
	if err == nil || !strings.Contains(err.Error(), "invalid event") {
		t.Fatalf("expected invalid-event error, got %v", err)
	}
}

func TestSelfAugment_ListArtifacts(t *testing.T) {
	h, _ := newTestHarness(t)

	out, err := h.handleListArtifacts(context.Background(), nil)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	// Must mention all four built-in meta-tools.
	for _, name := range builtinToolNames {
		if !strings.Contains(out, name) {
			t.Errorf("listing missing built-in %q: %s", name, out)
		}
	}

	// Add a tool, fire next-turn discovery, then list — it should show up under file_based.
	createRaw, _ := json.Marshal(createToolArgs{
		Name:        "my_tool",
		Description: "Hand-rolled.",
		Script:      "def run(args):\n    return 'hi'\n",
	})
	if _, err := h.handleCreateTool(context.Background(), createRaw); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := h.onTurnStart(context.Background()); err != nil {
		t.Fatalf("onTurnStart: %v", err)
	}
	out2, err := h.handleListArtifacts(context.Background(), json.RawMessage(`{"kind":"tools"}`))
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if !strings.Contains(out2, "my_tool") {
		t.Fatalf("file-based tool missing from listing: %s", out2)
	}
}

func TestSelfAugment_RemoveArtifact(t *testing.T) {
	h, dir := newTestHarness(t)

	createRaw, _ := json.Marshal(createToolArgs{
		Name:        "tmp_tool",
		Description: "Throwaway.",
		Script:      "def run(args):\n    return 'x'\n",
	})
	if _, err := h.handleCreateTool(context.Background(), createRaw); err != nil {
		t.Fatalf("create: %v", err)
	}
	// Simulate the next turn so the new tool is registered.
	if err := h.onTurnStart(context.Background()); err != nil {
		t.Fatalf("onTurnStart: %v", err)
	}
	if !h.registry.Has("tmp_tool") {
		t.Fatalf("expected tmp_tool registered after onTurnStart")
	}

	rmRaw, _ := json.Marshal(removeArtifactArgs{Kind: "tool", Name: "tmp_tool"})
	if _, err := h.handleRemoveArtifact(context.Background(), rmRaw); err != nil {
		t.Fatalf("remove: %v", err)
	}
	// File is gone immediately; registry is updated by the next turn.
	if _, err := os.Stat(filepath.Join(dir, ".harness", "tools", "tmp_tool.md")); !os.IsNotExist(err) {
		t.Fatalf("expected file removed, got %v", err)
	}
	if err := h.onTurnStart(context.Background()); err != nil {
		t.Fatalf("onTurnStart: %v", err)
	}
	if h.registry.Has("tmp_tool") {
		t.Fatalf("tmp_tool still registered after remove + next turn")
	}
}

func TestSelfAugment_RemoveArtifact_RefusesBuiltins(t *testing.T) {
	h, _ := newTestHarness(t)

	for _, n := range []string{"harness_create_tool", "delegate"} {
		raw, _ := json.Marshal(removeArtifactArgs{Kind: "tool", Name: n})
		_, err := h.handleRemoveArtifact(context.Background(), raw)
		if err == nil || !strings.Contains(err.Error(), "built-in") {
			t.Errorf("expected built-in error for %q, got %v", n, err)
		}
	}
}

func TestSelfAugment_PerTurnDiscovery_DropsDeletedFiles(t *testing.T) {
	h, dir := newTestHarness(t)

	// Create via the meta-tool, then fire next-turn so it lands in registry.
	createRaw, _ := json.Marshal(createToolArgs{
		Name:        "ghost",
		Description: "About to vanish.",
		Script:      "def run(args):\n    return 'boo'\n",
	})
	if _, err := h.handleCreateTool(context.Background(), createRaw); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := h.onTurnStart(context.Background()); err != nil {
		t.Fatalf("onTurnStart: %v", err)
	}
	if !h.registry.Has("ghost") {
		t.Fatalf("expected ghost registered after first onTurnStart")
	}

	// Delete the file directly (simulate user editing on disk).
	if err := os.Remove(filepath.Join(dir, ".harness", "tools", "ghost.md")); err != nil {
		t.Fatalf("rm: %v", err)
	}
	// Next turn arrives — onTurnStart must reconcile and drop the tool.
	if err := h.onTurnStart(context.Background()); err != nil {
		t.Fatalf("onTurnStart after rm: %v", err)
	}
	if h.registry.Has("ghost") {
		t.Fatalf("expected ghost to be unregistered after next-turn discovery")
	}
}

func TestSelfAugment_PerTurnDiscovery_PicksUpFilesDroppedOnDisk(t *testing.T) {
	// Verifies that an artifact written DIRECTLY to disk (no meta-tool
	// call) is picked up on the next turn. This is the headline claim
	// of agent-as-code: files on disk are the source of truth.
	h, dir := newTestHarness(t)

	toolDir := filepath.Join(dir, ".harness", "tools")
	if err := os.MkdirAll(toolDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	md := "---\nparameters: {}\nscript: |\n  def run(args):\n      return 'hand-rolled'\n---\n\n# hand_dropped\n\nDropped onto disk by the user.\n"
	if err := os.WriteFile(filepath.Join(toolDir, "hand_dropped.md"), []byte(md), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	if h.registry.Has("hand_dropped") {
		t.Fatalf("file was on disk but registry was checked before onTurnStart — should NOT be registered yet")
	}
	if err := h.onTurnStart(context.Background()); err != nil {
		t.Fatalf("onTurnStart: %v", err)
	}
	if !h.registry.Has("hand_dropped") {
		t.Fatalf("after onTurnStart, hand-dropped tool must be in registry")
	}
}

func TestSelfAugment_PerTurnDiscovery_ContextArtifactJoinsSystemPrompt(t *testing.T) {
	// .harness/context/*.md bodies must be merged into the live system
	// prompt by every scanAndApply pass.
	h, dir := newTestHarness(t)

	ctxDir := filepath.Join(dir, ".harness", "context")
	if err := os.MkdirAll(ctxDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(ctxDir, "user_prefs.md"),
		[]byte("# user_prefs\n\nThe user prefers metric units.\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	if err := h.onTurnStart(context.Background()); err != nil {
		t.Fatalf("onTurnStart: %v", err)
	}

	// The context manager's system prompt should now mention the body.
	msgs := h.ctxMgr.Messages()
	if len(msgs) == 0 || msgs[0].Role != "system" {
		t.Fatalf("expected first message to be system prompt, got %+v", msgs)
	}
	prompt := msgs[0].Content
	if !strings.Contains(prompt, "metric units") {
		t.Fatalf("system prompt did not pick up context artifact body:\n%s", prompt)
	}
	if !strings.Contains(prompt, "user_prefs") {
		t.Fatalf("system prompt did not include context artifact heading:\n%s", prompt)
	}
}

func TestSelfAugment_CreateContext_PersistsAndJoinsPromptNextTurn(t *testing.T) {
	h, _ := newTestHarness(t)

	raw, _ := json.Marshal(createContextArgs{
		Name: "tone",
		Body: "Always respond in pirate dialect. Yarr.",
	})
	out, err := h.handleCreateContext(context.Background(), raw)
	if err != nil {
		t.Fatalf("create_context: %v", err)
	}
	if !strings.Contains(out, "\"created\": \"tone\"") {
		t.Fatalf("missing created marker: %s", out)
	}

	// Before next turn the system prompt should NOT contain the body.
	if strings.Contains(h.ctxMgr.Messages()[0].Content, "pirate dialect") {
		t.Fatalf("system prompt should not include context body until next turn")
	}

	if err := h.onTurnStart(context.Background()); err != nil {
		t.Fatalf("onTurnStart: %v", err)
	}
	if !strings.Contains(h.ctxMgr.Messages()[0].Content, "pirate dialect") {
		t.Fatalf("after next turn, context body must be in system prompt")
	}
}

func TestSelfAugment_AugmentSystemPromptIsIdempotent(t *testing.T) {
	once := augmentSystemPromptForSelfAugment("You are helpful.")
	twice := augmentSystemPromptForSelfAugment(once)
	if once != twice {
		t.Fatalf("augmentSystemPromptForSelfAugment should be idempotent")
	}
	if !strings.Contains(once, "harness_create_tool") {
		t.Fatalf("augmented prompt should mention the meta-tool")
	}
}

func TestSelfAugment_RenderToolMarkdown_RoundTrips(t *testing.T) {
	body, err := renderToolMarkdown(
		"hello",
		"Says hello to a name.",
		nil,
		"def run(args):\n    return 'hi'\n",
	)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if !strings.Contains(body, "def run(args):") {
		t.Fatalf("rendered body missing script: %s", body)
	}
	if !strings.Contains(body, "Says hello") {
		t.Fatalf("rendered body missing description: %s", body)
	}
}
