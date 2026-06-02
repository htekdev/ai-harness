// Package governance implements a Statewright-compatible state machine engine
// for AI agent governance in Go.
//
// The workflow schema is designed to be source-compatible with the Statewright
// JSON format (https://github.com/statewright/statewright), allowing workflow
// definitions authored for Statewright to be consumed directly by AI Harness.
//
// AI Harness extends the base schema with trace-based temporal governance rules
// (TraceRules), derived-fact references (FactRef), and progressive enforcement
// actions (deny / require / inject / rank) that operate over event history rather
// than only current state.
package governance

import (
	"encoding/json"
	"fmt"
)

// Workflow is a declarative state machine definition for AI agent governance.
// It is the root document of a workflow definition file and is fully
// compatible with the Statewright JSON workflow schema.
type Workflow struct {
	// ID is the canonical identifier for this workflow.
	ID string `json:"id"`

	// Version is the semantic version of this workflow definition.
	Version string `json:"version,omitempty"`

	// Description explains the workflow's purpose.
	Description string `json:"description,omitempty"`

	// Initial is the name of the starting state.
	Initial string `json:"initial"`

	// States maps state names to their definitions.
	States map[string]*State `json:"states"`

	// Guards maps guard IDs to their conditions.
	// Guards can be referenced from transitions to make them conditional.
	Guards map[string]*Guard `json:"guards,omitempty"`

	// TraceRules are AI Harness extensions for temporal governance over event
	// history. These rules operate across multiple turns and are evaluated by
	// the trace evaluator rather than the state machine evaluator.
	TraceRules []*TraceRule `json:"trace_rules,omitempty"`

	// DefaultAllowedTools is the baseline tool set available in all states that
	// do not define their own allowed_tools. A nil slice means all tools are
	// permitted (open policy); a non-nil empty slice means no tools are permitted.
	DefaultAllowedTools []string `json:"default_allowed_tools,omitempty"`
}

// Validate checks that the workflow is internally consistent.
func (w *Workflow) Validate() error {
	if w.ID == "" {
		return fmt.Errorf("workflow: id is required")
	}
	if w.Initial == "" {
		return fmt.Errorf("workflow %q: initial is required", w.ID)
	}
	if len(w.States) == 0 {
		return fmt.Errorf("workflow %q: at least one state is required", w.ID)
	}
	if _, ok := w.States[w.Initial]; !ok {
		return fmt.Errorf("workflow %q: initial state %q is not defined", w.ID, w.Initial)
	}
	for name, s := range w.States {
		if err := s.validate(name, w); err != nil {
			return err
		}
	}
	return nil
}

