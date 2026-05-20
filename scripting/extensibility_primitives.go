package scripting

import (
	"bytes"
	"context"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"go.starlark.net/starlark"
	"go.starlark.net/starlarkstruct"
)

func execModule() starlark.Value {
	return starlarkstruct.FromStringDict(starlarkstruct.Default, starlark.StringDict{
		"run": starlark.NewBuiltin("exec.run", builtinExecRun),
	})
}

func builtinAssert(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	condition := starlark.Value(starlark.False)
	message := "assertion failed"
	if err := starlark.UnpackArgs("assert", args, kwargs, "condition", &condition, "msg?", &message); err != nil {
		return nil, err
	}
	if !condition.Truth() {
		return nil, fmt.Errorf("assert: %s", message)
	}
	return starlark.None, nil
}

func builtinExecRun(thread *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var cmdName string
	argvVal := starlark.Value(starlark.None)
	var timeoutMS int
	var dir string
	if err := starlark.UnpackArgs("exec.run", args, kwargs, "cmd", &cmdName, "args?", &argvVal, "timeout_ms?", &timeoutMS, "dir?", &dir); err != nil {
		return nil, err
	}
	if err := validateExecCommand(cmdName); err != nil {
		return nil, fmt.Errorf("exec.run: %w", err)
	}
	argv, err := starlarkValueToStrings(argvVal)
	if err != nil {
		return nil, fmt.Errorf("exec.run: %w", err)
	}
	if timeoutMS <= 0 {
		timeoutMS = 30000
	}

	ctx := context.Background()
	if threadCtx, ok := thread.Local(threadContextKey).(context.Context); ok && threadCtx != nil {
		ctx = threadCtx
	}
	cmdCtx, cancel := context.WithTimeout(ctx, time.Duration(timeoutMS)*time.Millisecond)
	defer cancel()

	command := exec.CommandContext(cmdCtx, cmdName, argv...)
	if strings.TrimSpace(dir) != "" {
		command.Dir = sanitizePath(dir)
	}
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr

	started := time.Now()
	err = command.Run()
	durationMS := time.Since(started).Milliseconds()
	exitCode := 0
	ok := true
	if err != nil {
		var exitErr *exec.ExitError
		switch {
		case errorsIsTimeout(cmdCtx, err):
			return nil, fmt.Errorf("exec.run: %w", cmdCtx.Err())
		case isExitError(err, &exitErr):
			exitCode = exitErr.ExitCode()
			ok = false
		default:
			return nil, fmt.Errorf("exec.run: %w", err)
		}
	}

	return goToStarlark(map[string]any{
		"ok":          ok,
		"stdout":      stdout.String(),
		"stderr":      stderr.String(),
		"exit_code":   exitCode,
		"duration_ms": durationMS,
		"cmd":         cmdName,
		"args":        argv,
	})
}

func errorsIsTimeout(ctx context.Context, err error) bool {
	if ctx != nil && ctx.Err() != nil {
		return true
	}
	return false
}

func isExitError(err error, target **exec.ExitError) bool {
	if err == nil {
		return false
	}
	exitErr, ok := err.(*exec.ExitError)
	if !ok {
		return false
	}
	*target = exitErr
	return true
}

func validateExecCommand(cmd string) error {
	trimmed := strings.TrimSpace(cmd)
	if trimmed == "" {
		return fmt.Errorf("cmd cannot be empty")
	}
	if strings.ContainsAny(trimmed, "\r\n") {
		return fmt.Errorf("cmd cannot contain newlines")
	}
	for _, forbidden := range []string{"|", ">", "<", ";", "&&", "||"} {
		if strings.Contains(trimmed, forbidden) {
			return fmt.Errorf("cmd contains forbidden shell token %q", forbidden)
		}
	}
	return nil
}

func builtinFsGlob(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var pattern string
	if err := starlark.UnpackArgs("fs.glob", args, kwargs, "pattern", &pattern); err != nil {
		return nil, err
	}
	matcher, err := compileGlobPattern(pattern)
	if err != nil {
		return nil, fmt.Errorf("fs.glob: %w", err)
	}
	cwd, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("fs.glob: %w", err)
	}
	var matches []string
	err = filepath.WalkDir(cwd, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(cwd, path)
		if err != nil {
			return err
		}
		rel = filepath.Clean(rel)
		if rel == "." {
			return nil
		}
		matchPath := filepath.ToSlash(rel)
		if matcher.MatchString(matchPath) {
			matches = append(matches, rel)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("fs.glob: %w", err)
	}
	sort.Strings(matches)
	values := make([]starlark.Value, 0, len(matches))
	for _, match := range matches {
		values = append(values, starlark.String(match))
	}
	return starlark.NewList(values), nil
}

func compileGlobPattern(pattern string) (*regexp.Regexp, error) {
	normalized := filepath.ToSlash(strings.TrimSpace(pattern))
	if normalized == "" {
		return nil, fmt.Errorf("pattern cannot be empty")
	}
	normalized = strings.TrimPrefix(normalized, "./")
	var b strings.Builder
	b.WriteString("^")
	for i := 0; i < len(normalized); {
		switch {
		case strings.HasPrefix(normalized[i:], "**"):
			if i+2 < len(normalized) && normalized[i+2] == '/' {
				b.WriteString("(?:.*/)?")
				i += 3
				continue
			}
			b.WriteString(".*")
			i += 2
		case normalized[i] == '*':
			b.WriteString("[^/]*")
			i++
		case normalized[i] == '?':
			b.WriteString("[^/]")
			i++
		default:
			b.WriteString(regexp.QuoteMeta(string(normalized[i])))
			i++
		}
	}
	b.WriteString("$")
	return regexp.Compile(b.String())
}
