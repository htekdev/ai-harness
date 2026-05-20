package scripting

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	"github.com/htekdev/ai-harness/hooks"
)

func TestEngine_CompileToolScript_Success(t *testing.T) {
	engine := NewEngine()

	script := `
def run(args):
    return "hello " + args["name"]
`
	runner, err := engine.CompileToolScript("greet", script)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	args := json.RawMessage(`{"name": "world"}`)
	result, err := runner.Run(context.Background(), args)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if result != "hello world" {
		t.Errorf("got %q, want %q", result, "hello world")
	}
}

func TestEngine_CompileToolScript_MissingRunFunction(t *testing.T) {
	engine := NewEngine()

	script := `
def helper():
    return "nope"
`
	_, err := engine.CompileToolScript("bad", script)
	if err == nil {
		t.Fatal("expected error for missing run function")
	}
}

func TestEngine_CompileToolScript_Arithmetic(t *testing.T) {
	engine := NewEngine()

	script := `
def run(args):
    op = args["operation"]
    a = args["a"]
    b = args["b"]
    if op == "add":
        return str(a + b)
    elif op == "subtract":
        return str(a - b)
    elif op == "multiply":
        return str(a * b)
    elif op == "divide":
        if b == 0:
            fail("division by zero")
        return str(a / b)
    fail("unknown operation: " + op)
`
	runner, err := engine.CompileToolScript("calc", script)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	tests := []struct {
		args string
		want string
	}{
		{`{"operation":"add","a":3,"b":4}`, "7"},
		{`{"operation":"subtract","a":10,"b":3}`, "7"},
		{`{"operation":"multiply","a":5,"b":6}`, "30"},
	}

	for _, tt := range tests {
		result, err := runner.Run(context.Background(), json.RawMessage(tt.args))
		if err != nil {
			t.Errorf("run(%s): %v", tt.args, err)
			continue
		}
		if result != tt.want {
			t.Errorf("run(%s) = %q, want %q", tt.args, result, tt.want)
		}
	}
}

func TestEngine_CompileToolScript_RuntimeError(t *testing.T) {
	engine := NewEngine()

	script := `
def run(args):
    fail("intentional error")
`
	runner, err := engine.CompileToolScript("failing", script)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	_, err = runner.Run(context.Background(), json.RawMessage(`{}`))
	if err == nil {
		t.Fatal("expected runtime error")
	}
}