// State is a node in the workflow state machine.
// Each state constrains which tools the agent may call and defines transitions
// to other states based on named events.
type State struct {
	// Type classifies the state. Use "final" for terminal states.
	// Sub-machine invoke states use "invoke". Parallel fork states use "parallel".
	// An empty Type indicates a normal (non-terminal) state.
	Type StateType `json:"type,omitempty"`

	// AllowedTools restricts which tools the agent may invoke while in this state.
	// Tool names are matched exactly (case-sensitive). A nil slice inherits the
	// workflow-level DefaultAllowedTools. An explicit empty slice denies all tools.
	AllowedTools []string `json:"allowed_tools,omitempty"`

	// BlockedTools lists tools that are explicitly denied in this state even if
	// they would otherwise be allowed. Blocks take precedence over AllowedTools.
	// AI Harness extension: Statewright uses bash_discernment for bash sub-filtering.
	BlockedTools []string `json:"blocked_tools,omitempty"`

	// AllowedCommands restricts shell commands by prefix when a shell tool (Bash,
	// Shell) is in AllowedTools. Only commands matching a listed prefix are
	// permitted. An empty slice permits all commands (when Bash is allowed).
	AllowedCommands []string `json:"allowed_commands,omitempty"`

	// MaxIterations caps the number of agent turns in this state before a
	// mandatory transition must occur. Zero means no limit.
	MaxIterations int `json:"max_iterations,omitempty"`

	// MaxEditLines caps the number of lines that may be changed in a single edit
	// tool call while in this state. Zero means no limit.
	MaxEditLines int `json:"max_edit_lines,omitempty"`

	// MaxFilesPerState caps the total number of distinct files that may be edited
	// across all edit calls while in this state. Zero means no limit.
	MaxFilesPerState int `json:"max_files_per_state,omitempty"`

	// RequiresApproval pauses the workflow and waits for explicit human approval
	// before the agent may execute any tool in this state.
	RequiresApproval bool `json:"requires_approval,omitempty"`

	// Model optionally overrides which model handles agent turns in this state.
	// Per-state model routing is a Statewright feature; AI Harness maps this to
	// the model field in the composed harness policy.
	Model string `json:"model,omitempty"`

	// On maps named event strings to transitions. When the agent emits an event
	// with the matching name, the corresponding transition is evaluated.
	On map[string]Transition `json:"on,omitempty"`

	// Interrupt defines conditions under which the workflow automatically
	// transitions to a different state (and optionally remembers where to return).
	Interrupt *Interrupt `json:"interrupt,omitempty"`

	// Parallel lists sub-machine IDs to execute in parallel (fork/join).
	// Valid only when Type is "parallel".
	Parallel []string `json:"parallel,omitempty"`

	// Invoke references the ID of a nested workflow to execute (sub-machine invoke).
	// Valid only when Type is "invoke".
	Invoke string `json:"invoke,omitempty"`

	// EnterHook is a Starlark expression evaluated when the agent enters this state.
	// AI Harness extension: wired into the hook dispatch system.
	EnterHook string `json:"enter_hook,omitempty"`

	// ExitHook is a Starlark expression evaluated when the agent exits this state.
	// AI Harness extension: wired into the hook dispatch system.
	ExitHook string `json:"exit_hook,omitempty"`

	// FactRefs lists derived-fact names that must be resolved before tool calls
	// in this state are evaluated. AI Harness extension: maps to the fact reducer.
	FactRefs []string `json:"fact_refs,omitempty"`
}

func (s *State) validate(name string, w *Workflow) error {
	for event, t := range s.On {
		if err := t.validate(event, name, w); err != nil {
			return err
		}
	}
	if s.Type == StateTypeParallel && len(s.Parallel) == 0 {
		return fmt.Errorf("state %q: parallel state must list at least one sub-machine", name)
	}
	if s.Type == StateTypeInvoke && s.Invoke == "" {
		return fmt.Errorf("state %q: invoke state must set invoke field", name)
	}
	return nil
}

// StateType classifies a state node.
type StateType string

const (
	// StateTypeFinal marks a terminal state; no further transitions are possible.
	StateTypeFinal StateType = "final"
	// StateTypeParallel marks a fork/join state that runs sub-machines in parallel.
	StateTypeParallel StateType = "parallel"
	// StateTypeInvoke marks a state that delegates to a nested workflow.
	StateTypeInvoke StateType = "invoke"
)

// Transition describes the target of a state machine event.
//
// Statewright supports two forms:
//
//	"READY": "implementing"               // simple: event -> target state name
//	"PASS":  {"target": "done", "guard": "tests_passed"}  // guarded
//
// Both forms are represented by this struct. For the simple form, only Target
// is set. UnmarshalJSON handles both JSON representations transparently.
type Transition struct {
	// Target is the name of the destination state.
	Target string `json:"target"`
	// Guard is the ID of a guard condition that must evaluate to true.
	// An empty string means the transition is unconditional.
	Guard string `json:"guard,omitempty"`
}

// UnmarshalJSON allows a Transition to be either a plain string (the target
// state name) or a full object with target and optional guard.
func (t *Transition) UnmarshalJSON(data []byte) error {
	// Try string first (simple shorthand form).
	var s string
	if err := json.Unmarshal(data, &s); err == nil {
		t.Target = s
		return nil
	}
	// Fall back to full object form.
	type plain Transition
	return json.Unmarshal(data, (*plain)(t))
}

