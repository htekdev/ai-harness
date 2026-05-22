package scripting

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/htekdev/ai-harness/hooks"
)

func TestEngine_AssertBuiltin(t *testing.T) {
	engine := NewEngine()
	runner, err := engine.CompileToolScript("assert_tool", `
def run(args):
    assert(2 + 2 == 4, "math broke")
    return "ok"
`)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	result, err := runner.Run(WithTurnState(context.Background()), json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if result != "ok" {
		t.Fatalf("unexpected result: %q", result)
	}
}

func TestEngine_AssertBuiltinFailure(t *testing.T) {
	engine := NewEngine()
	runner, err := engine.CompileToolScript("assert_fail_tool", `
def run(args):
    assert(False, "boom")
    return "nope"
`)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	_, err = runner.Run(WithTurnState(context.Background()), json.RawMessage(`{}`))
	if err == nil || !strings.Contains(err.Error(), "assert: boom") {
		t.Fatalf("expected assertion failure, got %v", err)
	}
}

func TestEngine_FsGlobBuiltin(t *testing.T) {
	engine := NewEngine()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	tmp := t.TempDir()
	if err := os.Chdir(tmp); err != nil {
		t.Fatalf("chdir temp: %v", err)
	}
	defer func() {
		_ = os.Chdir(cwd)
	}()

	for _, rel := range []string{"root.txt", filepath.Join("nested", "a.txt"), filepath.Join("nested", "deep", "b.txt"), filepath.Join("nested", "deep", "skip.log")} {
		full := filepath.Join(tmp, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", rel, err)
		}
		if err := os.WriteFile(full, []byte(rel), 0o644); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
	}

	runner, err := engine.CompileToolScript("glob_tool", `
def run(args):
    return json.encode(fs.glob("**/*.txt"))
`)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	result, err := runner.Run(WithTurnState(context.Background()), json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	var matches []string
	if err := json.Unmarshal([]byte(result), &matches); err != nil {
		t.Fatalf("decode: %v", err)
	}
	want := []string{filepath.Clean(filepath.Join("nested", "a.txt")), filepath.Clean(filepath.Join("nested", "deep", "b.txt")), "root.txt"}
	if len(matches) != len(want) {
		t.Fatalf("matches = %v, want %v", matches, want)
	}
	for i, item := range want {
		if matches[i] != item {
			t.Fatalf("matches[%d] = %q, want %q", i, matches[i], item)
		}
	}
}

func TestEngine_ExecRunBuiltin(t *testing.T) {
	t.Setenv("GO_WANT_HELPER_PROCESS", "1")
	engine := NewEngine()
	runner, err := engine.CompileToolScript("exec_tool", `
def run(args):
    result = exec.run(args["cmd"], ["-test.run=TestExecHelperProcess", "--", "success"], 10000)
    return json.encode(result)
`)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	result, err := runner.Run(WithTurnState(context.Background()), json.RawMessage(`{"cmd":`+jsonString(os.Args[0])+`}`))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(result), &payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if payload["ok"] != true || strings.TrimSpace(payload["stdout"].(string)) != "helper-success" {
		t.Fatalf("unexpected payload: %+v", payload)
	}
}

func TestEngine_ExecRunBuiltinNonZeroExit(t *testing.T) {
	t.Setenv("GO_WANT_HELPER_PROCESS", "1")
	engine := NewEngine()
	runner, err := engine.CompileToolScript("exec_nonzero_tool", `
def run(args):
    result = exec.run(args["cmd"], ["-test.run=TestExecHelperProcess", "--", "fail"], 10000)
    return json.encode(result)
`)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	result, err := runner.Run(WithTurnState(context.Background()), json.RawMessage(`{"cmd":`+jsonString(os.Args[0])+`}`))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(result), &payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if payload["ok"] != false || int(payload["exit_code"].(float64)) != 7 {
		t.Fatalf("unexpected payload: %+v", payload)
	}
}

func TestEngine_ExecRunBuiltinTimeout(t *testing.T) {
	t.Setenv("GO_WANT_HELPER_PROCESS", "1")
	engine := NewEngine()
	runner, err := engine.CompileToolScript("exec_timeout_tool", `
def run(args):
    result = exec.run(args["cmd"], ["-test.run=TestExecHelperProcess", "--", "sleep"], 25)
    return json.encode(result)
`)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	_, err = runner.Run(WithTurnState(context.Background()), json.RawMessage(`{"cmd":`+jsonString(os.Args[0])+`}`))
	if err == nil || !strings.Contains(err.Error(), context.DeadlineExceeded.Error()) {
		t.Fatalf("expected timeout error, got %v", err)
	}
}

func TestEngine_ExecRunBuiltinRejectsShellSyntax(t *testing.T) {
	engine := NewEngine()
	runner, err := engine.CompileToolScript("exec_reject_tool", `
def run(args):
    exec.run("echo | whoami")
    return "nope"
`)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	_, err = runner.Run(WithTurnState(context.Background()), json.RawMessage(`{}`))
	if err == nil || !strings.Contains(err.Error(), "forbidden shell token") {
		t.Fatalf("expected forbidden token error, got %v", err)
	}
}

func TestEngine_TurnStateBuiltinsAcrossHookAndTool(t *testing.T) {
	engine := NewEngine()
	hookRunner, err := engine.CompileHookScript("state_hook", `
def handle(event, payload):
    ctx.set("active_tool", payload["name"])
    return allow()
`)
	if err != nil {
		t.Fatalf("compile hook: %v", err)
	}
	toolRunner, err := engine.CompileToolScript("state_tool", `
def run(args):
    snapshot = ctx.snapshot()
    assert(ctx.has("active_tool"), "missing active_tool")
    value = ctx.get("active_tool")
    deleted = ctx.delete("active_tool")
    return value + "|" + str(deleted) + "|" + str(snapshot["active_tool"])
`)
	if err != nil {
		t.Fatalf("compile tool: %v", err)
	}

	ctx := WithTurnState(context.Background())
	result := hookRunner.Run(ctx, hooks.EventToolPre, map[string]any{"name": "state_tool"})
	if result.Action != hooks.ActionContinue {
		t.Fatalf("unexpected hook result: %+v", result)
	}
	toolResult, err := toolRunner.Run(ctx, json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("tool run: %v", err)
	}
	if toolResult != "state_tool|True|state_tool" {
		t.Fatalf("unexpected tool result: %q", toolResult)
	}
}

func TestExecHelperProcess(t *testing.T) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") != "1" {
		return
	}
	args := os.Args
	idx := 0
	for idx < len(args) && args[idx] != "--" {
		idx++
	}
	if idx >= len(args)-1 {
		os.Exit(2)
	}
	switch args[idx+1] {
	case "success":
		_, _ = os.Stdout.WriteString("helper-success\n")
		os.Exit(0)
	case "fail":
		_, _ = os.Stderr.WriteString("helper-failure\n")
		os.Exit(7)
	case "sleep":
		time.Sleep(200 * time.Millisecond)
		_, _ = os.Stdout.WriteString("slow-success\n")
		os.Exit(0)
	default:
		os.Exit(3)
	}
}

func jsonString(s string) string {
	data, _ := json.Marshal(s)
	return string(data)
}
