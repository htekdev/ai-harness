// Package scripting provides a Starlark-based scripting engine for defining
// tool handlers and hook handlers inline in YAML configuration.
package scripting

import (
	"context"
	"crypto/hmac"
	"crypto/md5"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math"
	mrand "math/rand/v2"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"go.starlark.net/starlark"
	"go.starlark.net/starlarkstruct"
	"go.starlark.net/syntax"

	"github.com/htekdev/ai-harness/hooks"
)

// Engine manages Starlark script compilation and execution.
type Engine struct {
	mu       sync.Mutex
	builtins starlark.StringDict
	meta     *MetaContext

	cacheMu   sync.RWMutex
	cache     map[string]any
	metricsMu sync.RWMutex
	metrics   map[string]int64

	// sandboxMu guards sandbox; it may be replaced at runtime (e.g. by a
	// future config hot-reload). The http.* built-ins read it on every
	// request so updates take effect for subsequent calls.
	sandboxMu sync.RWMutex
	sandbox   *NetworkSandbox
}

// NewEngine creates a new scripting engine with built-in modules.
func NewEngine() *Engine {
	e := &Engine{
		cache:   make(map[string]any),
		metrics: make(map[string]int64),
	}
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

	osMod := starlarkstruct.FromStringDict(starlarkstruct.Default, starlark.StringDict{
		"cwd":      starlark.NewBuiltin("os.cwd", builtinOSCwd),
		"hostname": starlark.NewBuiltin("os.hostname", builtinOSHostname),
		"platform": starlark.NewBuiltin("os.platform", builtinOSPlatform),
		"args":     starlark.NewBuiltin("os.args", builtinOSArgs),
	})

	urlMod := starlarkstruct.FromStringDict(starlarkstruct.Default, starlark.StringDict{
		"parse":  starlark.NewBuiltin("url.parse", builtinURLParse),
		"encode": starlark.NewBuiltin("url.encode", builtinURLEncode),
	})

	uuidMod := starlarkstruct.FromStringDict(starlarkstruct.Default, starlark.StringDict{
		"v4": starlark.NewBuiltin("uuid.v4", builtinUUIDV4),
	})

	httpMod := starlarkstruct.FromStringDict(starlarkstruct.Default, starlark.StringDict{
		"get":  starlark.NewBuiltin("http.get", e.builtinHTTPGet),
		"post": starlark.NewBuiltin("http.post", e.builtinHTTPPost),
	})

	reMod := starlarkstruct.FromStringDict(starlarkstruct.Default, starlark.StringDict{
		"match":    starlark.NewBuiltin("re.match", builtinRegexMatch),
		"find_all": starlark.NewBuiltin("re.find_all", builtinRegexFindAll),
		"replace":  starlark.NewBuiltin("re.replace", builtinRegexReplace),
	})

	hashMod := starlarkstruct.FromStringDict(starlarkstruct.Default, starlark.StringDict{
		"sha256": starlark.NewBuiltin("hash.sha256", builtinHashSHA256),
		"md5":    starlark.NewBuiltin("hash.md5", builtinHashMD5),
	})

	base64Mod := starlarkstruct.FromStringDict(starlarkstruct.Default, starlark.StringDict{
		"encode": starlark.NewBuiltin("base64.encode", builtinBase64Encode),
		"decode": starlark.NewBuiltin("base64.decode", builtinBase64Decode),
	})

	cryptoMod := starlarkstruct.FromStringDict(starlarkstruct.Default, starlark.StringDict{
		"hmac_sha256": starlark.NewBuiltin("crypto.hmac_sha256", builtinCryptoHMACSHA256),
	})

	stringMod := starlarkstruct.FromStringDict(starlarkstruct.Default, starlark.StringDict{
		"upper":     starlark.NewBuiltin("string.upper", builtinStringUpper),
		"lower":     starlark.NewBuiltin("string.lower", builtinStringLower),
		"trim":      starlark.NewBuiltin("string.trim", builtinStringTrim),
		"split":     starlark.NewBuiltin("string.split", builtinStringSplit),
		"join":      starlark.NewBuiltin("string.join", builtinStringJoin),
		"truncate":  starlark.NewBuiltin("string.truncate", builtinStringTruncate),
		"pad_left":  starlark.NewBuiltin("string.pad_left", builtinStringPadLeft),
		"pad_right": starlark.NewBuiltin("string.pad_right", builtinStringPadRight),
	})

	templateMod := templateModule()
	validateMod := validateModule()
	setMod := setModule()

	cacheMod := starlarkstruct.FromStringDict(starlarkstruct.Default, starlark.StringDict{
		"set":    starlark.NewBuiltin("cache.set", e.builtinCacheSet),
		"get":    starlark.NewBuiltin("cache.get", e.builtinCacheGet),
		"has":    starlark.NewBuiltin("cache.has", e.builtinCacheHas),
		"delete": starlark.NewBuiltin("cache.delete", e.builtinCacheDelete),
		"clear":  starlark.NewBuiltin("cache.clear", e.builtinCacheClear),
	})

	metricsMod := starlarkstruct.FromStringDict(starlarkstruct.Default, starlark.StringDict{
		"incr":     starlark.NewBuiltin("metrics.incr", e.builtinMetricsIncr),
		"get":      starlark.NewBuiltin("metrics.get", e.builtinMetricsGet),
		"reset":    starlark.NewBuiltin("metrics.reset", e.builtinMetricsReset),
		"snapshot": starlark.NewBuiltin("metrics.snapshot", e.builtinMetricsSnapshot),
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
		"glob":          starlark.NewBuiltin("fs.glob", builtinFsGlob),
		"copy":          starlark.NewBuiltin("fs.copy", builtinFsCopy),
		"move":          starlark.NewBuiltin("fs.move", builtinFsMove),
		"diff":          starlark.NewBuiltin("fs.diff", builtinFsDiff),
		"replace":       starlark.NewBuiltin("fs.replace", builtinFsReplace),
		"replace_all":   starlark.NewBuiltin("fs.replace_all", builtinFsReplaceAll),
		"read_lines":    starlark.NewBuiltin("fs.read_lines", builtinFsReadLines),
		"insert_at":     starlark.NewBuiltin("fs.insert_at", builtinFsInsertAt),
		"replace_lines": starlark.NewBuiltin("fs.replace_lines", builtinFsReplaceLines),
		"delete_lines":  starlark.NewBuiltin("fs.delete_lines", builtinFsDeleteLines),
		"line_count":    starlark.NewBuiltin("fs.line_count", builtinFsLineCount),
		"find":          starlark.NewBuiltin("fs.find", builtinFsFind),
	})

	result := starlark.StringDict{
		"time":     timeMod,
		"json":     jsonMod,
		"math":     mathMod,
		"os":       osMod,
		"url":      urlMod,
		"uuid":     uuidMod,
		"http":     httpMod,
		"re":       reMod,
		"hash":     hashMod,
		"base64":   base64Mod,
		"crypto":   cryptoMod,
		"string":   stringMod,
		"template": templateMod,
		"validate": validateMod,
		"set":      setMod,
		"cache":    cacheMod,
		"metrics":  metricsMod,
		"fs":       fsMod,
		"ctx":      ctxModule(),
		"exec":     execModule(),
		"parallel": asyncModule(),
		"env":      starlark.NewBuiltin("env", builtinEnv),
		"log":      starlark.NewBuiltin("log", builtinLog),
		"assert":   starlark.NewBuiltin("assert", builtinAssert),
		"allow":    starlark.NewBuiltin("allow", builtinContinue),
		"block":    starlark.NewBuiltin("block", builtinBlock),
		"modify":   starlark.NewBuiltin("modify", builtinModify),
		"emit":     starlark.NewBuiltin("emit", builtinEmit),
		"random":   starlark.NewBuiltin("random", builtinRandom),
		"sleep":    starlark.NewBuiltin("sleep", builtinSleep),
	}

	// Add meta module if configured.
	if e.meta != nil {
		result["meta"] = e.makeMetaModule()
	}

	return result
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
	return e.CompileConditionalHookScript(name, "", script)
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
	thread.SetLocal(threadContextKey, ctx)

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
	whenFn   starlark.Callable
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
	thread.SetLocal(threadContextKey, ctx)
	callArgs := starlark.Tuple{
		starlark.String(event),
		payloadVal,
	}
	if hr.whenFn != nil {
		allowed, err := starlark.Call(thread, hr.whenFn, callArgs, nil)
		if err != nil {
			return hooks.Result{
				Action: hooks.ActionContinue,
				Reason: fmt.Sprintf("hook %q when condition error: %v", hr.name, err),
			}
		}
		if !allowed.Truth() {
			return hooks.Result{Action: hooks.ActionContinue}
		}
	}

	result, err := starlark.Call(thread, handleFn, callArgs, nil)
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

	// Also handle dict-style returns: {"action": "block", "reason": "..."}
	// This is the ergonomic path for scripts that return plain dicts.
	if dict, ok := val.(*starlark.Dict); ok {
		actionVal, found, _ := dict.Get(starlark.String("action"))
		if found {
			action := ""
			if s, ok := actionVal.(starlark.String); ok {
				action = string(s)
			}
			reason := ""
			if reasonVal, found, _ := dict.Get(starlark.String("reason")); found {
				if s, ok := reasonVal.(starlark.String); ok {
					reason = string(s)
				}
			}
			switch action {
			case "block":
				return hooks.Result{Action: hooks.ActionBlock, Reason: reason}
			case "modify":
				payloadVal, found, _ := dict.Get(starlark.String("payload"))
				if found {
					return hooks.Result{Action: hooks.ActionModify, Payload: starlarkToGo(payloadVal)}
				}
				return hooks.Result{Action: hooks.ActionModify, Payload: starlarkToGo(dict)}
			default:
				return hooks.Result{Action: hooks.ActionContinue}
			}
		}
	}

	return hooks.Result{Action: hooks.ActionContinue}
}

