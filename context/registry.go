// Package context manages conversation history, context windows, and
// declarative context source loading for the AI harness.
package context

import (
	"fmt"
	"sort"
	"sync"

	"github.com/htekdev/ai-harness/compose"
	"github.com/htekdev/ai-harness/config"
)

// ContextSource represents a single declarative context source — a named
// piece of content (file or URL) that can be conditionally injected into
// the system prompt at the start of each agent turn.
type ContextSource struct {
	// Name is the unique identifier for this source.
	Name string
	// Type is the source type: "file" or "url".
	Type string
	// Path is the file path (relative to harness root) for type="file".
	Path string
	// URL is the endpoint to fetch for type="url".
	URL string
	// When is a Starlark expression evaluated against turn state each turn.
	// Empty string means the source is always active.
	When string
	// Trigger is an optional hook event name that activates this source.
	Trigger string
	// Priority controls injection order; lower values are injected first.
	Priority int
	// TTL is the number of turns to stay active after the condition first
	// becomes true. 0 means no TTL (stays active as long as condition holds).
	TTL int
	// Scope is "session" or "turn".
	// "turn" sources are automatically unloaded after each turn.
	Scope string
}

// Registry holds all registered context sources and evaluates which are
// active for a given agent turn based on Starlark `when` expressions.
//
// The registry is thread-safe.
type Registry struct {
	mu      sync.RWMutex
	sources map[string]ContextSource
}

// NewRegistry creates an empty context source registry.
func NewRegistry() *Registry {
	return &Registry{
		sources: make(map[string]ContextSource),
	}
}

// Register adds or replaces a context source by name.
// Returns an error if the source name is empty.
func (r *Registry) Register(source ContextSource) error {
	if source.Name == "" {
		return fmt.Errorf("context source name cannot be empty")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sources[source.Name] = source
	return nil
}

// LoadFromConfig bulk-loads context sources from a config slice.
// Returns the first error encountered; registration stops on error.
func (r *Registry) LoadFromConfig(sources []config.ContextSourceConfig) error {
	for _, s := range sources {
		src := ContextSource{
			Name:     s.Name,
			Type:     s.Type,
			Path:     s.Path,
			URL:      s.URL,
			When:     s.When,
			Trigger:  s.Trigger,
			Priority: s.Priority,
			TTL:      s.TTL,
			Scope:    s.Scope,
		}
		if err := r.Register(src); err != nil {
			return fmt.Errorf("load context source %q: %w", s.Name, err)
		}
	}
	return nil
}

// Active evaluates all registered sources against turnState and returns
// those whose `when` expression evaluates to true, sorted by priority
// ascending (lower = injected first), with name as a stable tie-break.
//
// Sources with an empty When expression are always active.
// Condition evaluation uses compose.EvaluateCondition (Starlark).
func (r *Registry) Active(turnState map[string]interface{}) ([]ContextSource, error) {
	r.mu.RLock()
	all := make([]ContextSource, 0, len(r.sources))
	for _, s := range r.sources {
		all = append(all, s)
	}
	r.mu.RUnlock()

	condCtx := compose.ConditionContext{Values: turnState}

	active := make([]ContextSource, 0, len(all))
	for _, s := range all {
		if s.When == "" {
			active = append(active, s)
			continue
		}
		ok, err := compose.EvaluateCondition(s.When, condCtx)
		if err != nil {
			return nil, fmt.Errorf("context source %q when expression: %w", s.Name, err)
		}
		if ok {
			active = append(active, s)
		}
	}

	sort.Slice(active, func(i, j int) bool {
		if active[i].Priority != active[j].Priority {
			return active[i].Priority < active[j].Priority
		}
		return active[i].Name < active[j].Name
	})

	return active, nil
}

// All returns all registered sources in priority order (ascending),
// with name as a stable tie-break.
func (r *Registry) All() []ContextSource {
	r.mu.RLock()
	all := make([]ContextSource, 0, len(r.sources))
	for _, s := range r.sources {
		all = append(all, s)
	}
	r.mu.RUnlock()

	sort.Slice(all, func(i, j int) bool {
		if all[i].Priority != all[j].Priority {
			return all[i].Priority < all[j].Priority
		}
		return all[i].Name < all[j].Name
	})
	return all
}

// Count returns the number of registered context sources.
func (r *Registry) Count() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.sources)
}
