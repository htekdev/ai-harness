package scripting

import (
	"context"
	"encoding/json"
	"os"
	"regexp"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/htekdev/ai-harness/hooks"
)

func TestEngine_OSBuiltins(t *testing.T) {
	engine := NewEngine()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	hostname, err := os.Hostname()
	if err != nil {
		t.Fatalf("hostname: %v", err)
	}

	runner, err := engine.CompileToolScript("os_tool", `
def run(args):
    return json.encode({
        "cwd": os.cwd(),
        "hostname": os.hostname(),
        "platform": os.platform(),
        "argc": len(os.args()),
    })
`)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	result, err := runner.Run(context.Background(), json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("run: %v", err)
	}

	var payload struct {
		CWD      string `json:"cwd"`
		Hostname string `json:"hostname"`
		Platform string `json:"platform"`
		Argc     int    `json:"argc"`
	}
	if err := json.Unmarshal([]byte(result), &payload); err != nil {
		t.Fatalf("decode result: %v", err)
	}
	if payload.CWD != cwd {
		t.Fatalf("cwd = %q, want %q", payload.CWD, cwd)
	}
	if payload.Hostname != hostname {
		t.Fatalf("hostname = %q, want %q", payload.Hostname, hostname)
	}
	if payload.Platform != runtime.GOOS {
		t.Fatalf("platform = %q, want %q", payload.Platform, runtime.GOOS)
	}
	if payload.Argc == 0 {
		t.Fatal("expected os.args() to include at least the test binary path")
	}
}

func TestEngine_URLBuiltins(t *testing.T) {
	engine := NewEngine()
	runner, err := engine.CompileToolScript("url_tool", `
def run(args):
    parsed = url.parse("https://user:pass@example.com:8443/api/v1/items?q=go+lang&tag=ai&tag=llm#frag")
    encoded = url.encode({
        "q": "go lang",
        "page": 2,
        "tag": ["ai", "llm"],
    })
    return json.encode({
        "scheme": parsed["scheme"],
        "host": parsed["host"],
        "hostname": parsed["hostname"],
        "port": parsed["port"],
        "path": parsed["path"],
        "fragment": parsed["fragment"],
        "query": parsed["query"]["q"][0],
        "tag2": parsed["query"]["tag"][1],
        "username": parsed["username"],
        "password": parsed["password"],
        "encoded": encoded,
    })
`)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	result, err := runner.Run(context.Background(), json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("run: %v", err)
	}

	var payload map[string]string
	if err := json.Unmarshal([]byte(result), &payload); err != nil {
		t.Fatalf("decode result: %v", err)
	}
	if payload["scheme"] != "https" || payload["host"] != "example.com:8443" || payload["hostname"] != "example.com" || payload["port"] != "8443" {
		t.Fatalf("unexpected host payload: %+v", payload)
	}
	if payload["path"] != "/api/v1/items" || payload["fragment"] != "frag" || payload["query"] != "go lang" || payload["tag2"] != "llm" {
		t.Fatalf("unexpected parsed URL payload: %+v", payload)
	}
	if payload["username"] != "user" || payload["password"] != "pass" {
		t.Fatalf("unexpected user info: %+v", payload)
	}
	if payload["encoded"] != "page=2&q=go+lang&tag=ai&tag=llm" {
		t.Fatalf("encoded = %q", payload["encoded"])
	}
}

func TestEngine_UUIDBuiltin(t *testing.T) {
	engine := NewEngine()
	runner, err := engine.CompileToolScript("uuid_tool", `
def run(args):
    return uuid.v4() + "|" + uuid.v4()
`)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	result, err := runner.Run(context.Background(), json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("run: %v", err)
	}

	parts := strings.Split(result, "|")
	if len(parts) != 2 {
		t.Fatalf("expected two UUIDs, got %q", result)
	}
	pattern := regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
	for _, part := range parts {
		if !pattern.MatchString(part) {
			t.Fatalf("invalid uuid: %q", part)
		}
	}
	if parts[0] == parts[1] {
		t.Fatal("expected uuid.v4() to generate unique IDs")
	}
}