// hookResultValue is a Starlark value wrapping a hooks.Result.
type hookResultValue struct {
	result hooks.Result
}

func (h *hookResultValue) String() string        { return fmt.Sprintf("HookResult(%d)", h.result.Action) }
func (h *hookResultValue) Type() string          { return "HookResult" }
func (h *hookResultValue) Freeze()               {}
func (h *hookResultValue) Truth() starlark.Bool  { return starlark.True }
func (h *hookResultValue) Hash() (uint32, error) { return 0, fmt.Errorf("unhashable") }

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
	n := mrand.IntN(max-min+1) + min
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

// SetNetworkSandbox attaches (or replaces) the network sandbox enforced
// by the http.* Starlark built-ins. Passing nil — or a sandbox with no
// allowed domains — disables sandboxing (back-compat default). Safe for
// concurrent use; updates take effect for subsequent http calls.
func (e *Engine) SetNetworkSandbox(s *NetworkSandbox) {
	e.sandboxMu.Lock()
	e.sandbox = s
	e.sandboxMu.Unlock()
}

// NetworkSandbox returns the currently attached sandbox, or nil.
func (e *Engine) NetworkSandbox() *NetworkSandbox {
	e.sandboxMu.RLock()
	defer e.sandboxMu.RUnlock()
	return e.sandbox
}

