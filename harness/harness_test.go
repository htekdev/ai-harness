package harness

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/htekdev/ai-harness/config"
	"github.com/htekdev/ai-harness/hooks"
	"github.com/htekdev/ai-harness/tools"
)

func setTestEnv(t *testing.T, key, value string) {
	t.Helper()
	old, hadOld := os.LookupEnv(key)
	if err := os.Setenv(key, value); err != nil {
		t.Fatalf("set env: %v", err)
	}
	t.Cleanup(func() {
		if hadOld {
			_ = os.Setenv(key, old)
			return
		}
		_ = os.Unsetenv(key)
	})
}

func httptestServer(t *testing.T, responses []string) string {
	t.Helper()
	index := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if index >= len(responses) {
			t.Fatalf("received unexpected request %d", index+1)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(responses[index]))
		index++
	}))
	t.Cleanup(server.Close)
	return server.URL
}

func testConfig() *config.Config {
	return &config.Config{
		Model: config.ModelConfig{
			Provider:    "openai",
			Name:        "gpt-4o-mini",
			MaxTokens:   256,
			Temperature: 0.5,
			BaseURL:     "http://127.0.0.1",
			APIKeyEnv:   "AI_HARNESS_TEST_KEY",
		},
		Context: config.ContextConfig{
			MaxHistory:   10,
			MaxTokens:    1024,
			SystemPrompt: "You are a harness test assistant.",
		},
		Tools: []config.ToolConfig{{
			Name:        "echo",
			Description: "Echo a message",
			Parameters: map[string]config.ParamConfig{
				"message": {Type: "string", Description: "Message to echo", Required: true},
			},
		}},
		Hooks: []config.HookConfig{{Event: string(hooks.EventSessionStart), Handler: "session_hook"}},
	}
}

func scriptedToolConfig(name, script string) config.ToolConfig {
	return config.ToolConfig{
		Name:        name,
		Description: name,
		Script:      script,
	}
}

func TestNewMissingFile(t *testing.T) {
	_, err := New(filepath.Join(t.TempDir(), "missing.yaml"))
	if err == nil {
		t.Fatal("expected error for missing config file")
	}
}

func TestNewFromConfigNil(t *testing.T) {
	_, err := NewFromConfig(nil, nil)
	if err == nil {
		t.Fatal("expected error for nil config")
	}
}

func TestNewFromConfigRequiresAPIKey(t *testing.T) {
	cfg := testConfig()
	old, hadOld := os.LookupEnv(cfg.Model.APIKeyEnv)
	_ = os.Unsetenv(cfg.Model.APIKeyEnv)
	defer func() {
		if hadOld {
			_ = os.Setenv(cfg.Model.APIKeyEnv, old)
			return
		}
		_ = os.Unsetenv(cfg.Model.APIKeyEnv)
	}()

	_, err := NewFromConfig(cfg, nil)
	if err == nil {
		t.Fatal("expected error when API key env var is missing")
	}
}

