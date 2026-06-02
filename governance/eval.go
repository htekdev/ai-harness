package governance

import (
	"fmt"
	"regexp"
	"strings"
)

// Evaluator maintains runtime state for a single workflow execution.
// It is the core state machine engine: given a workflow definition and an
// initial state, it tracks the current state, enforces tool restrictions,
// evaluates transitions, and checks guard conditions.
//
// Evaluator is not safe for concurrent use. Each agent session should own
// its own Evaluator instance.
type Evaluator struct {
	workflow     *Workflow
	current      string
	history      string // last non-interrupt state (for HistoryState resume)
	iterations   map[string]int
	filesEdited  map[string]int // per-state file-edit counts
	contextData  map[string]any
}

// NewEvaluator creates an Evaluator for the given workflow starting at its
// initial state.
func NewEvaluator(w *Workflow) (*Evaluator, error) {
	if err := w.Validate(); err != nil {
		return nil, fmt.Errorf("governance: invalid workflow: %w", err)
	}
	return &Evaluator{
		workflow:    w,
		current:     w.Initial,
		iterations:  make(map[string]int),
		filesEdited: make(map[string]int),
		contextData: make(map[string]any),
	}, nil
}

// CurrentState returns the name of the state the workflow is currently in.
func (e *Evaluator) CurrentState() string {
	return e.current
}

// CurrentStateConfig returns the State definition for the current state.
func (e *Evaluator) CurrentStateConfig() *State {
	return e.workflow.States[e.current]
}

// IsFinal reports whether the current state is a terminal state.
func (e *Evaluator) IsFinal() bool {
	s := e.workflow.States[e.current]
	return s != nil && s.Type == StateTypeFinal
}

// AllowedTools returns the ordered list of tool names permitted in the current
// state. The result is the resolved set after applying state-level overrides
// and block lists.
//
// A nil return means all tools are permitted (open policy).
// A non-nil empty slice means no tools are permitted.
func (e *Evaluator) AllowedTools() []string {
	s := e.workflow.States[e.current]
	if s == nil {
		return nil
	}

	// Determine the base allowed set.
	var base []string
	if s.AllowedTools != nil {
		base = s.AllowedTools
	} else if e.workflow.DefaultAllowedTools != nil {
		base = e.workflow.DefaultAllowedTools
	} else {
		// Open policy: nil means all tools allowed.
		return nil
	}

	if len(s.BlockedTools) == 0 {
		return base
	}

	// Apply block list.
	blocked := make(map[string]bool, len(s.BlockedTools))
	for _, b := range s.BlockedTools {
		blocked[b] = true
	}
	out := make([]string, 0, len(base))
	for _, tool := range base {
		if !blocked[tool] {
			out = append(out, tool)
		}
	}
	return out
}

// IsToolAllowed reports whether the named tool may be called in the current state.
func (e *Evaluator) IsToolAllowed(tool string) bool {
	allowed := e.AllowedTools()
	if allowed == nil {
		// Open policy.
		return !e.isToolBlocked(tool)
	}
	for _, a := range allowed {
		if a == tool {
			return !e.isToolBlocked(tool)
		}
	}
	return false
}

func (e *Evaluator) isToolBlocked(tool string) bool {
	s := e.workflow.States[e.current]
	if s == nil {
		return false
	}
	for _, b := range s.BlockedTools {
		if b == tool {
			return true
		}
	}
	return false
}

// IsCommandAllowed reports whether the given shell command is permitted in the
// current state. Commands are matched by prefix against AllowedCommands. If the
// current state has no AllowedCommands list, all commands are permitted.
func (e *Evaluator) IsCommandAllowed(cmd string) bool {
	s := e.workflow.States[e.current]
	if s == nil || len(s.AllowedCommands) == 0 {
		return true
	}
	for _, prefix := range s.AllowedCommands {
		if strings.HasPrefix(cmd, prefix) {
			return true
		}
	}
	return false
}

// RecordIteration increments the iteration counter for the current state and
// reports whether the MaxIterations limit has been exceeded.
// Returns (count, exceeded).
func (e *Evaluator) RecordIteration() (int, bool) {
	e.iterations[e.current]++
	count := e.iterations[e.current]
	s := e.workflow.States[e.current]
	if s != nil && s.MaxIterations > 0 && count > s.MaxIterations {
		return count, true
	}
	return count, false
}

// RecordFileEdit records that the given file path was edited in this state.
// Returns (totalFiles, exceeded) where exceeded is true if MaxFilesPerState
// has been reached.
func (e *Evaluator) RecordFileEdit(path string) (int, bool) {
	key := e.current + ":" + path
	if e.filesEdited[key] == 0 {
		e.filesEdited[key] = 1
	}
	// Count distinct files in this state.
	prefix := e.current + ":"
	count := 0
	for k := range e.filesEdited {
		if strings.HasPrefix(k, prefix) {
			count++
		}
	}
	s := e.workflow.States[e.current]
	if s != nil && s.MaxFilesPerState > 0 && count > s.MaxFilesPerState {
		return count, true
	}
	return count, false
}