func (e *Engine) builtinHTTPGet(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var rawURL string
	var headersVal starlark.Value = starlark.None
	var timeoutSeconds int = 30
	if err := starlark.UnpackArgs("http.get", args, kwargs, "url", &rawURL, "headers?", &headersVal, "timeout_seconds?", &timeoutSeconds); err != nil {
		return nil, err
	}
	return e.doHTTPRequest("GET", rawURL, "", headersVal, timeoutSeconds)
}

func (e *Engine) builtinHTTPPost(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var rawURL string
	var body string
	var headersVal starlark.Value = starlark.None
	var timeoutSeconds int = 30
	if err := starlark.UnpackArgs("http.post", args, kwargs, "url", &rawURL, "body?", &body, "headers?", &headersVal, "timeout_seconds?", &timeoutSeconds); err != nil {
		return nil, err
	}
	return e.doHTTPRequest("POST", rawURL, body, headersVal, timeoutSeconds)
}

func (e *Engine) doHTTPRequest(method, rawURL, body string, headersVal starlark.Value, timeoutSeconds int) (starlark.Value, error) {
	if sb := e.NetworkSandbox(); sb != nil {
		if err := sb.Allow(rawURL); err != nil {
			return nil, fmt.Errorf("http.%s: %w", strings.ToLower(method), err)
		}
	}

	headers, err := starlarkValueToStringMap(headersVal)
	if err != nil {
		return nil, fmt.Errorf("http.%s: %w", strings.ToLower(method), err)
	}
	if timeoutSeconds <= 0 {
		timeoutSeconds = 30
	}

	req, err := http.NewRequest(method, rawURL, strings.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("http.%s: build request: %w", strings.ToLower(method), err)
	}
	for key, value := range headers {
		req.Header.Set(key, value)
	}

	client := &http.Client{Timeout: time.Duration(timeoutSeconds) * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http.%s: %w", strings.ToLower(method), err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("http.%s: read response: %w", strings.ToLower(method), err)
	}

	responseHeaders := make(map[string]any, len(resp.Header))
	for key, values := range resp.Header {
		responseHeaders[key] = strings.Join(values, ", ")
	}

	return goToStarlark(map[string]any{
		"status":  resp.StatusCode,
		"ok":      resp.StatusCode >= 200 && resp.StatusCode < 300,
		"url":     resp.Request.URL.String(),
		"body":    string(respBody),
		"headers": responseHeaders,
	})
}

