package compose

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"go.starlark.net/starlark"
	"go.starlark.net/starlarkstruct"
)

// EvaluateCondition evaluates a Starlark condition expression.
func EvaluateCondition(expr string, ctx ConditionContext) (bool, error) {
	if strings.TrimSpace(expr) == "" {
		return true, nil
	}
	if filepath.Clean(ctx.BaseDir) == "." && ctx.BaseDir == "" {
		ctx.BaseDir = "."
	}

	globals := starlark.StringDict{
		"ctx":  makeContextModule(ctx),
		"env":  starlark.NewBuiltin("env", builtinEnv),
		"time": makeTimeModule(),
		"fs":   makeFSModule(ctx.BaseDir),
	}

	value, err := starlark.Eval(&starlark.Thread{Name: "compose-condition"}, "condition.star", expr, globals)
	if err != nil {
		return false, fmt.Errorf("evaluate condition: %w", err)
	}

	result, ok := value.(starlark.Bool)
	if !ok {
		return false, fmt.Errorf("condition did not evaluate to a boolean")
	}
	return bool(result), nil
}

func makeContextModule(ctx ConditionContext) starlark.Value {
	return starlarkstruct.FromStringDict(starlarkstruct.Default, starlark.StringDict{
		"get": starlark.NewBuiltin("ctx.get", func(thread *starlark.Thread, builtin *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
			var key string
			defaultValue := starlark.Value(starlark.None)
			if err := starlark.UnpackArgs("ctx.get", args, kwargs, "key", &key, "default?", &defaultValue); err != nil {
				return nil, err
			}

			if ctx.Values == nil {
				return defaultValue, nil
			}
			value, ok := ctx.Values[key]
			if !ok {
				return defaultValue, nil
			}
			converted, err := toStarlarkValue(value)
			if err != nil {
				return nil, err
			}
			return converted, nil
		}),
	})
}

func makeTimeModule() starlark.Value {
	return starlarkstruct.FromStringDict(starlarkstruct.Default, starlark.StringDict{
		"now": starlark.NewBuiltin("time.now", func(thread *starlark.Thread, builtin *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
			if err := starlark.UnpackArgs("time.now", args, kwargs); err != nil {
				return nil, err
			}
			return starlark.String(time.Now().UTC().Format(time.RFC3339Nano)), nil
		}),
	})
}

func makeFSModule(baseDir string) starlark.Value {
	return starlarkstruct.FromStringDict(starlarkstruct.Default, starlark.StringDict{
		"exists": starlark.NewBuiltin("fs.exists", func(thread *starlark.Thread, builtin *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
			var path string
			if err := starlark.UnpackArgs("fs.exists", args, kwargs, "path", &path); err != nil {
				return nil, err
			}
			resolved := path
			if !filepath.IsAbs(resolved) {
				resolved = filepath.Join(baseDir, resolved)
			}
			_, err := os.Stat(resolved)
			if err == nil {
				return starlark.Bool(true), nil
			}
			if os.IsNotExist(err) {
				return starlark.Bool(false), nil
			}
			return nil, fmt.Errorf("stat %s: %w", resolved, err)
		}),
	})
}

func builtinEnv(thread *starlark.Thread, builtin *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var key string
	if err := starlark.UnpackArgs("env", args, kwargs, "key", &key); err != nil {
		return nil, err
	}
	return starlark.String(os.Getenv(key)), nil
}

func toStarlarkValue(value interface{}) (starlark.Value, error) {
	switch v := value.(type) {
	case nil:
		return starlark.None, nil
	case starlark.Value:
		return v, nil
	case string:
		return starlark.String(v), nil
	case bool:
		return starlark.Bool(v), nil
	case int:
		return starlark.MakeInt(v), nil
	case int8:
		return starlark.MakeInt64(int64(v)), nil
	case int16:
		return starlark.MakeInt64(int64(v)), nil
	case int32:
		return starlark.MakeInt64(int64(v)), nil
	case int64:
		return starlark.MakeInt64(v), nil
	case uint:
		return starlark.MakeUint64(uint64(v)), nil
	case uint8:
		return starlark.MakeUint64(uint64(v)), nil
	case uint16:
		return starlark.MakeUint64(uint64(v)), nil
	case uint32:
		return starlark.MakeUint64(uint64(v)), nil
	case uint64:
		return starlark.MakeUint64(v), nil
	case float32:
		return starlark.Float(v), nil
	case float64:
		return starlark.Float(v), nil
	case []string:
		items := make([]starlark.Value, 0, len(v))
		for _, item := range v {
			items = append(items, starlark.String(item))
		}
		return starlark.NewList(items), nil
	case []interface{}:
		items := make([]starlark.Value, 0, len(v))
		for _, item := range v {
			converted, err := toStarlarkValue(item)
			if err != nil {
				return nil, err
			}
			items = append(items, converted)
		}
		return starlark.NewList(items), nil
	case map[string]interface{}:
		dict := starlark.NewDict(len(v))
		for key, item := range v {
			converted, err := toStarlarkValue(item)
			if err != nil {
				return nil, err
			}
			if err := dict.SetKey(starlark.String(key), converted); err != nil {
				return nil, err
			}
		}
		return dict, nil
	case map[string]string:
		dict := starlark.NewDict(len(v))
		for key, item := range v {
			if err := dict.SetKey(starlark.String(key), starlark.String(item)); err != nil {
				return nil, err
			}
		}
		return dict, nil
	default:
		return nil, fmt.Errorf("unsupported ctx value type %T", value)
	}
}
