package scripting

import (
	"context"
	crand "crypto/rand"
	"fmt"
	"net/url"
	"os"
	"runtime"
	"strings"
	"time"

	"go.starlark.net/starlark"
)

const threadContextKey = "context"

// CompileConditionalHookScript compiles a hook script with an optional Starlark when expression.
func (e *Engine) CompileConditionalHookScript(name, when, script string) (*HookRunner, error) {
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

	var whenFn starlark.Callable
	when = strings.TrimSpace(when)
	if when != "" {
		predeclared := make(starlark.StringDict, len(e.builtins)+len(globals))
		for key, value := range e.builtins {
			predeclared[key] = value
		}
		for key, value := range globals {
			predeclared[key] = value
		}

		whenGlobals, err := starlark.ExecFile(
			&starlark.Thread{Name: name + "-when"},
			name+".when.star",
			"def __when__(event, payload):\n    return ("+when+")\n",
			predeclared,
		)
		if err != nil {
			return nil, fmt.Errorf("compile hook condition %q: %w", name, err)
		}

		compiledWhen, ok := whenGlobals["__when__"]
		if !ok {
			return nil, fmt.Errorf("hook condition %q did not produce a callable", name)
		}
		whenFn, ok = compiledWhen.(starlark.Callable)
		if !ok {
			return nil, fmt.Errorf("hook condition %q: __when__ must be callable", name)
		}
	}

	return &HookRunner{
		name:     name,
		globals:  globals,
		builtins: e.builtins,
		whenFn:   whenFn,
	}, nil
}

func builtinOSCwd(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	if err := starlark.UnpackArgs("os.cwd", args, kwargs); err != nil {
		return nil, err
	}
	cwd, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("os.cwd: %w", err)
	}
	return starlark.String(cwd), nil
}

func builtinOSHostname(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	if err := starlark.UnpackArgs("os.hostname", args, kwargs); err != nil {
		return nil, err
	}
	hostname, err := os.Hostname()
	if err != nil {
		return nil, fmt.Errorf("os.hostname: %w", err)
	}
	return starlark.String(hostname), nil
}

func builtinOSPlatform(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	if err := starlark.UnpackArgs("os.platform", args, kwargs); err != nil {
		return nil, err
	}
	return starlark.String(runtime.GOOS), nil
}

func builtinOSArgs(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	if err := starlark.UnpackArgs("os.args", args, kwargs); err != nil {
		return nil, err
	}
	items := make([]starlark.Value, 0, len(os.Args))
	for _, arg := range os.Args {
		items = append(items, starlark.String(arg))
	}
	return starlark.NewList(items), nil
}

func builtinURLParse(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var raw string
	if err := starlark.UnpackArgs("url.parse", args, kwargs, "s", &raw); err != nil {
		return nil, err
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("url.parse: %w", err)
	}

	query := make(map[string]any, len(parsed.Query()))
	for key, values := range parsed.Query() {
		items := make([]any, 0, len(values))
		for _, value := range values {
			items = append(items, value)
		}
		query[key] = items
	}

	result := map[string]any{
		"scheme":    parsed.Scheme,
		"host":      parsed.Host,
		"hostname":  parsed.Hostname(),
		"port":      parsed.Port(),
		"path":      parsed.Path,
		"raw_query": parsed.RawQuery,
		"fragment":  parsed.Fragment,
		"query":     query,
		"string":    parsed.String(),
	}
	if parsed.User != nil {
		result["username"] = parsed.User.Username()
		if password, ok := parsed.User.Password(); ok {
			result["password"] = password
		}
	}
	return goToStarlark(result)
}

func builtinURLEncode(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var params starlark.Value
	if err := starlark.UnpackArgs("url.encode", args, kwargs, "params", &params); err != nil {
		return nil, err
	}

	dict, ok := params.(*starlark.Dict)
	if !ok {
		return nil, fmt.Errorf("url.encode: params must be a dict, got %s", params.Type())
	}

	values := url.Values{}
	for _, item := range dict.Items() {
		key, ok := item[0].(starlark.String)
		if !ok {
			return nil, fmt.Errorf("url.encode: parameter names must be strings")
		}
		if err := addURLValues(values, string(key), item[1]); err != nil {
			return nil, err
		}
	}
	return starlark.String(values.Encode()), nil
}

func addURLValues(values url.Values, key string, value starlark.Value) error {
	if value == nil || value == starlark.None {
		return nil
	}

	switch typed := value.(type) {
	case *starlark.List:
		iter := typed.Iterate()
		defer iter.Done()
		var item starlark.Value
		for iter.Next(&item) {
			values.Add(key, starlarkToString(item))
		}
	case starlark.Tuple:
		for _, item := range typed {
			values.Add(key, starlarkToString(item))
		}
	default:
		values.Add(key, starlarkToString(value))
	}
	return nil
}

func builtinUUIDV4(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	if err := starlark.UnpackArgs("uuid.v4", args, kwargs); err != nil {
		return nil, err
	}
	var buf [16]byte
	if _, err := crand.Read(buf[:]); err != nil {
		return nil, fmt.Errorf("uuid.v4: %w", err)
	}
	buf[6] = (buf[6] & 0x0f) | 0x40
	buf[8] = (buf[8] & 0x3f) | 0x80
	return starlark.String(fmt.Sprintf(
		"%08x-%04x-%04x-%04x-%012x",
		buf[0:4],
		buf[4:6],
		buf[6:8],
		buf[8:10],
		buf[10:16],
	)), nil
}

func builtinSleep(thread *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var millis int
	if err := starlark.UnpackArgs("sleep", args, kwargs, "ms", &millis); err != nil {
		return nil, err
	}
	if millis < 0 {
		return nil, fmt.Errorf("sleep: ms must be >= 0")
	}

	duration := time.Duration(millis) * time.Millisecond
	if ctx, ok := thread.Local(threadContextKey).(context.Context); ok && ctx != nil {
		timer := time.NewTimer(duration)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("sleep: %w", ctx.Err())
		case <-timer.C:
		}
	} else {
		time.Sleep(duration)
	}
	return starlark.None, nil
}