func builtinRegexMatch(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var pattern, text string
	if err := starlark.UnpackArgs("re.match", args, kwargs, "pattern", &pattern, "text", &text); err != nil {
		return nil, err
	}

	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, fmt.Errorf("re.match: %w", err)
	}
	indices := re.FindStringSubmatchIndex(text)
	if len(indices) == 0 || indices[0] != 0 {
		return goToStarlark(map[string]any{
			"matched":      false,
			"full_match":   "",
			"groups":       []any{},
			"named_groups": map[string]any{},
			"start":        -1,
			"end":          -1,
		})
	}

	groups := make([]any, 0, (len(indices)/2)-1)
	namedGroups := make(map[string]any)
	for i := 2; i < len(indices); i += 2 {
		group := ""
		if indices[i] >= 0 && indices[i+1] >= 0 {
			group = text[indices[i]:indices[i+1]]
		}
		groups = append(groups, group)
		if name := re.SubexpNames()[i/2]; name != "" {
			namedGroups[name] = group
		}
	}

	return goToStarlark(map[string]any{
		"matched":      true,
		"full_match":   text[indices[0]:indices[1]],
		"groups":       groups,
		"named_groups": namedGroups,
		"start":        indices[0],
		"end":          indices[1],
	})
}

func builtinRegexFindAll(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var pattern, text string
	if err := starlark.UnpackArgs("re.find_all", args, kwargs, "pattern", &pattern, "text", &text); err != nil {
		return nil, err
	}

	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, fmt.Errorf("re.find_all: %w", err)
	}

	matches := re.FindAllString(text, -1)
	items := make([]any, len(matches))
	for i, match := range matches {
		items[i] = match
	}
	return goToStarlark(items)
}

func builtinRegexReplace(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var pattern, repl, text string
	if err := starlark.UnpackArgs("re.replace", args, kwargs, "pattern", &pattern, "repl", &repl, "text", &text); err != nil {
		return nil, err
	}

	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, fmt.Errorf("re.replace: %w", err)
	}
	return starlark.String(re.ReplaceAllString(text, repl)), nil
}

func builtinHashSHA256(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var text string
	if err := starlark.UnpackArgs("hash.sha256", args, kwargs, "text", &text); err != nil {
		return nil, err
	}
	sum := sha256.Sum256([]byte(text))
	return starlark.String(hex.EncodeToString(sum[:])), nil
}

func builtinHashMD5(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var text string
	if err := starlark.UnpackArgs("hash.md5", args, kwargs, "text", &text); err != nil {
		return nil, err
	}
	sum := md5.Sum([]byte(text))
	return starlark.String(hex.EncodeToString(sum[:])), nil
}

func builtinBase64Encode(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var text string
	if err := starlark.UnpackArgs("base64.encode", args, kwargs, "s", &text); err != nil {
		return nil, err
	}
	return starlark.String(base64.StdEncoding.EncodeToString([]byte(text))), nil
}

func builtinBase64Decode(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var encoded string
	if err := starlark.UnpackArgs("base64.decode", args, kwargs, "s", &encoded); err != nil {
		return nil, err
	}
	decoded, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, fmt.Errorf("base64.decode: %w", err)
	}
	return starlark.String(string(decoded)), nil
}

func builtinCryptoHMACSHA256(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var key, message string
	if err := starlark.UnpackArgs("crypto.hmac_sha256", args, kwargs, "key", &key, "msg", &message); err != nil {
		return nil, err
	}
	h := hmac.New(sha256.New, []byte(key))
	_, _ = h.Write([]byte(message))
	return starlark.String(hex.EncodeToString(h.Sum(nil))), nil
}

func builtinStringUpper(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var s string
	if err := starlark.UnpackArgs("string.upper", args, kwargs, "s", &s); err != nil {
		return nil, err
	}
	return starlark.String(strings.ToUpper(s)), nil
}