// SetContext sets a key/value pair in the evaluation context used by guards.
func (e *Evaluator) SetContext(key string, value any) {
	e.contextData[key] = value
}

// Transition attempts to move the workflow to the next state by processing the
// named event. If the event resolves to a guarded transition, the guard is
// evaluated against the current context data. Returns the name of the new
// state or an error if the transition is not valid.
func (e *Evaluator) Transition(event string) (string, error) {
	if e.IsFinal() {
		return "", fmt.Errorf("governance: workflow is in a final state %q; no transitions allowed", e.current)
	}
	s := e.workflow.States[e.current]
	if s == nil {
		return "", fmt.Errorf("governance: current state %q not found", e.current)
	}
	t, ok := s.On[event]
	if !ok {
		return "", fmt.Errorf("governance: no transition for event %q in state %q", event, e.current)
	}
	if t.Guard != "" {
		g, ok := e.workflow.Guards[t.Guard]
		if !ok {
			return "", fmt.Errorf("governance: guard %q not found", t.Guard)
		}
		passed, err := e.evalGuard(g)
		if err != nil {
			return "", fmt.Errorf("governance: evaluating guard %q: %w", t.Guard, err)
		}
		if !passed {
			return "", fmt.Errorf("governance: guard %q not satisfied for transition %q -> %q", t.Guard, e.current, t.Target)
		}
	}
	prev := e.current
	// If transitioning from a non-interrupt state, save history.
	if e.workflow.States[e.current].Interrupt == nil {
		e.history = prev
	}
	e.current = t.Target
	return e.current, nil
}

// ResumeFromHistory returns the workflow to the state saved when an interrupt
// last fired. If no history is recorded, the initial state is used.
func (e *Evaluator) ResumeFromHistory() string {
	if e.history != "" {
		e.current = e.history
		e.history = ""
	} else {
		e.current = e.workflow.Initial
	}
	return e.current
}

// RequiredModel returns the model name required by the current state, or an
// empty string if the state does not override the model.
func (e *Evaluator) RequiredModel() string {
	s := e.workflow.States[e.current]
	if s == nil {
		return ""
	}
	return s.Model
}

// RequiresApproval reports whether human approval is required before the agent
// may execute any tool in the current state.
func (e *Evaluator) RequiresApproval() bool {
	s := e.workflow.States[e.current]
	return s != nil && s.RequiresApproval
}

// evalGuard evaluates a guard condition against the current context data.
func (e *Evaluator) evalGuard(g *Guard) (bool, error) {
	actual, ok := e.contextData[g.Field]
	if !ok {
		return false, nil
	}
	return compareValues(actual, g.Op, g.Value)
}

// compareValues evaluates op(actual, expected) and returns the boolean result.
func compareValues(actual any, op GuardOp, expected any) (bool, error) {
	switch op {
	case GuardOpEq:
		return fmt.Sprintf("%v", actual) == fmt.Sprintf("%v", expected), nil
	case GuardOpNe:
		return fmt.Sprintf("%v", actual) != fmt.Sprintf("%v", expected), nil
	case GuardOpGt, GuardOpLt, GuardOpGe, GuardOpLe:
		a, errA := toFloat(actual)
		b, errB := toFloat(expected)
		if errA != nil || errB != nil {
			return false, fmt.Errorf("guard: numeric comparison requires numeric values (got %T and %T)", actual, expected)
		}
		switch op {
		case GuardOpGt:
			return a > b, nil
		case GuardOpLt:
			return a < b, nil
		case GuardOpGe:
			return a >= b, nil
		case GuardOpLe:
			return a <= b, nil
		}
	case GuardOpContains:
		aStr := fmt.Sprintf("%v", actual)
		bStr := fmt.Sprintf("%v", expected)
		return strings.Contains(aStr, bStr), nil
	case GuardOpMatches:
		aStr := fmt.Sprintf("%v", actual)
		bStr := fmt.Sprintf("%v", expected)
		re, err := regexp.Compile(bStr)
		if err != nil {
			return false, fmt.Errorf("guard: invalid regex %q: %w", bStr, err)
		}
		return re.MatchString(aStr), nil
	}
	return false, fmt.Errorf("guard: unknown op %q", op)
}

// toFloat converts a numeric value to float64 for comparison.
func toFloat(v any) (float64, error) {
	switch n := v.(type) {
	case int:
		return float64(n), nil
	case int64:
		return float64(n), nil
	case float64:
		return n, nil
	case float32:
		return float64(n), nil
	}
	return 0, fmt.Errorf("not a number: %T", v)
}