func TestEngine_CompileToolScript_TimeBuiltin(t *testing.T) {
	engine := NewEngine()

	script := `
def run(args):
    return time.now()
`
	runner, err := engine.CompileToolScript("time_tool", script)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	result, err := runner.Run(context.Background(), json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if result == "" {
		t.Error("expected non-empty time string")
	}
}

func TestEngine_CompileToolScript_JSONBuiltins(t *testing.T) {
	engine := NewEngine()

	script := `
def run(args):
    encoded = json.encode({"key": "value", "num": 42})
    decoded = json.decode(encoded)
    return decoded["key"] + ":" + str(decoded["num"])
`
	runner, err := engine.CompileToolScript("json_tool", script)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	result, err := runner.Run(context.Background(), json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if result != "value:42" {
		t.Errorf("got %q, want %q", result, "value:42")
	}
}

func TestEngine_CompileToolScript_EnvBuiltin(t *testing.T) {
	engine := NewEngine()

	t.Setenv("TEST_SCRIPTING_VAR", "hello_env")

	script := `
def run(args):
    return env("TEST_SCRIPTING_VAR")
`
	runner, err := engine.CompileToolScript("env_tool", script)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	result, err := runner.Run(context.Background(), json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if result != "hello_env" {
		t.Errorf("got %q, want %q", result, "hello_env")
	}
}

func TestEngine_CompileHookScript_Continue(t *testing.T) {
	engine := NewEngine()

	script := `
def handle(event, payload):
    return allow()
`
	runner, err := engine.CompileHookScript("pass_hook", script)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	result := runner.Run(context.Background(), hooks.EventToolPre, map[string]any{"name": "test"})
	if result.Action != hooks.ActionContinue {
		t.Errorf("got action %d, want ActionContinue", result.Action)
	}
}

func TestEngine_CompileHookScript_Block(t *testing.T) {
	engine := NewEngine()

	script := `
def handle(event, payload):
    if payload["name"] == "dangerous":
        return block("tool is blocked")
    return allow()
`
	runner, err := engine.CompileHookScript("guard_hook", script)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	result := runner.Run(context.Background(), hooks.EventToolPre, map[string]any{"name": "dangerous"})
	if result.Action != hooks.ActionBlock {
		t.Errorf("got action %d, want ActionBlock", result.Action)
	}
	if result.Reason != "tool is blocked" {
		t.Errorf("got reason %q, want %q", result.Reason, "tool is blocked")
	}

	result = runner.Run(context.Background(), hooks.EventToolPre, map[string]any{"name": "safe"})
	if result.Action != hooks.ActionContinue {
		t.Errorf("got action %d, want ActionContinue for safe tool", result.Action)
	}
}

func TestEngine_CompileHookScript_Modify(t *testing.T) {
	engine := NewEngine()

	script := `
def handle(event, payload):
    payload["modified"] = True
    return modify(payload)
`
	runner, err := engine.CompileHookScript("modify_hook", script)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	result := runner.Run(context.Background(), hooks.EventTurnStart, map[string]any{"msg": "hello"})
	if result.Action != hooks.ActionModify {
		t.Errorf("got action %d, want ActionModify", result.Action)
	}
	payload, ok := result.Payload.(map[string]any)
	if !ok {
		t.Fatalf("payload type: %T", result.Payload)
	}
	if payload["modified"] != true {
		t.Errorf("expected modified=true in payload")
	}
}

func TestEngine_CompileHookScript_MissingHandleFunction(t *testing.T) {
	engine := NewEngine()

	script := `
def something_else(event, payload):
    return allow()
`
	_, err := engine.CompileHookScript("bad_hook", script)
	if err == nil {
		t.Fatal("expected error for missing handle function")
	}
}

func TestEngine_CompileHookScript_EventString(t *testing.T) {
	engine := NewEngine()

	script := `
def handle(event, payload):
    if event == "tool.pre":
        return block("blocked at tool.pre")
    return allow()
`
	runner, err := engine.CompileHookScript("event_hook", script)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	result := runner.Run(context.Background(), hooks.EventToolPre, nil)
	if result.Action != hooks.ActionBlock {
		t.Errorf("expected block for tool.pre")
	}

	result = runner.Run(context.Background(), hooks.EventToolPost, nil)
	if result.Action != hooks.ActionContinue {
		t.Errorf("expected continue for tool.post")
	}
}

func TestNewToolHandler(t *testing.T) {
	engine := NewEngine()

	handler, err := NewToolHandler(engine, "echo", `
def run(args):
    return args["message"]
`)
	if err != nil {
		t.Fatalf("create handler: %v", err)
	}

	result, err := handler(context.Background(), json.RawMessage(`{"message":"ping"}`))
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result != "ping" {
		t.Errorf("got %q, want %q", result, "ping")
	}
}

func TestNewHookHandler(t *testing.T) {
	engine := NewEngine()

	handler, err := NewHookHandler(engine, "test_hook", `
def handle(event, payload):
    return block("nope")
`)
	if err != nil {
		t.Fatalf("create handler: %v", err)
	}

	result := handler(context.Background(), hooks.EventToolPre, nil)
	if result.Action != hooks.ActionBlock {
		t.Errorf("expected block")
	}
}

func TestEngine_CompileToolScript_NullArgs(t *testing.T) {
	engine := NewEngine()

	script := `
def run(args):
    return "ok"
`
	runner, err := engine.CompileToolScript("null_args", script)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	result, err := runner.Run(context.Background(), nil)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if result != "ok" {
		t.Errorf("got %q, want %q", result, "ok")
	}
}

func TestEngine_FsBuiltins(t *testing.T) {
	engine := NewEngine()

	// Test fs.write + fs.read
	t.Run("write_and_read", func(t *testing.T) {
		script := `
def run(args):
    fs.write(args["path"], args["content"])
    return fs.read(args["path"])
`
		runner, err := engine.CompileToolScript("fs_write_read", script)
		if err != nil {
			t.Fatalf("compile: %v", err)
		}

		args := json.RawMessage(`{"path": "testdata_fs_test.txt", "content": "hello fs"}`)
		result, err := runner.Run(context.Background(), args)
		if err != nil {
			t.Fatalf("run: %v", err)
		}
		if result != "hello fs" {
			t.Errorf("got %q, want %q", result, "hello fs")
		}
		// Cleanup
		_ = os.Remove("testdata_fs_test.txt")
	})

	// Test fs.exists
	t.Run("exists", func(t *testing.T) {
		script := `
def run(args):
    fs.write("testdata_exists_check.txt", "x")
    before = fs.exists("testdata_exists_check.txt")
    fs.remove("testdata_exists_check.txt")
    after = fs.exists("testdata_exists_check.txt")
    return str(before) + "," + str(after)
`
		runner, err := engine.CompileToolScript("fs_exists", script)
		if err != nil {
			t.Fatalf("compile: %v", err)
		}

		result, err := runner.Run(context.Background(), json.RawMessage(`{}`))
		if err != nil {
			t.Fatalf("run: %v", err)
		}
		if result != "True,False" {
			t.Errorf("got %q, want %q", result, "True,False")
		}
	})

	// Test fs.append
	t.Run("append", func(t *testing.T) {
		script := `
def run(args):
    fs.write("testdata_append.txt", "line1\n")
    fs.append("testdata_append.txt", "line2\n")
    return fs.read("testdata_append.txt")
`
		runner, err := engine.CompileToolScript("fs_append", script)
		if err != nil {
			t.Fatalf("compile: %v", err)
		}

		result, err := runner.Run(context.Background(), json.RawMessage(`{}`))
		if err != nil {
			t.Fatalf("run: %v", err)
		}
		if result != "line1\nline2\n" {
			t.Errorf("got %q, want %q", result, "line1\nline2\n")
		}
		_ = os.Remove("testdata_append.txt")
	})

	// Test fs.mkdir + fs.list
	t.Run("mkdir_and_list", func(t *testing.T) {
		script := `
def run(args):
    fs.mkdir("testdata_dir/sub")
    fs.write("testdata_dir/sub/a.txt", "a")
    fs.write("testdata_dir/sub/b.txt", "b")
    entries = fs.list("testdata_dir/sub")
    return json.encode(entries)
`
		runner, err := engine.CompileToolScript("fs_mkdir_list", script)
		if err != nil {
			t.Fatalf("compile: %v", err)
		}

		result, err := runner.Run(context.Background(), json.RawMessage(`{}`))
		if err != nil {
			t.Fatalf("run: %v", err)
		}
		if result != `["a.txt","b.txt"]` {
			t.Errorf("got %q, want %q", result, `["a.txt","b.txt"]`)
		}
		_ = os.RemoveAll("testdata_dir")
	})

	// Test fs.stat
	t.Run("stat", func(t *testing.T) {
		script := `
def run(args):
    fs.write("testdata_stat.txt", "12345")
    info = fs.stat("testdata_stat.txt")
    return str(info.size) + "," + str(info.is_dir)
`
		runner, err := engine.CompileToolScript("fs_stat", script)
		if err != nil {
			t.Fatalf("compile: %v", err)
		}

		result, err := runner.Run(context.Background(), json.RawMessage(`{}`))
		if err != nil {
			t.Fatalf("run: %v", err)
		}
		if result != "5,False" {
			t.Errorf("got %q, want %q", result, "5,False")
		}
		_ = os.Remove("testdata_stat.txt")
	})
}

func TestEngine_FsEditBuiltins(t *testing.T) {
	engine := NewEngine()

	// Test fs.replace (surgical find-and-replace)
	t.Run("replace_unique", func(t *testing.T) {
		script := `
def run(args):
    fs.write("testdata_edit.txt", "hello world\nfoo bar\nbaz qux")
    result = fs.replace("testdata_edit.txt", "foo bar", "FOO BAR")
    return fs.read("testdata_edit.txt")
`
		runner, err := engine.CompileToolScript("fs_replace", script)
		if err != nil {
			t.Fatalf("compile: %v", err)
		}
		result, err := runner.Run(context.Background(), json.RawMessage(`{}`))
		if err != nil {
			t.Fatalf("run: %v", err)
		}
		if result != "hello world\nFOO BAR\nbaz qux" {
			t.Errorf("got %q", result)
		}
		_ = os.Remove("testdata_edit.txt")
	})

	// Test fs.replace fails on duplicate
	t.Run("replace_not_unique", func(t *testing.T) {
		script := `
def run(args):
    fs.write("testdata_edit2.txt", "aaa\naaa\nbbb")
    return fs.replace("testdata_edit2.txt", "aaa", "ccc")
`
		runner, err := engine.CompileToolScript("fs_replace_dup", script)
		if err != nil {
			t.Fatalf("compile: %v", err)
		}
		_, err = runner.Run(context.Background(), json.RawMessage(`{}`))
		if err == nil {
			t.Fatal("expected error for non-unique match")
		}
		_ = os.Remove("testdata_edit2.txt")
	})

	// Test fs.replace_all
	t.Run("replace_all", func(t *testing.T) {
		script := `
def run(args):
    fs.write("testdata_edit3.txt", "aaa\naaa\nbbb")
    fs.replace_all("testdata_edit3.txt", "aaa", "ccc")
    return fs.read("testdata_edit3.txt")
`
		runner, err := engine.CompileToolScript("fs_replace_all", script)
		if err != nil {
			t.Fatalf("compile: %v", err)
		}
		result, err := runner.Run(context.Background(), json.RawMessage(`{}`))
		if err != nil {
			t.Fatalf("run: %v", err)
		}
		if result != "ccc\nccc\nbbb" {
			t.Errorf("got %q", result)
		}
		_ = os.Remove("testdata_edit3.txt")
	})

	// Test fs.read_lines
	t.Run("read_lines", func(t *testing.T) {
		script := `
def run(args):
    fs.write("testdata_lines.txt", "line1\nline2\nline3\nline4\nline5")
    return fs.read_lines("testdata_lines.txt", 2, 4)
`
		runner, err := engine.CompileToolScript("fs_read_lines", script)
		if err != nil {
			t.Fatalf("compile: %v", err)
		}
		result, err := runner.Run(context.Background(), json.RawMessage(`{}`))
		if err != nil {
			t.Fatalf("run: %v", err)
		}
		expected := "2. line2\n3. line3\n4. line4\n"
		if result != expected {
			t.Errorf("got %q, want %q", result, expected)
		}
		_ = os.Remove("testdata_lines.txt")
	})

	// Test fs.insert_at
	t.Run("insert_at", func(t *testing.T) {
		script := `
def run(args):
    fs.write("testdata_insert.txt", "line1\nline2\nline3")
    fs.insert_at("testdata_insert.txt", 2, "NEW LINE")
    return fs.read("testdata_insert.txt")
`
		runner, err := engine.CompileToolScript("fs_insert_at", script)
		if err != nil {
			t.Fatalf("compile: %v", err)
		}
		result, err := runner.Run(context.Background(), json.RawMessage(`{}`))
		if err != nil {
			t.Fatalf("run: %v", err)
		}
		if result != "line1\nNEW LINE\nline2\nline3" {
			t.Errorf("got %q", result)
		}
		_ = os.Remove("testdata_insert.txt")
	})

	// Test fs.replace_lines
	t.Run("replace_lines", func(t *testing.T) {
		script := `
def run(args):
    fs.write("testdata_repl.txt", "a\nb\nc\nd\ne")
    fs.replace_lines("testdata_repl.txt", 2, 4, "X\nY")
    return fs.read("testdata_repl.txt")
`
		runner, err := engine.CompileToolScript("fs_replace_lines", script)
		if err != nil {
			t.Fatalf("compile: %v", err)
		}
		result, err := runner.Run(context.Background(), json.RawMessage(`{}`))
		if err != nil {
			t.Fatalf("run: %v", err)
		}
		if result != "a\nX\nY\ne" {
			t.Errorf("got %q", result)
		}
		_ = os.Remove("testdata_repl.txt")
	})

	// Test fs.delete_lines
	t.Run("delete_lines", func(t *testing.T) {
		script := `
def run(args):
    fs.write("testdata_del.txt", "a\nb\nc\nd\ne")
    fs.delete_lines("testdata_del.txt", 2, 4)
    return fs.read("testdata_del.txt")
`
		runner, err := engine.CompileToolScript("fs_delete_lines", script)
		if err != nil {
			t.Fatalf("compile: %v", err)
		}
		result, err := runner.Run(context.Background(), json.RawMessage(`{}`))
		if err != nil {
			t.Fatalf("run: %v", err)
		}
		if result != "a\ne" {
			t.Errorf("got %q", result)
		}
		_ = os.Remove("testdata_del.txt")
	})

	// Test fs.line_count
	t.Run("line_count", func(t *testing.T) {
		script := `
def run(args):
    fs.write("testdata_lc.txt", "a\nb\nc\nd")
    return str(fs.line_count("testdata_lc.txt"))
`
		runner, err := engine.CompileToolScript("fs_line_count", script)
		if err != nil {
			t.Fatalf("compile: %v", err)
		}
		result, err := runner.Run(context.Background(), json.RawMessage(`{}`))
		if err != nil {
			t.Fatalf("run: %v", err)
		}
		if result != "4" {
			t.Errorf("got %q, want %q", result, "4")
		}
		_ = os.Remove("testdata_lc.txt")
	})

	// Test fs.find
	t.Run("find", func(t *testing.T) {
		script := `
def run(args):
    fs.write("testdata_find.txt", "apple\nbanana\napricot\ncherry")
    matches = fs.find("testdata_find.txt", "ap")
    results = []
    for m in matches:
        results.append(str(m.line_num) + ":" + m.text)
    return "|".join(results)
`
		runner, err := engine.CompileToolScript("fs_find", script)
		if err != nil {
			t.Fatalf("compile: %v", err)
		}
		result, err := runner.Run(context.Background(), json.RawMessage(`{}`))
		if err != nil {
			t.Fatalf("run: %v", err)
		}
		if result != "1:apple|3:apricot" {
			t.Errorf("got %q", result)
		}
		_ = os.Remove("testdata_find.txt")
	})
}