func TestEngine_SleepBuiltin(t *testing.T) {
	engine := NewEngine()
	runner, err := engine.CompileToolScript("sleep_tool", `
def run(args):
    sleep(25)
    return "awake"
`)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	start := time.Now()
	result, err := runner.Run(context.Background(), json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if result != "awake" {
		t.Fatalf("unexpected result: %q", result)
	}
	if elapsed := time.Since(start); elapsed < 20*time.Millisecond {
		t.Fatalf("sleep elapsed %v, expected at least 20ms", elapsed)
	}
}

func TestEngine_SleepBuiltinHonorsContextCancellation(t *testing.T) {
	engine := NewEngine()
	runner, err := engine.CompileToolScript("sleep_cancel_tool", `
def run(args):
    sleep(1000)
    return "awake"
`)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = runner.Run(ctx, json.RawMessage(`{}`))
	if err == nil {
		t.Fatal("expected sleep to fail after context cancellation")
	}
	if !strings.Contains(err.Error(), context.Canceled.Error()) {
		t.Fatalf("expected context cancellation error, got %v", err)
	}
}

func TestEngine_ConditionalHookScript(t *testing.T) {
	engine := NewEngine()
	runner, err := engine.CompileConditionalHookScript("conditional_hook", `should_fire(payload)`, `
def should_fire(payload):
    return payload.get("name", "") == "target"

def handle(event, payload):
    return block("blocked target")
`)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	blocked := runner.Run(context.Background(), hooks.EventToolPre, map[string]any{"name": "target"})
	if blocked.Action != hooks.ActionBlock || blocked.Reason != "blocked target" {
		t.Fatalf("unexpected blocked result: %+v", blocked)
	}

	allowed := runner.Run(context.Background(), hooks.EventToolPre, map[string]any{"name": "other"})
	if allowed.Action != hooks.ActionContinue {
		t.Fatalf("expected when condition to skip hook, got %+v", allowed)
	}
}

func TestEngine_Base64AndCryptoBuiltins(t *testing.T) {
	engine := NewEngine()
	runner, err := engine.CompileToolScript("encoding_tool", `
def run(args):
    encoded = base64.encode("hello webhook")
    decoded = base64.decode(encoded)
    signature = crypto.hmac_sha256("top-secret", encoded)
    return decoded + "|" + encoded + "|" + signature
`)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	result, err := runner.Run(context.Background(), json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if want := "hello webhook|aGVsbG8gd2ViaG9vaw==|9533b9fe167775d43e3c6727a3d0327ab49e3d905fe03ec9ba23642cb6fcd9bf"; result != want {
		t.Fatalf("got %q, want %q", result, want)
	}
}

func TestEngine_StringBuiltins(t *testing.T) {
	engine := NewEngine()
	runner, err := engine.CompileToolScript("string_tool", `
def run(args):
    parts = string.split("  alpha,beta,gamma  ", ",")
    return json.encode({
        "upper": string.upper("go"),
        "lower": string.lower("AI"),
        "trim": string.trim("  spaced  "),
        "join": string.join(parts, "|"),
        "truncate": string.truncate("truncate-me", 8),
        "left": string.pad_left("7", 3, "0"),
        "right": string.pad_right("7", 3, "0"),
    })
`)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	result, err := runner.Run(context.Background(), json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("run: %v", err)
	}

	var payload map[string]string
	if err := json.Unmarshal([]byte(result), &payload); err != nil {
		t.Fatalf("decode result: %v", err)
	}
	if payload["upper"] != "GO" || payload["lower"] != "ai" || payload["trim"] != "spaced" {
		t.Fatalf("unexpected case/trim payload: %+v", payload)
	}
	if payload["join"] != "  alpha|beta|gamma  " || payload["truncate"] != "truncate" {
		t.Fatalf("unexpected split/join payload: %+v", payload)
	}
	if payload["left"] != "007" || payload["right"] != "700" {
		t.Fatalf("unexpected padding payload: %+v", payload)
	}
}

func TestEngine_MetricsBuiltinsPersistAcrossRuns(t *testing.T) {
	engine := NewEngine()
	runner, err := engine.CompileToolScript("metrics_tool", `
def run(args):
    current = metrics.incr("packets")
    metrics.incr("bytes", 5)
    snapshot = metrics.snapshot()
    return json.encode({
        "packets": current,
        "bytes": metrics.get("bytes"),
        "snapshot": snapshot,
    })
`)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	first, err := runner.Run(context.Background(), json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("first run: %v", err)
	}
	second, err := runner.Run(context.Background(), json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("second run: %v", err)
	}

	var payload map[string]any
	if err := json.Unmarshal([]byte(first), &payload); err != nil {
		t.Fatalf("decode first: %v", err)
	}
	if payload["packets"].(float64) != 1 || payload["bytes"].(float64) != 5 {
		t.Fatalf("unexpected first payload: %+v", payload)
	}
	if err := json.Unmarshal([]byte(second), &payload); err != nil {
		t.Fatalf("decode second: %v", err)
	}
	if payload["packets"].(float64) != 2 || payload["bytes"].(float64) != 10 {
		t.Fatalf("unexpected second payload: %+v", payload)
	}

	resetRunner, err := engine.CompileToolScript("metrics_reset_tool", `
def run(args):
    metrics.reset("bytes")
    metrics.reset()
    return str(metrics.get("packets")) + "|" + str(metrics.get("bytes"))
`)
	if err != nil {
		t.Fatalf("compile reset: %v", err)
	}
	resetResult, err := resetRunner.Run(context.Background(), json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("reset run: %v", err)
	}
	if resetResult != "0|0" {
		t.Fatalf("unexpected reset result: %q", resetResult)
	}
}

func TestEngine_EmitCustomEventBuiltin(t *testing.T) {
	system := hooks.NewSystem()
	system.Register(hooks.Registration{
		Name:  "custom-audit",
		Event: hooks.Event("custom.audit"),
		Handler: func(ctx context.Context, event hooks.Event, payload any) hooks.Result {
			data := payload.(map[string]any)
			data["seen"] = true
			data["count"] = 3
			return hooks.Result{Action: hooks.ActionModify, Payload: data}
		},
	})

	engine := NewEngine()
	runner, err := engine.CompileToolScript("emit_tool", `
def run(args):
    payload = emit("custom.audit", {"kind": string.upper(args["kind"])})
    return json.encode(payload)
`)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	ctx := hooks.WithDispatcher(context.Background(), system)
	result, err := runner.Run(ctx, json.RawMessage(`{"kind":"webhook"}`))
	if err != nil {
		t.Fatalf("run: %v", err)
	}

	var payload map[string]any
	if err := json.Unmarshal([]byte(result), &payload); err != nil {
		t.Fatalf("decode result: %v", err)
	}
	if payload["kind"] != "WEBHOOK" || payload["seen"] != true || payload["count"].(float64) != 3 {
		t.Fatalf("unexpected custom event payload: %+v", payload)
	}
}

func TestEngine_EmitBuiltinRejectsNonCustomEvents(t *testing.T) {
	engine := NewEngine()
	runner, err := engine.CompileToolScript("emit_bad_tool", `
def run(args):
    emit("tool.pre", {"name": "oops"})
    return "ok"
`)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	_, err = runner.Run(context.Background(), json.RawMessage(`{}`))
	if err == nil || !strings.Contains(err.Error(), hooks.CustomEventPrefix) {
		t.Fatalf("expected custom event prefix error, got %v", err)
	}
}
