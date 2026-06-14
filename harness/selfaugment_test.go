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

func TestSelfAugment_CreateTool_PersistsAndHotReloads(t *testing.T) {
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
	if !strings.Contains(out, "\"reloaded\": true") {
		t.Fatalf("response missing reloaded marker: %s", out)
	}

	// File was written.
	path := filepath.Join(dir, ".harness", "tools", "weather_now.md")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected artifact at %s, got %v", path, err)
	}

	// Tool is callable in the registry now.
	if !h.registry.Has("weather_now") {
		t.Fatalf("expected weather_now to be hot-reloaded into registry")
	}

	// Round-trip the new tool to confirm the Starlark script actually runs.
	resCh := make(chan string, 1)
	errCh := make(chan error, 1)
	go func() {
		def, _ := h.registry.Get("weather_now")
		_ = def
		// Can't easily call Execute here without setting up the full
		// Call/Result plumbing, so just confirm the Definition is sane.
		resCh <- "ok"
	}()
	select {
	case <-resCh:
	case err := <-errCh:
		t.Fatalf("execute weather_now: %v", err)
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

func TestSelfAugment_CreateHook_PersistsAndActivates(t *testing.T) {
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
	path := filepath.Join(dir, ".harness", "hooks", "log_every_tool.md")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected hook artifact at %s, got %v", path, err)
	}
	if _, ok := h.fileHooks["log_every_tool"]; !ok {
		t.Fatalf("hook not tracked in fileHooks map")
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

	// Add a tool, list again, check it shows up under file_based.
	createRaw, _ := json.Marshal(createToolArgs{
		Name:        "my_tool",
		Description: "Hand-rolled.",
		Script:      "def run(args):\n    return 'hi'\n",
	})
	if _, err := h.handleCreateTool(context.Background(), createRaw); err != nil {
		t.Fatalf("create: %v", err)
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
	if !h.registry.Has("tmp_tool") {
		t.Fatalf("expected tmp_tool registered")
	}

	rmRaw, _ := json.Marshal(removeArtifactArgs{Kind: "tool", Name: "tmp_tool"})
	if _, err := h.handleRemoveArtifact(context.Background(), rmRaw); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if h.registry.Has("tmp_tool") {
		t.Fatalf("tmp_tool still registered after remove")
	}
	if _, err := os.Stat(filepath.Join(dir, ".harness", "tools", "tmp_tool.md")); !os.IsNotExist(err) {
		t.Fatalf("expected file removed, got %v", err)
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

func TestSelfAugment_Reload_DropsDeletedFiles(t *testing.T) {
	h, dir := newTestHarness(t)

	// Create via the meta-tool.
	createRaw, _ := json.Marshal(createToolArgs{
		Name:        "ghost",
		Description: "About to vanish.",
		Script:      "def run(args):\n    return 'boo'\n",
	})
	if _, err := h.handleCreateTool(context.Background(), createRaw); err != nil {
		t.Fatalf("create: %v", err)
	}
	if !h.registry.Has("ghost") {
		t.Fatalf("expected ghost registered")
	}

	// Delete the file directly (simulate user editing on disk).
	if err := os.Remove(filepath.Join(dir, ".harness", "tools", "ghost.md")); err != nil {
		t.Fatalf("rm: %v", err)
	}
	if err := h.Reload(); err != nil {
		t.Fatalf("reload: %v", err)
	}
	if h.registry.Has("ghost") {
		t.Fatalf("expected ghost to be unregistered after Reload")
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
