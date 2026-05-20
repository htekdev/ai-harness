// Package scripting provides a Starlark-based scripting engine for defining
// tool handlers and hook handlers inline in YAML configuration.
package scripting

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"math/rand/v2"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"go.starlark.net/starlark"
	"go.starlark.net/starlarkstruct"
	"go.starlark.net/syntax"

	"github.com/htekdev/ai-harness/hooks"
)

// Engine manages Starlark script compilation and execution.
type Engine struct {
	mu       sync.Mutex
	builtins starlark.StringDict
}

// NewEngine creates a new scripting engine with built-in modules.
func NewEngine() *Engine {
	e := &Engine{}
	e.builtins = e.makeBuiltins()
	return e
}

// makeBuiltins creates the standard library available to all scripts.
func (e *Engine) makeBuiltins() starlark.StringDict {
	timeMod := starlarkstruct.FromStringDict(starlarkstruct.Default, starlark.StringDict{
		"now": starlark.NewBuiltin("time.now", builtinTimeNow),
	})

	jsonMod := starlarkstruct.FromStringDict(starlarkstruct.Default, starlark.StringDict{
		"encode": starlark.NewBuiltin("json.encode", builtinJSONEncode),
		"decode": starlark.NewBuiltin("json.decode", builtinJSONDecode),
	})

	mathMod := starlarkstruct.FromStringDict(starlarkstruct.Default, starlark.StringDict{
		"abs":   starlark.NewBuiltin("math.abs", builtinMathAbs),
		"min":   starlark.NewBuiltin("math.min", builtinMathMin),
		"max":   starlark.NewBuiltin("math.max", builtinMathMax),
		"floor": starlark.NewBuiltin("math.floor", builtinMathFloor),
		"ceil":  starlark.NewBuiltin("math.ceil", builtinMathCeil),
	})

	fsMod := starlarkstruct.FromStringDict(starlarkstruct.Default, starlark.StringDict{
		"read":          starlark.NewBuiltin("fs.read", builtinFsRead),
		"write":         starlark.NewBuiltin("fs.write", builtinFsWrite),
		"append":        starlark.NewBuiltin("fs.append", builtinFsAppend),
		"exists":        starlark.NewBuiltin("fs.exists", builtinFsExists),
		"remove":        starlark.NewBuiltin("fs.remove", builtinFsRemove),
		"mkdir":         starlark.NewBuiltin("fs.mkdir", builtinFsMkdir),
		"list":          starlark.NewBuiltin("fs.list", builtinFsList),
		"stat":          starlark.NewBuiltin("fs.stat", builtinFsStat),
		"replace":       starlark.NewBuiltin("fs.replace", builtinFsReplace),
		"replace_all":   starlark.NewBuiltin("fs.replace_all", builtinFsReplaceAll),
		"read_lines":    starlark.NewBuiltin("fs.read_lines", builtinFsReadLines),
		"insert_at":     starlark.NewBuiltin("fs.insert_at", builtinFsInsertAt),
		"replace_lines": starlark.NewBuiltin("fs.replace_lines", builtinFsReplaceLines),
		"delete_lines":  starlark.NewBuiltin("fs.delete_lines", builtinFsDeleteLines),
		"line_count":    starlark.NewBuiltin("fs.line_count", builtinFsLineCount),
		"find":          starlark.NewBuiltin("fs.find", builtinFsFind),
	})

	return starlark.StringDict{
		"time":   timeMod,
		"json":   jsonMod,
		"math":   mathMod,
		"fs":     fsMod,
		"env":    starlark.NewBuiltin("env", builtinEnv),
		"log":    starlark.NewBuiltin("log", builtinLog),
		"allow":  starlark.NewBuiltin("allow", builtinContinue),
		"block":  starlark.NewBuiltin("block", builtinBlock),
		"modify": starlark.NewBuiltin("modify", builtinModify),
		"random": starlark.NewBuiltin("random", builtinRandom),
	}
}