func builtinStringLower(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var s string
	if err := starlark.UnpackArgs("string.lower", args, kwargs, "s", &s); err != nil {
		return nil, err
	}
	return starlark.String(strings.ToLower(s)), nil
}

func builtinStringTrim(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var s string
	if err := starlark.UnpackArgs("string.trim", args, kwargs, "s", &s); err != nil {
		return nil, err
	}
	return starlark.String(strings.TrimSpace(s)), nil
}

func builtinStringSplit(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var s, sep string
	if err := starlark.UnpackArgs("string.split", args, kwargs, "s", &s, "sep", &sep); err != nil {
		return nil, err
	}
	parts := strings.Split(s, sep)
	items := make([]starlark.Value, 0, len(parts))
	for _, part := range parts {
		items = append(items, starlark.String(part))
	}
	return starlark.NewList(items), nil
}

func builtinStringJoin(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var partsVal starlark.Value
	var sep string
	if err := starlark.UnpackArgs("string.join", args, kwargs, "parts", &partsVal, "sep", &sep); err != nil {
		return nil, err
	}
	parts, err := starlarkValueToStrings(partsVal)
	if err != nil {
		return nil, fmt.Errorf("string.join: %w", err)
	}
	return starlark.String(strings.Join(parts, sep)), nil
}

func builtinStringTruncate(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var s string
	var limit int
	if err := starlark.UnpackArgs("string.truncate", args, kwargs, "s", &s, "n", &limit); err != nil {
		return nil, err
	}
	if limit < 0 {
		return nil, fmt.Errorf("string.truncate: n must be >= 0")
	}
	runes := []rune(s)
	if len(runes) <= limit {
		return starlark.String(s), nil
	}
	return starlark.String(string(runes[:limit])), nil
}

func builtinStringPadLeft(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	return builtinStringPad("string.pad_left", true, args, kwargs)
}

func builtinStringPadRight(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	return builtinStringPad("string.pad_right", false, args, kwargs)
}

func builtinStringPad(name string, left bool, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var s string
	var width int
	pad := " "
	if err := starlark.UnpackArgs(name, args, kwargs, "s", &s, "n", &width, "char?", &pad); err != nil {
		return nil, err
	}
	if width < 0 {
		return nil, fmt.Errorf("%s: n must be >= 0", name)
	}
	if utf8.RuneCountInString(pad) != 1 {
		return nil, fmt.Errorf("%s: char must be exactly one character", name)
	}
	currentWidth := utf8.RuneCountInString(s)
	if currentWidth >= width {
		return starlark.String(s), nil
	}
	repeat := strings.Repeat(pad, width-currentWidth)
	if left {
		return starlark.String(repeat + s), nil
	}
	return starlark.String(s + repeat), nil
}

func builtinEmit(thread *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var eventName string
	payload := starlark.Value(starlark.None)
	if err := starlark.UnpackArgs("emit", args, kwargs, "event", &eventName, "payload?", &payload); err != nil {
		return nil, err
	}
	if !hooks.IsCustomEvent(eventName) {
		return nil, fmt.Errorf("emit: event must start with %q", hooks.CustomEventPrefix)
	}
	ctx, _ := thread.Local(threadContextKey).(context.Context)
	dispatcher := hooks.DispatcherFromContext(ctx)
	if dispatcher == nil {
		return nil, fmt.Errorf("emit: hook dispatcher not available")
	}
	result := dispatcher.Dispatch(ctx, hooks.Event(eventName), starlarkToGo(payload))
	if result.Action == hooks.ActionBlock {
		return nil, fmt.Errorf("emit: %s", result.Reason)
	}
	return goToStarlark(result.Payload)
}

func (e *Engine) builtinCacheSet(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var key string
	var value starlark.Value
	if err := starlark.UnpackArgs("cache.set", args, kwargs, "key", &key, "value", &value); err != nil {
		return nil, err
	}

	e.cacheMu.Lock()
	e.cache[key] = starlarkToGo(value)
	e.cacheMu.Unlock()
	return value, nil
}

func (e *Engine) builtinCacheGet(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var key string
	defaultVal := starlark.Value(starlark.None)
	if err := starlark.UnpackArgs("cache.get", args, kwargs, "key", &key, "default?", &defaultVal); err != nil {
		return nil, err
	}

	e.cacheMu.RLock()
	stored, ok := e.cache[key]
	e.cacheMu.RUnlock()
	if !ok {
		return defaultVal, nil
	}
	return goToStarlark(stored)
}

