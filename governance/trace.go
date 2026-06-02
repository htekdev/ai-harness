package governance

import (
	"strings"
	"time"
)

// TraceEvent is a single entry in the agent event history.
// It records one tool call or turn boundary with its key attributes so that
// temporal rules can pattern-match across the history.
type TraceEvent struct {
	// Seq is the monotonically increasing sequence number within the session.
	Seq int64
	// Timestamp is when the event was recorded.
	Timestamp time.Time
	// Type classifies the event (e.g. "tool.call", "turn.start", "turn.end").
	Type string
	// Tool is the name of the tool invoked (for tool.call events).
	Tool string
	// State is the workflow state active when this event occurred.
	State string
	// Fields carries additional typed attributes for guard evaluation.
	Fields map[string]any
}

// TraceLog is an append-only sequence of TraceEvents that represents the
// agent's observed history within the current session. It is the foundation
// for temporal (trace-based) governance rules.
type TraceLog struct {
	events []TraceEvent
}

// NewTraceLog creates an empty TraceLog.
func NewTraceLog() *TraceLog {
	return &TraceLog{}
}

// Append adds an event to the end of the trace.
func (tl *TraceLog) Append(ev TraceEvent) {
	if ev.Seq == 0 {
		ev.Seq = int64(len(tl.events) + 1)
	}
	if ev.Timestamp.IsZero() {
		ev.Timestamp = time.Now()
	}
	tl.events = append(tl.events, ev)
}

// Len returns the number of events in the trace.
func (tl *TraceLog) Len() int {
	return len(tl.events)
}

// Last returns the most recent n events, or all events if n > Len().
// Returns events in chronological order.
func (tl *TraceLog) Last(n int) []TraceEvent {
	if n >= len(tl.events) {
		out := make([]TraceEvent, len(tl.events))
		copy(out, tl.events)
		return out
	}
	out := make([]TraceEvent, n)
	copy(out, tl.events[len(tl.events)-n:])
	return out
}

// MatchesPattern evaluates a temporal pattern expression against the trace
// history and returns true if the pattern matches.
//
// # Pattern syntax (minimal, extensible)
//
// Patterns are whitespace-separated clauses:
//
//	LAST <n> CALLS <tool>
//	  True when the last n tool.call events all called <tool>.
//
//	NO <tool> WITHIN <n> TURNS
//	  True when <tool> has not been called in the last n turn events.
//
//	COUNT <tool> GT <n>
//	  True when <tool> has been called more than n times in total.
//
//	COUNT <tool> LT <n>
//	  True when <tool> has been called fewer than n times in total.
//
// The syntax is intentionally minimal for the prototype. Full XPath-style trace
// query language is deferred to the production fact-reducer.
func (tl *TraceLog) MatchesPattern(pattern string) bool {
	tokens := strings.Fields(pattern)
	if len(tokens) < 3 {
		return false
	}

	switch strings.ToUpper(tokens[0]) {
	case "LAST":
		return tl.matchLast(tokens[1:])
	case "NO":
		return tl.matchNo(tokens[1:])
	case "COUNT":
		return tl.matchCount(tokens[1:])
	}
	return false
}

// matchLast handles: LAST <n> CALLS <tool>
func (tl *TraceLog) matchLast(tokens []string) bool {
	// tokens = ["<n>", "CALLS", "<tool>"]
	if len(tokens) < 3 || strings.ToUpper(tokens[1]) != "CALLS" {
		return false
	}
	n := parseInt(tokens[0])
	tool := tokens[2]
	if n <= 0 || tool == "" {
		return false
	}
	calls := tl.toolCalls()
	if len(calls) < n {
		return false
	}
	last := calls[len(calls)-n:]
	for _, ev := range last {
		if ev.Tool != tool {
			return false
		}
	}
	return true
}

// matchNo handles: NO <tool> WITHIN <n> TURNS
func (tl *TraceLog) matchNo(tokens []string) bool {
	// tokens = ["<tool>", "WITHIN", "<n>", "TURNS"]
	if len(tokens) < 4 || strings.ToUpper(tokens[1]) != "WITHIN" {
		return false
	}
	tool := tokens[0]
	n := parseInt(tokens[2])
	if n <= 0 || tool == "" {
		return false
	}
	recent := tl.Last(n)
	for _, ev := range recent {
		if ev.Tool == tool {
			return false
		}
	}
	return true
}

// matchCount handles: COUNT <tool> GT|LT <n>
func (tl *TraceLog) matchCount(tokens []string) bool {
	// tokens = ["<tool>", "GT|LT", "<n>"]
	if len(tokens) < 3 {
		return false
	}
	tool := tokens[0]
	op := strings.ToUpper(tokens[1])
	n := parseInt(tokens[2])
	count := 0
	for _, ev := range tl.events {
		if ev.Type == "tool.call" && ev.Tool == tool {
			count++
		}
	}
	switch op {
	case "GT":
		return count > n
	case "LT":
		return count < n
	case "GE":
		return count >= n
	case "LE":
		return count <= n
	case "EQ":
		return count == n
	}
	return false
}

// toolCalls returns the subset of events that are tool.call events.
func (tl *TraceLog) toolCalls() []TraceEvent {
	var out []TraceEvent
	for _, ev := range tl.events {
		if ev.Type == "tool.call" {
			out = append(out, ev)
		}
	}
	return out
}

// parseInt parses an integer token, returning 0 on failure.
func parseInt(s string) int {
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0
		}
		n = n*10 + int(c-'0')
	}
	return n
}