// CompileToolScript compiles a tool script and returns a ToolRunner.
// The script must define a `run(args)` function.
func (e *Engine) CompileToolScript(name, script string) (*ToolRunner, error) {
	thread := &starlark.Thread{Name: name}
	globals, err := starlark.ExecFile(thread, name+".star", script, e.builtins)
	if err != nil {
		return nil, fmt.Errorf("compile tool script %q: %w", name, err)
	}

	runFn, ok := globals["run"]
	if !ok {
		return nil, fmt.Errorf("tool script %q must define a 'run' function", name)
	}
	if _, ok := runFn.(starlark.Callable); !ok {
		return nil, fmt.Errorf("tool script %q: 'run' must be a function", name)
	}

	return &ToolRunner{
		name:     name,
		globals:  globals,
		builtins: e.builtins,
	}, nil
}

// CompileHookScript compiles a hook script and returns a HookRunner.
// The script must define a `handle(event, payload)` function.
func (e *Engine) CompileHookScript(name, script string) (*HookRunner, error) {
	thread := &starlark.Thread{Name: name}
	globals, err := starlark.ExecFile(thread, name+".star", script, e.builtins)
	if err != nil {
		return nil, fmt.Errorf("compile hook script %q: %w", name, err)
	}

	handleFn, ok := globals["handle"]
	if !ok {
		return nil, fmt.Errorf("hook script %q must define a 'handle' function", name)
	}
	if _, ok := handleFn.(starlark.Callable); !ok {
		return nil, fmt.Errorf("hook script %q: 'handle' must be a function", name)
	}

	return &HookRunner{
		name:     name,
		globals:  globals,
		builtins: e.builtins,
	}, nil
}

// ToolRunner executes a compiled tool script.
type ToolRunner struct {
	name     string
	globals  starlark.StringDict
	builtins starlark.StringDict
}

// Run executes the tool's `run(args)` function with the given JSON arguments.
func (tr *ToolRunner) Run(ctx context.Context, args json.RawMessage) (string, error) {
	argsDict, err := jsonToStarlark(args)
	if err != nil {
		return "", fmt.Errorf("convert args: %w", err)
	}

	runFn := tr.globals["run"].(starlark.Callable)
	thread := &starlark.Thread{Name: tr.name + "-exec"}

	result, err := starlark.Call(thread, runFn, starlark.Tuple{argsDict}, nil)
	if err != nil {
		return "", fmt.Errorf("script error: %w", err)
	}

	return starlarkToString(result), nil
}

// HookRunner executes a compiled hook script.
type HookRunner struct {
	name     string
	globals  starlark.StringDict
	builtins starlark.StringDict
}

// Run executes the hook's `handle(event, payload)` function.
func (hr *HookRunner) Run(ctx context.Context, event hooks.Event, payload any) hooks.Result {
	payloadVal, err := goToStarlark(payload)
	if err != nil {
		return hooks.Result{
			Action: hooks.ActionContinue,
			Reason: fmt.Sprintf("hook %q: failed to convert payload: %v", hr.name, err),
		}
	}

	handleFn := hr.globals["handle"].(starlark.Callable)
	thread := &starlark.Thread{Name: hr.name + "-exec"}

	result, err := starlark.Call(thread, handleFn, starlark.Tuple{
		starlark.String(event),
		payloadVal,
	}, nil)
	if err != nil {
		return hooks.Result{
			Action: hooks.ActionContinue,
			Reason: fmt.Sprintf("hook %q script error: %v", hr.name, err),
		}
	}

	return interpretHookResult(result)
}

// interpretHookResult converts a Starlark return value into a hooks.Result.
func interpretHookResult(val starlark.Value) hooks.Result {
	if hr, ok := val.(*hookResultValue); ok {
		return hr.result
	}
	return hooks.Result{Action: hooks.ActionContinue}
}

// hookResultValue is a Starlark value wrapping a hooks.Result.
type hookResultValue struct {
	result hooks.Result
}

func (h *hookResultValue) String() string        { return fmt.Sprintf("HookResult(%d)", h.result.Action) }
func (h *hookResultValue) Type() string           { return "HookResult" }
func (h *hookResultValue) Freeze()                {}
func (h *hookResultValue) Truth() starlark.Bool   { return starlark.True }
func (h *hookResultValue) Hash() (uint32, error)  { return 0, fmt.Errorf("unhashable") }