func (e *Engine) builtinCacheHas(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var key string
	if err := starlark.UnpackArgs("cache.has", args, kwargs, "key", &key); err != nil {
		return nil, err
	}

	e.cacheMu.RLock()
	_, ok := e.cache[key]
	e.cacheMu.RUnlock()
	return starlark.Bool(ok), nil
}

func (e *Engine) builtinCacheDelete(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var key string
	if err := starlark.UnpackArgs("cache.delete", args, kwargs, "key", &key); err != nil {
		return nil, err
	}

	e.cacheMu.Lock()
	_, ok := e.cache[key]
	delete(e.cache, key)
	e.cacheMu.Unlock()
	return starlark.Bool(ok), nil
}

func (e *Engine) builtinCacheClear(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	if err := starlark.UnpackArgs("cache.clear", args, kwargs); err != nil {
		return nil, err
	}

	e.cacheMu.Lock()
	e.cache = make(map[string]any)
	e.cacheMu.Unlock()
	return starlark.None, nil
}

func starlarkValueToStringMap(val starlark.Value) (map[string]string, error) {
	if val == nil || val == starlark.None {
		return map[string]string{}, nil
	}

	dict, ok := val.(*starlark.Dict)
	if !ok {
		return nil, fmt.Errorf("expected dict for headers, got %s", val.Type())
	}

	result := make(map[string]string, dict.Len())
	for _, item := range dict.Items() {
		key, ok := item[0].(starlark.String)
		if !ok {
			return nil, fmt.Errorf("header keys must be strings")
		}
		result[string(key)] = starlarkToString(item[1])
	}
	return result, nil
}

func starlarkValueToStrings(val starlark.Value) ([]string, error) {
	if val == nil || val == starlark.None {
		return nil, nil
	}
	switch typed := val.(type) {
	case *starlark.List:
		parts := make([]string, 0, typed.Len())
		iter := typed.Iterate()
		defer iter.Done()
		var item starlark.Value
		for iter.Next(&item) {
			parts = append(parts, starlarkToString(item))
		}
		return parts, nil
	case starlark.Tuple:
		parts := make([]string, 0, len(typed))
		for _, item := range typed {
			parts = append(parts, starlarkToString(item))
		}
		return parts, nil
	default:
		return nil, fmt.Errorf("expected list or tuple, got %s", val.Type())
	}
}

func (e *Engine) builtinMetricsIncr(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var name string
	delta := 1
	if err := starlark.UnpackArgs("metrics.incr", args, kwargs, "name", &name, "delta?", &delta); err != nil {
		return nil, err
	}
	e.metricsMu.Lock()
	e.metrics[name] += int64(delta)
	value := e.metrics[name]
	e.metricsMu.Unlock()
	return starlark.MakeInt64(value), nil
}

func (e *Engine) builtinMetricsGet(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var name string
	if err := starlark.UnpackArgs("metrics.get", args, kwargs, "name", &name); err != nil {
		return nil, err
	}
	e.metricsMu.RLock()
	value := e.metrics[name]
	e.metricsMu.RUnlock()
	return starlark.MakeInt64(value), nil
}

func (e *Engine) builtinMetricsReset(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	name := ""
	if err := starlark.UnpackArgs("metrics.reset", args, kwargs, "name?", &name); err != nil {
		return nil, err
	}
	e.metricsMu.Lock()
	if strings.TrimSpace(name) == "" {
		e.metrics = make(map[string]int64)
	} else {
		delete(e.metrics, name)
	}
	e.metricsMu.Unlock()
	return starlark.None, nil
}

func (e *Engine) builtinMetricsSnapshot(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	if err := starlark.UnpackArgs("metrics.snapshot", args, kwargs); err != nil {
		return nil, err
	}
	e.metricsMu.RLock()
	snapshot := make(map[string]any, len(e.metrics))
	keys := make([]string, 0, len(e.metrics))
	for key := range e.metrics {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		snapshot[key] = e.metrics[key]
	}
	e.metricsMu.RUnlock()
	return goToStarlark(snapshot)
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
		"name":     starlark.String(info.Name()),
		"size":     starlark.MakeInt64(info.Size()),
		"is_dir":   starlark.Bool(info.IsDir()),
		"mode":     starlark.String(info.Mode().String()),
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
