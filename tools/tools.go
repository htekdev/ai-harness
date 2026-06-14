// Package tools provides a tool registry and execution system for the AI harness.
// Tools are functions that the AI agent can invoke to interact with the outside world.
package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/htekdev/ai-harness/harness/errs"
)

// tracerName is the OTel instrumentation library name shared across the
// ai-harness packages so spans coalesce under a single instrumentation scope.
const tracerName = "github.com/htekdev/ai-harness"

// ParameterType represents the JSON Schema type of a tool parameter.
type ParameterType string

const (
	TypeString  ParameterType = "string"
	TypeNumber  ParameterType = "number"
	TypeBoolean ParameterType = "boolean"
	TypeObject  ParameterType = "object"
	TypeArray   ParameterType = "array"
)

// Parameter defines a single tool parameter.
type Parameter struct {
	Name        string        `json:"name"`
	Type        ParameterType `json:"type"`
	Description string        `json:"description"`
	Required    bool          `json:"required"`
	// Items defines the schema for array elements (required when Type is "array").
	Items *ParameterSchema `json:"items,omitempty"`
	// Properties defines nested properties for object types.
	Properties map[string]*ParameterSchema `json:"properties,omitempty"`
}

// ParameterSchema is a JSON Schema subset for nested parameter definitions.
type ParameterSchema struct {
	Type        ParameterType               `json:"type"`
	Description string                      `json:"description,omitempty"`
	Properties  map[string]*ParameterSchema `json:"properties,omitempty"`
	Items       *ParameterSchema            `json:"items,omitempty"`
	Required    []string                    `json:"required,omitempty"`
}

// Definition describes a tool that can be registered and invoked.
type Definition struct {
	Name        string      `json:"name"`
	Description string      `json:"description"`
	Parameters  []Parameter `json:"parameters"`
}

