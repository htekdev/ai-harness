package scripting

import (
	"encoding/json"
	"fmt"
	"io"
	iofs "io/fs"
	"net/mail"
	neturl "net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/pmezard/go-difflib/difflib"
	"go.starlark.net/starlark"
	"go.starlark.net/starlarkstruct"
)

func templateModule() starlark.Value {
	return starlarkstruct.FromStringDict(starlarkstruct.Default, starlark.StringDict{
		"render": starlark.NewBuiltin("template.render", builtinTemplateRender),
	})
}

func validateModule() starlark.Value {
	return starlarkstruct.FromStringDict(starlarkstruct.Default, starlark.StringDict{
		"email": starlark.NewBuiltin("validate.email", builtinValidateEmail),
		"url":   starlark.NewBuiltin("validate.url", builtinValidateURL),
		"json":  starlark.NewBuiltin("validate.json", builtinValidateJSON),
	})
}

func setModule() starlark.Value {
	return starlarkstruct.FromStringDict(starlarkstruct.Default, starlark.StringDict{
		"new":       starlark.NewBuiltin("set.new", builtinSetNew),
		"contains":  starlark.NewBuiltin("set.contains", builtinSetContains),
		"union":     starlark.NewBuiltin("set.union", builtinSetUnion),
		"intersect": starlark.NewBuiltin("set.intersect", builtinSetIntersect),
		"diff":      starlark.NewBuiltin("set.diff", builtinSetDiff),
		"values":    starlark.NewBuiltin("set.values", builtinSetValues),
		"size":      starlark.NewBuiltin("set.size", builtinSetSize),
	})
}

type scriptSet struct {
	order  []string
	values map[string]starlark.Value
}

func newScriptSet() *scriptSet {
	return &scriptSet{values: make(map[string]starlark.Value)}
}

func (s *scriptSet) String() string {
	return "set(" + starlark.NewList(s.listValues()).String() + ")"
}

func (s *scriptSet) Type() string { return "set" }

func (s *scriptSet) Freeze() {
	for _, value := range s.values {
		value.Freeze()
	}
}

func (s *scriptSet) Truth() starlark.Bool { return starlark.Bool(len(s.order) > 0) }

func (s *scriptSet) Hash() (uint32, error) { return 0, fmt.Errorf("unhashable: set") }

func (s *scriptSet) listValues() []starlark.Value {
	result := make([]starlark.Value, 0, len(s.order))
	for _, key := range s.order {
		result = append(result, s.values[key])
	}
	return result
}

func (s *scriptSet) toGoSlice() []any {
	result := make([]any, 0, len(s.order))
	for _, value := range s.listValues() {
		result = append(result, starlarkToGo(value))
	}
	return result
}

func (s *scriptSet) add(value starlark.Value) error {
	key, err := canonicalSetKey(value)
	if err != nil {
		return err
	}
	if _, exists := s.values[key]; exists {
		return nil
	}
	s.order = append(s.order, key)
	s.values[key] = value
	return nil
}

func canonicalSetKey(value starlark.Value) (string, error) {
	data, err := json.Marshal(starlarkToGo(value))
	if err != nil {
		return "", err
	}
	return value.Type() + ":" + string(data), nil
}

func coerceScriptSet(name string, value starlark.Value) (*scriptSet, error) {
	if existing, ok := value.(*scriptSet); ok {
		return existing, nil
	}
	values, err := iterableValues(name, value)
	if err != nil {
		return nil, err
	}
	result := newScriptSet()
	for _, item := range values {
		if err := result.add(item); err != nil {
			return nil, fmt.Errorf("%s: %w", name, err)
		}
	}
	return result, nil
}