// MarshalJSON serialises a Transition back to the compact form when no guard
// is set, matching Statewright's canonical schema output.
func (t Transition) MarshalJSON() ([]byte, error) {
	if t.Guard == "" {
		return json.Marshal(t.Target)
	}
	type plain Transition
	return json.Marshal(plain(t))
}

func (t Transition) validate(event, stateName string, w *Workflow) error {
	if t.Target == "" {
		return fmt.Errorf("state %q event %q: transition target is required", stateName, event)
	}
	if _, ok := w.States[t.Target]; !ok {
		return fmt.Errorf("state %q event %q: transition target %q is not defined", stateName, event, t.Target)
	}
	if t.Guard != "" {
		if _, ok := w.Guards[t.Guard]; !ok {
			return fmt.Errorf("state %q event %q: guard %q is not defined", stateName, event, t.Guard)
		}
	}
	return nil
}

// Guard is a programmatic condition attached to a guarded transition.
// The condition is satisfied when the named field in the evaluation context
// passes the comparison defined by Op and Value.
type Guard struct {
	// Field is the key in the evaluation context to test.
	Field string `json:"field"`
	// Op is the comparison operator.
	Op GuardOp `json:"op"`
	// Value is the expected value to compare against.
	Value any `json:"value"`
}

// GuardOp enumerates supported comparison operators for guards.
type GuardOp string

const (
	GuardOpEq       GuardOp = "eq"       // equal
	GuardOpNe       GuardOp = "ne"       // not equal
	GuardOpGt       GuardOp = "gt"       // greater than (numeric)
	GuardOpLt       GuardOp = "lt"       // less than (numeric)
	GuardOpGe       GuardOp = "ge"       // greater than or equal (numeric)
	GuardOpLe       GuardOp = "le"       // less than or equal (numeric)
	GuardOpContains GuardOp = "contains" // substring or array membership
	GuardOpMatches  GuardOp = "matches"  // regex match
)

// Interrupt defines an automatic state transition triggered by an external
// signal (e.g. the agent editing a file matching a glob pattern).
// After the interrupt-handling state completes, the workflow returns to the
// interrupted state if HistoryState is true.
type Interrupt struct {
	// Glob matches a file path pattern that triggers the interrupt.
	Glob string `json:"glob,omitempty"`
	// Event matches a named event that triggers the interrupt.
	Event string `json:"event,omitempty"`
	// Target is the state to transition to when the interrupt fires.
	Target string `json:"target"`
	// HistoryState preserves the interrupted state so execution can resume.
	HistoryState bool `json:"history_state,omitempty"`
}

// TraceRule is an AI Harness extension for temporal governance.
// Unlike Statewright guards (which evaluate only current-state context data),
// TraceRules match patterns over the full event history, enabling rules such as
// "deny write tools if the last 3 turns all called Read without any edit".
//
// TraceRules are evaluated by the TraceEvaluator, not the state machine Evaluator.
type TraceRule struct {
	// ID is the unique identifier for this rule.
	ID string `json:"id"`
	// Description explains the rule's intent for human readers.
	Description string `json:"description,omitempty"`
	// Pattern is the temporal pattern expression.
	// Syntax reference: see governance/trace.go.
	Pattern string `json:"pattern"`
	// Scope optionally restricts the rule to a specific state.
	// An empty scope applies the rule globally across all states.
	Scope string `json:"scope,omitempty"`
	// Enforcement defines what action to take when the pattern matches.
	Enforcement EnforcementAction `json:"enforcement"`
	// Payload carries action-specific data:
	//   - deny:    explanation message string
	//   - require: tool name string
	//   - inject:  context snippet string
	//   - rank:    comma-separated ordered tool names
	Payload string `json:"payload,omitempty"`
}