// Call represents a tool invocation request from the model.
type Call struct {
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

// Result represents the output of a tool execution.
type Result struct {
	CallID  string `json:"call_id"`
	Name    string `json:"name"`
	Content string `json:"content"`
	IsError bool   `json:"is_error"`
}

// Handler is a function that implements a tool's logic.
type Handler func(ctx context.Context, args json.RawMessage) (string, error)

// registration holds a tool definition and its handler.
type registration struct {
	Definition Definition
	Handler    Handler
	CreatedAt  time.Time
}

// Registry manages tool registrations and invocations.
type Registry struct {
	mu    sync.RWMutex
	tools map[string]registration

	// policyMu guards policy. It is intentionally separate from mu so a
	// policy update never blocks tool execution and vice versa.
	policyMu sync.RWMutex
	policy   *Policy
}

// NewRegistry creates a new tool registry.
func NewRegistry() *Registry {
	return &Registry{
		tools: make(map[string]registration),
	}
}

// Register adds a tool to the registry. Returns an error if a tool with the same name already exists.
func (r *Registry) Register(def Definition, handler Handler) error {
	if def.Name == "" {
		return errs.Newf(errs.KindTool, "tools.register", "tool name cannot be empty")
	}
	if handler == nil {
		return errs.Newf(errs.KindTool, "tools.register", "tool handler cannot be nil")
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.tools[def.Name]; exists {
		return errs.Newf(errs.KindTool, "tools.register", "tool %q already registered", def.Name)
	}

	r.tools[def.Name] = registration{
		Definition: def,
		Handler:    handler,
		CreatedAt:  time.Now(),
	}
	return nil
}

// Unregister removes a tool from the registry.
func (r *Registry) Unregister(name string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.tools[name]; !exists {
		return false
	}
	delete(r.tools, name)
	return true
}

// Replace registers a tool, overwriting any existing tool with the same name.
func (r *Registry) Replace(def Definition, handler Handler) error {
	if def.Name == "" {
		return errs.Newf(errs.KindTool, "tools.replace", "tool name cannot be empty")
	}
	if handler == nil {
		return errs.Newf(errs.KindTool, "tools.replace", "tool handler cannot be nil")
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	r.tools[def.Name] = registration{
		Definition: def,
		Handler:    handler,
		CreatedAt:  time.Now(),
	}
	return nil
}

// Has reports whether a tool with the given name is registered AND permitted
// by the active policy. A blocked tool reports false even when registered —
// callers that need raw registry presence should use HasRegistered.
func (r *Registry) Has(name string) bool {
	if !r.allowed(name) {
		return false
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	_, exists := r.tools[name]
	return exists
}

// HasRegistered reports whether a tool with the given name is registered,
// ignoring policy. Useful for reload/replace flows that need to inspect the
// raw registry state.
func (r *Registry) HasRegistered(name string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	_, exists := r.tools[name]
	return exists
}

// Count returns the number of registered tools.
func (r *Registry) Count() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.tools)
}

// Execute invokes a tool by name with the given arguments.
// It respects context cancellation and deadlines.
//
// Phase 5.2 (PR-B): each call emits a `tool.call` OTel span with attributes
//
//	tool.name      = call.Name
//	tool.call_id   = call.ID
//	tool.is_error  = result.IsError (set on End)
//
// When the tool is not registered or its handler returns an error, the span's
// status is set to Error with the failure message; the underlying error is
// recorded via span.RecordError when available.
func (r *Registry) Execute(ctx context.Context, call Call) (result Result) {
	ctx, span := otel.Tracer(tracerName).Start(ctx, "tool.call",
		trace.WithAttributes(
			attribute.String("tool.name", call.Name),
			attribute.String("tool.call_id", call.ID),
		),
	)
	defer func() {
		span.SetAttributes(attribute.Bool("tool.is_error", result.IsError))
		if result.IsError {
			span.SetStatus(codes.Error, result.Content)
		}
		span.End()
	}()

	r.mu.RLock()
	reg, exists := r.tools[call.Name]
	r.mu.RUnlock()

	if !exists {
		return Result{
			CallID:  call.ID,
			Name:    call.Name,
			Content: fmt.Sprintf("tool %q not found", call.Name),
			IsError: true,
		}
	}

	// Policy enforcement happens AFTER the registry lookup so we can
	// distinguish "unknown tool" from "policy denied". Both return
	// IsError=true so the model sees a tool error and can adapt, but the
	// span's status message preserves the distinction for audit.
	if !r.allowed(call.Name) {
		span.SetAttributes(attribute.String("tool.policy", "denied"))
		return Result{
			CallID:  call.ID,
			Name:    call.Name,
			Content: fmt.Sprintf("tool %q denied by policy", call.Name),
			IsError: true,
		}
	}

	output, err := reg.Handler(ctx, call.Arguments)
	if err != nil {
		span.RecordError(err)
		return Result{
			CallID:  call.ID,
			Name:    call.Name,
			Content: fmt.Sprintf("tool error: %v", err),
			IsError: true,
		}
	}

	return Result{
		CallID:  call.ID,
		Name:    call.Name,
		Content: output,
		IsError: false,
	}
}

// List returns all registered tool definitions that are permitted by the
// active policy. Blocked tools are omitted so model callers (List ⇒ prompt)
// never see capabilities they cannot invoke.
func (r *Registry) List() []Definition {
	r.mu.RLock()
	defer r.mu.RUnlock()

	defs := make([]Definition, 0, len(r.tools))
	for _, reg := range r.tools {
		if !r.allowed(reg.Definition.Name) {
			continue
		}
		defs = append(defs, reg.Definition)
	}
	return defs
}

// ListAll returns every registered tool definition regardless of policy.
// Use for diagnostics, observability, or reload bookkeeping.
func (r *Registry) ListAll() []Definition {
	r.mu.RLock()
	defer r.mu.RUnlock()

	defs := make([]Definition, 0, len(r.tools))
	for _, reg := range r.tools {
		defs = append(defs, reg.Definition)
	}
	return defs
}

// Get returns a specific tool definition by name, honoring the active
// policy. Blocked tools return (Definition{}, false) even when registered.
func (r *Registry) Get(name string) (Definition, bool) {
	if !r.allowed(name) {
		return Definition{}, false
	}
	r.mu.RLock()
	defer r.mu.RUnlock()

	reg, exists := r.tools[name]
	if !exists {
		return Definition{}, false
	}
	return reg.Definition, true
}

// ToOpenAIFormat converts policy-permitted tools to the OpenAI function
// calling format. Tools blocked by the active policy are omitted so the
// completion request never advertises a denied capability.
func (r *Registry) ToOpenAIFormat() []map[string]any {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make([]map[string]any, 0, len(r.tools))
	for _, reg := range r.tools {
		if !r.allowed(reg.Definition.Name) {
			continue
		}
		properties := make(map[string]any)
		required := []string{}

		for _, p := range reg.Definition.Parameters {
			propSchema := map[string]any{
				"type":        string(p.Type),
				"description": p.Description,
			}
			if p.Items != nil {
				propSchema["items"] = parameterSchemaToMap(p.Items)
			}
			if p.Properties != nil {
				nested := make(map[string]any)
				for k, v := range p.Properties {
					nested[k] = parameterSchemaToMap(v)
				}
				propSchema["properties"] = nested
			}
			properties[p.Name] = propSchema
			if p.Required {
				required = append(required, p.Name)
			}
		}

		tool := map[string]any{
			"type": "function",
			"function": map[string]any{
				"name":        reg.Definition.Name,
				"description": reg.Definition.Description,
				"parameters": map[string]any{
					"type":       "object",
					"properties": properties,
					"required":   required,
				},
			},
		}
		result = append(result, tool)
	}
	return result
}

// parameterSchemaToMap converts a ParameterSchema to a map for JSON serialization.
func parameterSchemaToMap(s *ParameterSchema) map[string]any {
	if s == nil {
		return nil
	}
	m := map[string]any{
		"type": string(s.Type),
	}
	if s.Description != "" {
		m["description"] = s.Description
	}
	if s.Items != nil {
		m["items"] = parameterSchemaToMap(s.Items)
	}
	if s.Properties != nil {
		nested := make(map[string]any)
		for k, v := range s.Properties {
			nested[k] = parameterSchemaToMap(v)
		}
		m["properties"] = nested
	}
	if len(s.Required) > 0 {
		m["required"] = s.Required
	}
	return m
}