// --- Built-in functions ---

func builtinTimeNow(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	if err := starlark.UnpackArgs("time.now", args, kwargs); err != nil {
		return nil, err
	}
	return starlark.String(time.Now().Format(time.RFC3339)), nil
}

func builtinEnv(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var key string
	if err := starlark.UnpackArgs("env", args, kwargs, "key", &key); err != nil {
		return nil, err
	}
	return starlark.String(os.Getenv(key)), nil
}

func builtinLog(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var msg string
	if err := starlark.UnpackArgs("log", args, kwargs, "msg", &msg); err != nil {
		return nil, err
	}
	fmt.Fprintf(os.Stderr, "[script] %s\n", msg)
	return starlark.None, nil
}

func builtinJSONEncode(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var val starlark.Value
	if err := starlark.UnpackArgs("json.encode", args, kwargs, "val", &val); err != nil {
		return nil, err
	}
	goVal := starlarkToGo(val)
	data, err := json.Marshal(goVal)
	if err != nil {
		return nil, fmt.Errorf("json.encode: %w", err)
	}
	return starlark.String(string(data)), nil
}

func builtinJSONDecode(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var s string
	if err := starlark.UnpackArgs("json.decode", args, kwargs, "s", &s); err != nil {
		return nil, err
	}
	var goVal any
	if err := json.Unmarshal([]byte(s), &goVal); err != nil {
		return nil, fmt.Errorf("json.decode: %w", err)
	}
	return goToStarlark(goVal)
}

func builtinContinue(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	if err := starlark.UnpackArgs("continue", args, kwargs); err != nil {
		return nil, err
	}
	return &hookResultValue{result: hooks.Result{Action: hooks.ActionContinue}}, nil
}

func builtinBlock(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var reason string
	if err := starlark.UnpackArgs("block", args, kwargs, "reason", &reason); err != nil {
		return nil, err
	}
	return &hookResultValue{result: hooks.Result{Action: hooks.ActionBlock, Reason: reason}}, nil
}

func builtinModify(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var payload starlark.Value
	if err := starlark.UnpackArgs("modify", args, kwargs, "payload", &payload); err != nil {
		return nil, err
	}
	goPayload := starlarkToGo(payload)
	return &hookResultValue{result: hooks.Result{Action: hooks.ActionModify, Payload: goPayload}}, nil
}

func builtinRandom(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var min, max int
	if err := starlark.UnpackArgs("random", args, kwargs, "min", &min, "max", &max); err != nil {
		return nil, err
	}
	if min >= max {
		return nil, fmt.Errorf("random: min must be less than max")
	}
	n := rand.IntN(max-min+1) + min
	return starlark.MakeInt(n), nil
}

func builtinMathAbs(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var x starlark.Value
	if err := starlark.UnpackArgs("math.abs", args, kwargs, "x", &x); err != nil {
		return nil, err
	}
	switch v := x.(type) {
	case starlark.Int:
		i, _ := v.Int64()
		if i < 0 {
			return starlark.MakeInt64(-i), nil
		}
		return v, nil
	case starlark.Float:
		return starlark.Float(math.Abs(float64(v))), nil
	default:
		return nil, fmt.Errorf("math.abs: expected number, got %s", x.Type())
	}
}

func builtinMathMin(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var a, b starlark.Value
	if err := starlark.UnpackArgs("math.min", args, kwargs, "a", &a, "b", &b); err != nil {
		return nil, err
	}
	cmp, err := starlark.Compare(syntax.LT, a, b)
	if err != nil {
		return nil, err
	}
	if cmp {
		return a, nil
	}
	return b, nil
}

func builtinMathMax(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var a, b starlark.Value
	if err := starlark.UnpackArgs("math.max", args, kwargs, "a", &a, "b", &b); err != nil {
		return nil, err
	}
	cmp, err := starlark.Compare(syntax.GT, a, b)
	if err != nil {
		return nil, err
	}
	if cmp {
		return a, nil
	}
	return b, nil
}

func builtinMathFloor(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var x starlark.Float
	if err := starlark.UnpackArgs("math.floor", args, kwargs, "x", &x); err != nil {
		return nil, err
	}
	return starlark.MakeInt64(int64(math.Floor(float64(x)))), nil
}