func TestNewFromConfigRegistersConfigToolsAndHooks(t *testing.T) {
	setTestEnv(t, "AI_HARNESS_TEST_KEY", "secret")
	cfg := testConfig()

	h, err := NewFromConfig(cfg, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if h.Agent() == nil {
		t.Fatal("expected agent to be created")
	}
	if _, ok := h.Agent().Tools().Get("echo"); !ok {
		t.Fatal("expected config tool to be registered")
	}
	handlers := h.Agent().Hooks().HandlersFor(hooks.EventSessionStart)
	if len(handlers) != 1 {
		t.Fatalf("expected 1 configured hook, got %d", len(handlers))
	}
	if result := handlers[0].Handler(context.Background(), hooks.EventSessionStart, nil); result.Action != hooks.ActionContinue {
		t.Fatalf("expected placeholder hook to continue, got %+v", result)
	}
}

func TestHarnessRunWithRegisteredTool(t *testing.T) {
	setTestEnv(t, "AI_HARNESS_TEST_KEY", "secret")
	cfg := testConfig()
	cfg.Model.BaseURL = httptestServer(t, []string{
		`{"choices":[{"message":{"role":"assistant","tool_calls":[{"id":"call_1","type":"function","function":{"name":"echo","arguments":"{\"message\":\"hello\"}"}}]},"finish_reason":"tool_calls"}]}`,
		`{"choices":[{"message":{"role":"assistant","content":"done"},"finish_reason":"stop"}]}`,
	})

	h, err := NewFromConfig(cfg, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	err = h.RegisterTool(tools.Definition{
		Name:        "echo",
		Description: "Echo a message",
		Parameters:  []tools.Parameter{{Name: "message", Type: tools.TypeString, Required: true}},
	}, func(ctx context.Context, args json.RawMessage) (string, error) {
		var payload struct {
			Message string `json:"message"`
		}
		if err := json.Unmarshal(args, &payload); err != nil {
			return "", err
		}
		return payload.Message, nil
	})
	if err != nil {
		t.Fatalf("register tool: %v", err)
	}

	result, err := h.Run(context.Background(), "say hello")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Response != "done" {
		t.Fatalf("unexpected response: %q", result.Response)
	}
	if len(result.ToolResults) != 1 || result.ToolResults[0].Content != "hello" {
		t.Fatalf("unexpected tool results: %+v", result.ToolResults)
	}
}

func TestHarnessRunWithUnimplementedToolPlaceholder(t *testing.T) {
	setTestEnv(t, "AI_HARNESS_TEST_KEY", "secret")
	cfg := testConfig()
	cfg.Model.BaseURL = httptestServer(t, []string{
		`{"choices":[{"message":{"role":"assistant","tool_calls":[{"id":"call_1","type":"function","function":{"name":"echo","arguments":"{\"message\":\"hello\"}"}}]},"finish_reason":"tool_calls"}]}`,
		`{"choices":[{"message":{"role":"assistant","content":"handled"},"finish_reason":"stop"}]}`,
	})

	h, err := NewFromConfig(cfg, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	result, err := h.Run(context.Background(), "say hello")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.ToolResults) != 1 || !result.ToolResults[0].IsError {
		t.Fatalf("expected placeholder tool error, got %+v", result.ToolResults)
	}
}

func TestHarnessConditionalHookFromConfig(t *testing.T) {
	setTestEnv(t, "AI_HARNESS_TEST_KEY", "secret")
	cfg := testConfig()
	cfg.Model.BaseURL = httptestServer(t, []string{
		`{"choices":[{"message":{"role":"assistant","tool_calls":[{"id":"call_1","type":"function","function":{"name":"echo","arguments":"{\"message\":\"hello\"}"}}]},"finish_reason":"tool_calls"}]}`,
		`{"choices":[{"message":{"role":"assistant","content":"done"},"finish_reason":"stop"}]}`,
	})
	cfg.Hooks = []config.HookConfig{{
		Event:   string(hooks.EventToolPre),
		Handler: "rewrite_echo_args",
		When:    `payload.get("name", "") == "echo"`,
		Script: `

def handle(event, payload):
    payload["arguments"] = {"message": "hooked"}
    return modify(payload)
`,
	}}

	h, err := NewFromConfig(cfg, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	err = h.RegisterTool(tools.Definition{
		Name:        "echo",
		Description: "Echo a message",
		Parameters:  []tools.Parameter{{Name: "message", Type: tools.TypeString, Required: true}},
	}, func(ctx context.Context, args json.RawMessage) (string, error) {
		var payload struct {
			Message string `json:"message"`
		}
		if err := json.Unmarshal(args, &payload); err != nil {
			return "", err
		}
		return payload.Message, nil
	})
	if err != nil {
		t.Fatalf("register tool: %v", err)
	}

	result, err := h.Run(context.Background(), "say hello")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.ToolResults) != 1 || result.ToolResults[0].Content != "hooked" {
		t.Fatalf("unexpected tool results: %+v", result.ToolResults)
	}
}

func TestHarnessToolTimeoutFromConfig(t *testing.T) {
	setTestEnv(t, "AI_HARNESS_TEST_KEY", "secret")
	cfg := testConfig()
	cfg.Tools = []config.ToolConfig{scriptedToolConfig("slow", `
def run(args):
    sleep(200)
    return "done"
`)}
	cfg.Tools[0].TimeoutMS = 25
	cfg.Model.BaseURL = httptestServer(t, []string{
		`{"choices":[{"message":{"role":"assistant","tool_calls":[{"id":"call_1","type":"function","function":{"name":"slow","arguments":"{}"}}]},"finish_reason":"tool_calls"}]}`,
		`{"choices":[{"message":{"role":"assistant","content":"handled"},"finish_reason":"stop"}]}`,
	})

	h, err := NewFromConfig(cfg, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	result, err := h.Run(context.Background(), "run slow tool")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.ToolResults) != 1 || !result.ToolResults[0].IsError || !strings.Contains(result.ToolResults[0].Content, context.DeadlineExceeded.Error()) {
		t.Fatalf("expected timeout error result, got %+v", result.ToolResults)
	}
}

func TestHarnessTurnStateSharedBetweenHookAndTool(t *testing.T) {
	setTestEnv(t, "AI_HARNESS_TEST_KEY", "secret")
	cfg := testConfig()
	cfg.Tools = []config.ToolConfig{scriptedToolConfig("stateful", `
def run(args):
    return ctx.get("active_tool", "missing")
`)}
	cfg.Model.BaseURL = httptestServer(t, []string{
		`{"choices":[{"message":{"role":"assistant","tool_calls":[{"id":"call_1","type":"function","function":{"name":"stateful","arguments":"{}"}}]},"finish_reason":"tool_calls"}]}`,
		`{"choices":[{"message":{"role":"assistant","content":"done"},"finish_reason":"stop"}]}`,
	})
	cfg.Hooks = []config.HookConfig{{
		Event:   string(hooks.EventToolPre),
		Handler: "remember_tool",
		Script: `
def handle(event, payload):
    ctx.set("active_tool", payload["name"])
    return allow()
`,
	}}

	h, err := NewFromConfig(cfg, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	result, err := h.Run(context.Background(), "run stateful tool")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.ToolResults) != 1 || result.ToolResults[0].Content != "stateful" {
		t.Fatalf("expected hook state to reach tool, got %+v", result.ToolResults)
	}
}

func TestRegisterHookReplacesConfiguredHook(t *testing.T) {
	setTestEnv(t, "AI_HARNESS_TEST_KEY", "secret")
	cfg := testConfig()

	h, err := NewFromConfig(cfg, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	called := false
	h.RegisterHook(hooks.Registration{
		Name:  "session_hook",
		Event: hooks.EventSessionStart,
		Handler: func(ctx context.Context, event hooks.Event, payload any) hooks.Result {
			called = true
			return hooks.Result{Action: hooks.ActionContinue}
		},
	})

	if err := h.RunSession(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !called {
		t.Fatal("expected registered hook to run")
	}

	h.EndSession(context.Background())
}

func TestNewLoadsFromFile(t *testing.T) {
	setTestEnv(t, "AI_HARNESS_TEST_KEY", "secret")
	dir := t.TempDir()
	path := filepath.Join(dir, "harness.yaml")
	data := []byte(`
model:
  provider: openai
  name: gpt-4o-mini
  max_tokens: 128
  temperature: 0.3
  base_url: http://127.0.0.1
  api_key_env: AI_HARNESS_TEST_KEY
context:
  system_prompt: test
`)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	h, err := New(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if h.Agent() == nil {
		t.Fatal("expected agent")
	}
}
