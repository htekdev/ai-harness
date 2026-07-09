// Package async provides parallel tool execution with dependency graphs
// (DAG scheduler) and a loop-boundary barrier pattern for the agent loop.
//
// The core design:
//   - Tool scripts call async.launch(tool, args) to dispatch work to the executor.
//   - The executor respects a dependency graph: B won't start until A completes.
//   - async.wait_all / wait_any / race block until futures resolve.
//   - The agent's loop-boundary barrier drains any un-awaited futures before
//     the next LLM completion, preventing goroutine leaks.
package async

import (
	"errors"
	"fmt"
)

// Kind categorises async errors for programmatic handling.
type Kind int

const (
	// KindCycle indicates a dependency cycle was detected in the DAG.
	KindCycle Kind = iota + 1
	// KindCancelled indicates a placeholder was cancelled (e.g. by race).
	KindCancelled
	// KindTimeout indicates a wait timed out.
	KindTimeout
	// KindExecution indicates the underlying tool execution failed.
	KindExecution
	// KindUnknownTool indicates the requested tool name is not registered.
	KindUnknownTool
)

// Error is a typed async error.
type Error struct {
	Kind    Kind
	Message string
	Cause   error
}

func (e *Error) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("async(%v): %s: %v", e.Kind, e.Message, e.Cause)
	}
	return fmt.Sprintf("async(%v): %s", e.Kind, e.Message)
}

func (e *Error) Unwrap() error { return e.Cause }

// Sentinel errors that callers can use with errors.Is.
var (
	ErrCycleDetected = &Error{Kind: KindCycle, Message: "dependency cycle detected"}
	ErrCancelled     = &Error{Kind: KindCancelled, Message: "placeholder cancelled"}
)

func newf(kind Kind, msg string, args ...any) *Error {
	return &Error{Kind: kind, Message: fmt.Sprintf(msg, args...)}
}

func wrap(kind Kind, msg string, cause error) *Error {
	return &Error{Kind: kind, Message: msg, Cause: cause}
}

// IsCancelled reports whether err (or any wrapped error) is a cancellation error.
func IsCancelled(err error) bool {
	var e *Error
	return errors.As(err, &e) && e.Kind == KindCancelled
}