func builtinMathCeil(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var x starlark.Float
	if err := starlark.UnpackArgs("math.ceil", args, kwargs, "x", &x); err != nil {
		return nil, err
	}
	return starlark.MakeInt64(int64(math.Ceil(float64(x)))), nil
}

// --- Filesystem built-ins ---

func builtinFsRead(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var path string
	if err := starlark.UnpackArgs("fs.read", args, kwargs, "path", &path); err != nil {
		return nil, err
	}
	path = sanitizePath(path)
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("fs.read: %w", err)
	}
	return starlark.String(string(data)), nil
}

func builtinFsWrite(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var path, content string
	if err := starlark.UnpackArgs("fs.write", args, kwargs, "path", &path, "content", &content); err != nil {
		return nil, err
	}
	path = sanitizePath(path)
	// Ensure parent directory exists
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("fs.write: mkdir: %w", err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		return nil, fmt.Errorf("fs.write: %w", err)
	}
	return starlark.String("written: " + path), nil
}

func builtinFsAppend(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var path, content string
	if err := starlark.UnpackArgs("fs.append", args, kwargs, "path", &path, "content", &content); err != nil {
		return nil, err
	}
	path = sanitizePath(path)
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return nil, fmt.Errorf("fs.append: %w", err)
	}
	defer f.Close()
	if _, err := f.WriteString(content); err != nil {
		return nil, fmt.Errorf("fs.append: %w", err)
	}
	return starlark.String("appended to: " + path), nil
}

func builtinFsExists(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var path string
	if err := starlark.UnpackArgs("fs.exists", args, kwargs, "path", &path); err != nil {
		return nil, err
	}
	path = sanitizePath(path)
	_, err := os.Stat(path)
	if err == nil {
		return starlark.True, nil
	}
	if os.IsNotExist(err) {
		return starlark.False, nil
	}
	return nil, fmt.Errorf("fs.exists: %w", err)
}

func builtinFsRemove(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var path string
	if err := starlark.UnpackArgs("fs.remove", args, kwargs, "path", &path); err != nil {
		return nil, err
	}
	path = sanitizePath(path)
	if err := os.Remove(path); err != nil {
		return nil, fmt.Errorf("fs.remove: %w", err)
	}
	return starlark.String("removed: " + path), nil
}

func builtinFsMkdir(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var path string
	if err := starlark.UnpackArgs("fs.mkdir", args, kwargs, "path", &path); err != nil {
		return nil, err
	}
	path = sanitizePath(path)
	if err := os.MkdirAll(path, 0755); err != nil {
		return nil, fmt.Errorf("fs.mkdir: %w", err)
	}
	return starlark.String("created: " + path), nil
}

func builtinFsList(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var path string
	if err := starlark.UnpackArgs("fs.list", args, kwargs, "path", &path); err != nil {
		return nil, err
	}
	path = sanitizePath(path)
	entries, err := os.ReadDir(path)
	if err != nil {
		return nil, fmt.Errorf("fs.list: %w", err)
	}
	items := make([]starlark.Value, 0, len(entries))
	for _, e := range entries {
		items = append(items, starlark.String(e.Name()))
	}
	return starlark.NewList(items), nil
}

func builtinFsStat(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var path string
	if err := starlark.UnpackArgs("fs.stat", args, kwargs, "path", &path); err != nil {
		return nil, err
	}
	path = sanitizePath(path)
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("fs.stat: %w", err)
	}
	return starlarkstruct.FromStringDict(starlarkstruct.Default, starlark.StringDict{
		"name":    starlark.String(info.Name()),
		"size":    starlark.MakeInt64(info.Size()),
		"is_dir":  starlark.Bool(info.IsDir()),
		"mode":    starlark.String(info.Mode().String()),
		"mod_time": starlark.String(info.ModTime().Format(time.RFC3339)),
	}), nil
}