func iterableValues(name string, value starlark.Value) ([]starlark.Value, error) {
	if value == nil || value == starlark.None {
		return []starlark.Value{}, nil
	}
	switch typed := value.(type) {
	case *starlark.List:
		items := make([]starlark.Value, typed.Len())
		for i := 0; i < typed.Len(); i++ {
			items[i] = typed.Index(i)
		}
		return items, nil
	case starlark.Tuple:
		items := make([]starlark.Value, len(typed))
		copy(items, typed)
		return items, nil
	case *scriptSet:
		return typed.listValues(), nil
	case starlark.Iterable:
		iter := typed.Iterate()
		defer iter.Done()
		var item starlark.Value
		var items []starlark.Value
		for iter.Next(&item) {
			items = append(items, item)
		}
		return items, nil
	default:
		return nil, fmt.Errorf("%s: expected iterable, got %s", name, value.Type())
	}
}

func builtinSetNew(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	items := starlark.Value(starlark.None)
	if err := starlark.UnpackArgs("set.new", args, kwargs, "items?", &items); err != nil {
		return nil, err
	}
	result, err := coerceScriptSet("set.new", items)
	if err != nil {
		return nil, err
	}
	return result, nil
}

func builtinSetContains(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	setVal := starlark.Value(starlark.None)
	item := starlark.Value(starlark.None)
	if err := starlark.UnpackArgs("set.contains", args, kwargs, "set", &setVal, "item", &item); err != nil {
		return nil, err
	}
	setObj, err := coerceScriptSet("set.contains", setVal)
	if err != nil {
		return nil, err
	}
	key, err := canonicalSetKey(item)
	if err != nil {
		return nil, fmt.Errorf("set.contains: %w", err)
	}
	_, ok := setObj.values[key]
	return starlark.Bool(ok), nil
}

func builtinSetUnion(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	leftVal := starlark.Value(starlark.None)
	rightVal := starlark.Value(starlark.None)
	if err := starlark.UnpackArgs("set.union", args, kwargs, "a", &leftVal, "b", &rightVal); err != nil {
		return nil, err
	}
	left, err := coerceScriptSet("set.union", leftVal)
	if err != nil {
		return nil, err
	}
	right, err := coerceScriptSet("set.union", rightVal)
	if err != nil {
		return nil, err
	}
	result := newScriptSet()
	for _, item := range left.listValues() {
		if err := result.add(item); err != nil {
			return nil, fmt.Errorf("set.union: %w", err)
		}
	}
	for _, item := range right.listValues() {
		if err := result.add(item); err != nil {
			return nil, fmt.Errorf("set.union: %w", err)
		}
	}
	return result, nil
}

func builtinSetIntersect(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	leftVal := starlark.Value(starlark.None)
	rightVal := starlark.Value(starlark.None)
	if err := starlark.UnpackArgs("set.intersect", args, kwargs, "a", &leftVal, "b", &rightVal); err != nil {
		return nil, err
	}
	left, err := coerceScriptSet("set.intersect", leftVal)
	if err != nil {
		return nil, err
	}
	right, err := coerceScriptSet("set.intersect", rightVal)
	if err != nil {
		return nil, err
	}
	result := newScriptSet()
	for _, item := range left.listValues() {
		key, err := canonicalSetKey(item)
		if err != nil {
			return nil, fmt.Errorf("set.intersect: %w", err)
		}
		if _, ok := right.values[key]; ok {
			if err := result.add(item); err != nil {
				return nil, fmt.Errorf("set.intersect: %w", err)
			}
		}
	}
	return result, nil
}

func builtinSetDiff(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	leftVal := starlark.Value(starlark.None)
	rightVal := starlark.Value(starlark.None)
	if err := starlark.UnpackArgs("set.diff", args, kwargs, "a", &leftVal, "b", &rightVal); err != nil {
		return nil, err
	}
	left, err := coerceScriptSet("set.diff", leftVal)
	if err != nil {
		return nil, err
	}
	right, err := coerceScriptSet("set.diff", rightVal)
	if err != nil {
		return nil, err
	}
	result := newScriptSet()
	for _, item := range left.listValues() {
		key, err := canonicalSetKey(item)
		if err != nil {
			return nil, fmt.Errorf("set.diff: %w", err)
		}
		if _, ok := right.values[key]; ok {
			continue
		}
		if err := result.add(item); err != nil {
			return nil, fmt.Errorf("set.diff: %w", err)
		}
	}
	return result, nil
}

