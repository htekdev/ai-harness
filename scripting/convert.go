package scripting

import (
	"encoding/json"
	"fmt"

	"go.starlark.net/starlark"
)

// jsonToStarlark converts a JSON RawMessage into a Starlark dict.
func jsonToStarlark(data json.RawMessage) (starlark.Value, error) {
	if len(data) == 0 || string(data) == "null" {
		return starlark.NewDict(0), nil
	}

	var goVal any
	if err := json.Unmarshal(data, &goVal); err != nil {
		return nil, fmt.Errorf("unmarshal json: %w", err)
	}
	return goToStarlark(goVal)
}

// goToStarlark converts a Go value to a Starlark value.
func goToStarlark(v any) (starlark.Value, error) {
	switch val := v.(type) {
	case nil:
		return starlark.None, nil
	case bool:
		return starlark.Bool(val), nil
	case float64:
		if val == float64(int64(val)) {
			return starlark.MakeInt64(int64(val)), nil
		}
		return starlark.Float(val), nil
	case int:
		return starlark.MakeInt(val), nil
	case int64:
		return starlark.MakeInt64(val), nil
	case string:
		return starlark.String(val), nil
	case []any:
		list := make([]starlark.Value, 0, len(val))
		for _, item := range val {
			sv, err := goToStarlark(item)
			if err != nil {
				return nil, err
			}
			list = append(list, sv)
		}
		return starlark.NewList(list), nil
	case map[string]any:
		dict := starlark.NewDict(len(val))
		for k, v := range val {
			sv, err := goToStarlark(v)
			if err != nil {
				return nil, err
			}
			if err := dict.SetKey(starlark.String(k), sv); err != nil {
				return nil, err
			}
		}
		return dict, nil
	default:
		// For structs and pointers, try JSON round-trip to get a map
		data, err := json.Marshal(val)
		if err != nil {
			return starlark.String(fmt.Sprintf("%v", val)), nil
		}
		var m any
		if err := json.Unmarshal(data, &m); err != nil {
			return starlark.String(string(data)), nil
		}
		return goToStarlark(m)
	}
}

// starlarkToGo converts a Starlark value to a Go value.
func starlarkToGo(v starlark.Value) any {
	switch val := v.(type) {
	case starlark.NoneType:
		return nil
	case starlark.Bool:
		return bool(val)
	case starlark.Int:
		if i, ok := val.Int64(); ok {
			return i
		}
		return val.String()
	case starlark.Float:
		return float64(val)
	case starlark.String:
		return string(val)
	case *starlark.List:
		result := make([]any, val.Len())
		for i := 0; i < val.Len(); i++ {
			result[i] = starlarkToGo(val.Index(i))
		}
		return result
	case *starlark.Dict:
		result := make(map[string]any)
		for _, item := range val.Items() {
			key := starlarkToGo(item[0])
			value := starlarkToGo(item[1])
			result[fmt.Sprintf("%v", key)] = value
		}
		return result
	case *scriptSet:
		return val.toGoSlice()
	case starlark.Tuple:
		result := make([]any, len(val))
		for i, item := range val {
			result[i] = starlarkToGo(item)
		}
		return result
	default:
		return val.String()
	}
}

// starlarkToString converts a Starlark value to a string result.
func starlarkToString(v starlark.Value) string {
	switch val := v.(type) {
	case starlark.NoneType:
		return ""
	case starlark.String:
		return string(val)
	default:
		return val.String()
	}
}