// sanitizePath cleans a file path and prevents traversal above cwd.
func sanitizePath(path string) string {
	// Clean the path
	path = filepath.Clean(path)
	// If relative, resolve against cwd
	if !filepath.IsAbs(path) {
		cwd, err := os.Getwd()
		if err == nil {
			path = filepath.Join(cwd, path)
		}
	}
	// Block traversal: ensure path doesn't escape cwd
	cwd, _ := os.Getwd()
	if cwd != "" && !strings.HasPrefix(path, cwd) {
		// If it tries to go above cwd, jail it
		rel, err := filepath.Rel(cwd, path)
		if err != nil || strings.HasPrefix(rel, "..") {
			return filepath.Join(cwd, filepath.Base(path))
		}
	}
	return path
}

// --- File editing built-ins ---

// fs.replace(path, old, new) — find and replace exact string occurrence.
// Returns error if old_str not found or found multiple times (surgical edit).
func builtinFsReplace(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var path, oldStr, newStr string
	if err := starlark.UnpackArgs("fs.replace", args, kwargs, "path", &path, "old", &oldStr, "new", &newStr); err != nil {
		return nil, err
	}
	path = sanitizePath(path)
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("fs.replace: %w", err)
	}
	content := string(data)
	count := strings.Count(content, oldStr)
	if count == 0 {
		return nil, fmt.Errorf("fs.replace: old string not found in %s", filepath.Base(path))
	}
	if count > 1 {
		return nil, fmt.Errorf("fs.replace: old string found %d times in %s (must be unique)", count, filepath.Base(path))
	}
	content = strings.Replace(content, oldStr, newStr, 1)
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		return nil, fmt.Errorf("fs.replace: write: %w", err)
	}
	return starlark.String("replaced in: " + filepath.Base(path)), nil
}

// fs.replace_all(path, old, new) — replace ALL occurrences.
func builtinFsReplaceAll(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var path, oldStr, newStr string
	if err := starlark.UnpackArgs("fs.replace_all", args, kwargs, "path", &path, "old", &oldStr, "new", &newStr); err != nil {
		return nil, err
	}
	path = sanitizePath(path)
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("fs.replace_all: %w", err)
	}
	content := string(data)
	count := strings.Count(content, oldStr)
	if count == 0 {
		return nil, fmt.Errorf("fs.replace_all: old string not found in %s", filepath.Base(path))
	}
	content = strings.ReplaceAll(content, oldStr, newStr)
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		return nil, fmt.Errorf("fs.replace_all: write: %w", err)
	}
	return starlark.String(fmt.Sprintf("replaced %d occurrences in: %s", count, filepath.Base(path))), nil
}

// fs.read_lines(path, start, end) — read lines [start, end] (1-indexed, inclusive).
// If end is 0 or -1, reads to end of file.
func builtinFsReadLines(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var path string
	var start, end int
	if err := starlark.UnpackArgs("fs.read_lines", args, kwargs, "path", &path, "start", &start, "end?", &end); err != nil {
		return nil, err
	}
	path = sanitizePath(path)
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("fs.read_lines: %w", err)
	}
	lines := strings.Split(string(data), "\n")
	if start < 1 {
		start = 1
	}
	if end <= 0 || end > len(lines) {
		end = len(lines)
	}
	if start > len(lines) {
		return starlark.String(""), nil
	}
	// Build numbered output like view tool does
	var result strings.Builder
	for i := start - 1; i < end; i++ {
		fmt.Fprintf(&result, "%d. %s\n", i+1, lines[i])
	}
	return starlark.String(result.String()), nil
}

// fs.insert_at(path, line, content) — insert content BEFORE the given line number.
// Line is 1-indexed. If line > total lines, appends to end.
func builtinFsInsertAt(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var path, content string
	var lineNum int
	if err := starlark.UnpackArgs("fs.insert_at", args, kwargs, "path", &path, "line", &lineNum, "content", &content); err != nil {
		return nil, err
	}
	path = sanitizePath(path)
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("fs.insert_at: %w", err)
	}
	lines := strings.Split(string(data), "\n")
	newLines := strings.Split(content, "\n")

	if lineNum < 1 {
		lineNum = 1
	}
	idx := lineNum - 1
	if idx > len(lines) {
		idx = len(lines)
	}

	// Insert newLines at idx
	result := make([]string, 0, len(lines)+len(newLines))
	result = append(result, lines[:idx]...)
	result = append(result, newLines...)
	result = append(result, lines[idx:]...)

	if err := os.WriteFile(path, []byte(strings.Join(result, "\n")), 0644); err != nil {
		return nil, fmt.Errorf("fs.insert_at: write: %w", err)
	}
	return starlark.String(fmt.Sprintf("inserted %d lines at line %d", len(newLines), lineNum)), nil
}