func builtinSetValues(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	setVal := starlark.Value(starlark.None)
	if err := starlark.UnpackArgs("set.values", args, kwargs, "set", &setVal); err != nil {
		return nil, err
	}
	setObj, err := coerceScriptSet("set.values", setVal)
	if err != nil {
		return nil, err
	}
	return starlark.NewList(setObj.listValues()), nil
}

func builtinSetSize(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	setVal := starlark.Value(starlark.None)
	if err := starlark.UnpackArgs("set.size", args, kwargs, "set", &setVal); err != nil {
		return nil, err
	}
	setObj, err := coerceScriptSet("set.size", setVal)
	if err != nil {
		return nil, err
	}
	return starlark.MakeInt(len(setObj.order)), nil
}

var templatePlaceholderPattern = regexp.MustCompile(`\{\{\s*([^{}]+?)\s*\}\}`)

func builtinTemplateRender(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var tmpl string
	varsVal := starlark.Value(starlark.None)
	if err := starlark.UnpackArgs("template.render", args, kwargs, "tmpl", &tmpl, "vars", &varsVal); err != nil {
		return nil, err
	}
	root := starlarkToGo(varsVal)
	rendered, err := renderTemplateString(tmpl, root)
	if err != nil {
		return nil, fmt.Errorf("template.render: %w", err)
	}
	return starlark.String(rendered), nil
}

func renderTemplateString(tmpl string, vars any) (string, error) {
	matches := templatePlaceholderPattern.FindAllStringSubmatchIndex(tmpl, -1)
	if len(matches) == 0 {
		return tmpl, nil
	}
	var result strings.Builder
	last := 0
	for _, match := range matches {
		result.WriteString(tmpl[last:match[0]])
		expr := strings.TrimSpace(tmpl[match[2]:match[3]])
		value, err := resolveTemplatePath(vars, expr)
		if err != nil {
			return "", err
		}
		result.WriteString(stringifyTemplateValue(value))
		last = match[1]
	}
	result.WriteString(tmpl[last:])
	return result.String(), nil
}

func resolveTemplatePath(current any, expr string) (any, error) {
	if strings.TrimSpace(expr) == "" {
		return nil, fmt.Errorf("placeholder cannot be empty")
	}
	parts := strings.Split(expr, ".")
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			return nil, fmt.Errorf("invalid placeholder %q", expr)
		}
		switch typed := current.(type) {
		case map[string]any:
			next, ok := typed[part]
			if !ok {
				return nil, fmt.Errorf("missing value for %q", expr)
			}
			current = next
		case []any:
			idx, err := strconv.Atoi(part)
			if err != nil {
				return nil, fmt.Errorf("%q requires numeric index, got %q", expr, part)
			}
			if idx < 0 || idx >= len(typed) {
				return nil, fmt.Errorf("index %d out of range for %q", idx, expr)
			}
			current = typed[idx]
		default:
			return nil, fmt.Errorf("cannot resolve %q from %T", expr, current)
		}
	}
	return current, nil
}

func stringifyTemplateValue(value any) string {
	switch typed := value.(type) {
	case nil:
		return ""
	case string:
		return typed
	case bool, int, int64, float64:
		return fmt.Sprintf("%v", typed)
	default:
		data, err := json.Marshal(typed)
		if err != nil {
			return fmt.Sprintf("%v", typed)
		}
		return string(data)
	}
}

