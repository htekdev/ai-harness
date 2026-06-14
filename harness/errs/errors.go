// Package errs is the typed error taxonomy for ai-harness (Phase 5.3).
//
// Pi and Copilot CLI extensions surface errors as raw strings — callers cannot
// programmatically distinguish a tool-handler failure from a config-load
// failure from a provider outage. AI Harness lifts classification into a
// first-class runtime contract so that hooks, retries, and operator
// dashboards can react to *what kind* of failure occurred without parsing
// message text.
//
// The package is a leaf (no imports of agent/tools/delegation/harness) so any
// runtime package can depend on it without cycles.
package errs

import (
	"errors"
	"fmt"
)

// Kind classifies an error by the subsystem that surfaced it.
//
// Phase 5.3: typed error taxonomy. Pi and Copilot CLI extensions surface
// errors as raw strings — callers cannot programmatically distinguish a
// tool-handler failure from a config-load failure from a provider outage.
// AI Harness lifts classification into a first-class runtime contract so
// that hooks, retries, and operator dashboards can react to *what kind*
// of failure occurred without parsing message text.
type Kind int

const (
	// KindUnknown is the zero value and means the error has not been classified.
	KindUnknown Kind = iota
	// KindConfig: harness config / artifact loading / validation failed.
	KindConfig
	// KindTool: tool registry lookup or tool handler execution failed.
	KindTool
	// KindCompletion: model provider / completion call failed (network, API, quota).
	KindCompletion
	// KindDelegation: sub-agent delegation failed (depth, registry, runtime).
	KindDelegation
	// KindSource: input source (stdin, telegram, meshwire) failed.
	KindSource
	// KindPersistence: offset store, session store, or other state I/O failed.
	KindPersistence
	// KindInvalidConversation: outbound message array is malformed (e.g. a
	// role:tool message has no matching preceding assistant tool_calls
	// envelope). Surfacing this as a typed error lets the harness fail
	// fast pre-flight instead of bouncing off a provider 400.
	KindInvalidConversation
)

// String returns the canonical lower-case kind name. Stable for log/trace attrs.
func (k Kind) String() string {
	switch k {
	case KindConfig:
		return "config"
	case KindTool:
		return "tool"
	case KindCompletion:
		return "completion"
	case KindDelegation:
		return "delegation"
	case KindSource:
		return "source"
	case KindPersistence:
		return "persistence"
	case KindInvalidConversation:
		return "invalid_conversation"
	default:
		return "unknown"
	}
}

// Error is the canonical typed error for harness subsystems.
//
// It implements the standard error interface and supports errors.Is /
// errors.As / errors.Unwrap so it composes cleanly with stdlib error chains.
//
// Use the helpers (Wrap, Newf) to construct values; the zero value is not
// useful. Callers can introspect with KindOf and IsRetriable.
type Error struct {
	Kind      Kind
	Op        string // optional short subsystem op tag, e.g. "tools.execute"
	Msg       string // human message (no trailing newline)
	Retriable bool   // hint to callers: is a retry meaningful?
	Cause     error  // wrapped underlying error, may be nil
}

// Error renders the error as "<op>: <msg>: <cause>" with empty parts elided.
func (e *Error) Error() string {
	switch {
	case e.Op != "" && e.Cause != nil:
		return fmt.Sprintf("%s: %s: %v", e.Op, e.Msg, e.Cause)
	case e.Op != "":
		return fmt.Sprintf("%s: %s", e.Op, e.Msg)
	case e.Cause != nil:
		return fmt.Sprintf("%s: %v", e.Msg, e.Cause)
	default:
		return e.Msg
	}
}

// Unwrap returns the wrapped cause for errors.Is / errors.As traversal.
func (e *Error) Unwrap() error { return e.Cause }

// Is supports errors.Is(err, &Error{Kind: KindTool}) — matching by Kind only
// when the target is a *Error with a non-zero Kind. Falls back to identity.
func (e *Error) Is(target error) bool {
	t, ok := target.(*Error)
	if !ok {
		return false
	}
	if t.Kind != KindUnknown && t.Kind != e.Kind {
		return false
	}
	return true
}

// Newf constructs a typed Error without a wrapped cause.
func Newf(kind Kind, op, format string, args ...any) *Error {
	return &Error{Kind: kind, Op: op, Msg: fmt.Sprintf(format, args...)}
}

// Wrap constructs a typed Error wrapping cause. If cause is nil, returns nil
// so callers can write `return harness.Wrap(...)` without a nil check.
func Wrap(kind Kind, op string, cause error, format string, args ...any) error {
	if cause == nil {
		return nil
	}
	return &Error{Kind: kind, Op: op, Msg: fmt.Sprintf(format, args...), Cause: cause}
}

// Retriable wraps cause and marks it retriable. Convenience for hot paths
// (transient provider / network failures).
func Retriable(kind Kind, op string, cause error, format string, args ...any) error {
	if cause == nil {
		return nil
	}
	return &Error{Kind: kind, Op: op, Msg: fmt.Sprintf(format, args...), Cause: cause, Retriable: true}
}

// KindOf walks the error chain and returns the Kind of the first *Error it
// finds. Returns KindUnknown for nil or unclassified errors. Use this in
// hooks, dashboards, and tests instead of message-string matching.
func KindOf(err error) Kind {
	var e *Error
	if errors.As(err, &e) {
		return e.Kind
	}
	return KindUnknown
}

// IsRetriable returns true if any *Error in the chain has Retriable=true.
func IsRetriable(err error) bool {
	var e *Error
	for err != nil {
		if errors.As(err, &e) && e.Retriable {
			return true
		}
		err = errors.Unwrap(err)
	}
	return false
}