// fs.replace_lines(path, start, end, content) — replace lines [start, end] (1-indexed, inclusive)
// with the given content. If content is "", deletes those lines.
func builtinFsReplaceLines(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var path, content string
	var start, end int
	if err := starlark.UnpackArgs("fs.replace_lines", args, kwargs, "path", &path, "start", &start, "end", &end, "content", &content); err != nil {
		return nil, err
	}
	path = sanitizePath(path)
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("fs.replace_lines: %w", err)
	}
	lines := strings.Split(string(data), "\n")

	if start < 1 {
		start = 1
	}
	if end > len(lines) {
		end = len(lines)
	}
	if start > end {
		return nil, fmt.Errorf("fs.replace_lines: start (%d) > end (%d)", start, end)
	}

	var newLines []string
	if content != "" {
		newLines = strings.Split(content, "\n")
	}

	result := make([]string, 0, len(lines)-((end-start)+1)+len(newLines))
	result = append(result, lines[:start-1]...)
	result = append(result, newLines...)
	result = append(result, lines[end:]...)

	if err := os.WriteFile(path, []byte(strings.Join(result, "\n")), 0644); err != nil {
		return nil, fmt.Errorf("fs.replace_lines: write: %w", err)
	}
	removed := end - start + 1
	return starlark.String(fmt.Sprintf("replaced lines %d-%d (%d lines) with %d lines", start, end, removed, len(newLines))), nil
}

// fs.delete_lines(path, start, end) — delete lines [start, end] (1-indexed, inclusive).
func builtinFsDeleteLines(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var path string
	var start, end int
	if err := starlark.UnpackArgs("fs.delete_lines", args, kwargs, "path", &path, "start", &start, "end", &end); err != nil {
		return nil, err
	}
	path = sanitizePath(path)
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("fs.delete_lines: %w", err)
	}
	lines := strings.Split(string(data), "\n")

	if start < 1 {
		start = 1
	}
	if end > len(lines) {
		end = len(lines)
	}
	if start > end {
		return nil, fmt.Errorf("fs.delete_lines: start (%d) > end (%d)", start, end)
	}

	result := make([]string, 0, len(lines)-(end-start+1))
	result = append(result, lines[:start-1]...)
	result = append(result, lines[end:]...)

	if err := os.WriteFile(path, []byte(strings.Join(result, "\n")), 0644); err != nil {
		return nil, fmt.Errorf("fs.delete_lines: write: %w", err)
	}
	return starlark.String(fmt.Sprintf("deleted lines %d-%d (%d lines)", start, end, end-start+1)), nil
}

// fs.line_count(path) — return number of lines in a file.
func builtinFsLineCount(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var path string
	if err := starlark.UnpackArgs("fs.line_count", args, kwargs, "path", &path); err != nil {
		return nil, err
	}
	path = sanitizePath(path)
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("fs.line_count: %w", err)
	}
	lines := strings.Split(string(data), "\n")
	return starlark.MakeInt(len(lines)), nil
}

// fs.find(path, pattern) — find all lines containing pattern, returns list of {line_num, text}.
func builtinFsFind(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var path, pattern string
	if err := starlark.UnpackArgs("fs.find", args, kwargs, "path", &path, "pattern", &pattern); err != nil {
		return nil, err
	}
	path = sanitizePath(path)
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("fs.find: %w", err)
	}
	lines := strings.Split(string(data), "\n")
	var matches []starlark.Value
	for i, line := range lines {
		if strings.Contains(line, pattern) {
			entry := starlarkstruct.FromStringDict(starlarkstruct.Default, starlark.StringDict{
				"line_num": starlark.MakeInt(i + 1),
				"text":     starlark.String(line),
			})
			matches = append(matches, entry)
		}
	}
	return starlark.NewList(matches), nil
}