func builtinValidateEmail(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var value string
	if err := starlark.UnpackArgs("validate.email", args, kwargs, "s", &value); err != nil {
		return nil, err
	}
	trimmed := strings.TrimSpace(value)
	addr, err := mail.ParseAddress(trimmed)
	valid := err == nil && addr.Address == trimmed && !strings.Contains(trimmed, " ")
	return starlark.Bool(valid), nil
}

func builtinValidateURL(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var value string
	if err := starlark.UnpackArgs("validate.url", args, kwargs, "s", &value); err != nil {
		return nil, err
	}
	trimmed := strings.TrimSpace(value)
	parsed, err := neturl.Parse(trimmed)
	valid := err == nil && parsed.Scheme != "" && parsed.Host != ""
	return starlark.Bool(valid), nil
}

func builtinValidateJSON(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var value string
	if err := starlark.UnpackArgs("validate.json", args, kwargs, "s", &value); err != nil {
		return nil, err
	}
	return starlark.Bool(json.Valid([]byte(value))), nil
}

func builtinFsCopy(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var src, dst string
	if err := starlark.UnpackArgs("fs.copy", args, kwargs, "src", &src, "dst", &dst); err != nil {
		return nil, err
	}
	src = sanitizePath(src)
	dst = sanitizePath(dst)
	if src == dst {
		return nil, fmt.Errorf("fs.copy: src and dst must differ")
	}
	if err := copyPath(src, dst); err != nil {
		return nil, fmt.Errorf("fs.copy: %w", err)
	}
	return starlark.String("copied: " + dst), nil
}

func builtinFsMove(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var src, dst string
	if err := starlark.UnpackArgs("fs.move", args, kwargs, "src", &src, "dst", &dst); err != nil {
		return nil, err
	}
	src = sanitizePath(src)
	dst = sanitizePath(dst)
	if src == dst {
		return nil, fmt.Errorf("fs.move: src and dst must differ")
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return nil, fmt.Errorf("fs.move: mkdir: %w", err)
	}
	if err := os.Rename(src, dst); err != nil {
		if copyErr := copyPath(src, dst); copyErr != nil {
			return nil, fmt.Errorf("fs.move: %w", err)
		}
		if removeErr := os.RemoveAll(src); removeErr != nil {
			return nil, fmt.Errorf("fs.move: cleanup source: %w", removeErr)
		}
	}
	return starlark.String("moved: " + dst), nil
}

func copyPath(src, dst string) error {
	info, err := os.Stat(src)
	if err != nil {
		return err
	}
	if info.IsDir() {
		return copyDir(src, dst)
	}
	return copyFile(src, dst, info.Mode())
}

func copyDir(src, dst string) error {
	rel, err := filepath.Rel(src, dst)
	if err == nil && (rel == "." || (!strings.HasPrefix(rel, "..") && rel != "")) {
		return fmt.Errorf("destination %q cannot be inside source directory %q", dst, src)
	}
	return filepath.WalkDir(src, func(path string, d iofs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relPath, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := dst
		if relPath != "." {
			target = filepath.Join(dst, relPath)
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if d.IsDir() {
			return os.MkdirAll(target, info.Mode())
		}
		return copyFile(path, target, info.Mode())
	})
}

func copyFile(src, dst string, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	defer out.Close()
	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Chmod(mode)
}

func builtinFsDiff(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var oldContent, newContent string
	oldName := "old"
	newName := "new"
	if err := starlark.UnpackArgs("fs.diff", args, kwargs, "old_content", &oldContent, "new_content", &newContent, "old_name?", &oldName, "new_name?", &newName); err != nil {
		return nil, err
	}
	diff, err := difflib.GetUnifiedDiffString(difflib.UnifiedDiff{
		A:        difflib.SplitLines(oldContent),
		B:        difflib.SplitLines(newContent),
		FromFile: oldName,
		ToFile:   newName,
		Context:  3,
	})
	if err != nil {
		return nil, fmt.Errorf("fs.diff: %w", err)
	}
	return starlark.String(diff), nil
}
