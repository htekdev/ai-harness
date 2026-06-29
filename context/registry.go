package context

import (
	"fmt"
	"sort"
	"sync"

	"github.com/htekdev/ai-harness/compose"
)

// SourceRegistry manages declarative context sources.
//
// Sources are evaluated every turn: conditions are re-run, newly active sources
// have their content loaded, and expired sources are deactivated. The registry
// is safe for concurrent use.
type SourceRegistry struct {
	mu      sync.RWMutex
	entries []*SourceEntry
}

// NewSourceRegistry creates an empty source registry.
func NewSourceRegistry() *SourceRegistry {
	return &SourceRegistry{}
}

// Add registers a new context source.
// Returns an error if a source with the same name already exists.
func (r *SourceRegistry) Add(s Source) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	for _, e := range r.entries {
		if e.Source.Name == s.Name {
			return fmt.Errorf("context source %q already registered", s.Name)
		}
	}

	if s.Kind == "" {
		s.Kind = KindFile
	}
	if s.Scope == "" {
		s.Scope = ScopeSession
	}

	r.entries = append(r.entries, &SourceEntry{Source: s})
	return nil
}

// Evaluate re-evaluates all source conditions against the provided turn state,
// updating each entry's Active field. Newly activated sources have their content
// loaded via loader. If a source's condition evaluation fails the source is
// deactivated and the error is surfaced as an inactive reason (non-fatal).
//
// Parameters:
//   - values: the current turn-state key/value map (from scripting.TurnStateValues)
//   - baseDir: project root used to resolve relative file paths in conditions
//   - loader: function that loads content for a given Source
//   - turn: current turn number (used for TTL tracking)
func (r *SourceRegistry) Evaluate(values map[string]interface{}, baseDir string, loader func(Source) (string, error), turn int) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	for _, entry := range r.entries {
		s := entry.Source

		// Check TTL expiry before anything else.
		if s.TTL > 0 && entry.Active && (turn-entry.activatedAt) >= s.TTL {
			entry.Active = false
			entry.Reason = fmt.Sprintf("TTL expired after %d turns", s.TTL)
			continue
		}

		// Trigger-only sources are not activated by per-turn evaluation.
		if s.Trigger != "" && s.When == "" {
			continue
		}

		var nowActive bool
		var reason string

		if s.When == "" {
			// No condition → always active.
			nowActive = true
			reason = "always-on"
		} else {
			active, err := compose.EvaluateCondition(s.When, compose.ConditionContext{
				Values:  values,
				BaseDir: baseDir,
			})
			if err != nil {
				entry.Active = false
				entry.Reason = fmt.Sprintf("condition error: %v", err)
				continue
			}
			nowActive = active
			if active {
				reason = fmt.Sprintf("when: %s", s.When)
			} else {
				reason = fmt.Sprintf("condition false: %s", s.When)
			}
		}

		entry.Active = nowActive
		entry.Reason = reason

		if nowActive {
			if !entry.contentLoaded || s.Scope == ScopeTurn {
				content, err := loader(s)
				if err != nil {
					entry.Active = false
					entry.Reason = fmt.Sprintf("load error: %v", err)
					continue
				}
				entry.Content = content
				entry.contentLoaded = true
			}
			if entry.activatedAt == 0 {
				entry.activatedAt = turn
			}
		}
	}
	return nil
}

// ActivateTrigger marks all trigger-based sources that match the event name as
// active and loads their content via loader. Returns the first load error
// encountered (non-fatal: other matching sources are still activated).
func (r *SourceRegistry) ActivateTrigger(event string, loader func(Source) (string, error), turn int) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	var firstErr error
	for _, entry := range r.entries {
		if entry.Source.Trigger != event {
			continue
		}

		if !entry.contentLoaded || entry.Source.Scope == ScopeTurn {
			content, err := loader(entry.Source)
			if err != nil {
				if firstErr == nil {
					firstErr = fmt.Errorf("load trigger source %q: %w", entry.Source.Name, err)
				}
				entry.Reason = fmt.Sprintf("load error: %v", err)
				continue
			}
			entry.Content = content
			entry.contentLoaded = true
		}

		entry.Active = true
		entry.Reason = fmt.Sprintf("trigger: %s", event)
		if entry.activatedAt == 0 {
			entry.activatedAt = turn
		}
	}
	return firstErr
}

// Active returns all active source entries sorted by priority (ascending), then
// alphabetically by name as a stable tie-break.
func (r *SourceRegistry) Active() []*SourceEntry {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make([]*SourceEntry, 0, len(r.entries))
	for _, e := range r.entries {
		if e.Active {
			result = append(result, e)
		}
	}
	sort.Slice(result, func(i, j int) bool {
		pi, pj := result[i].Source.Priority, result[j].Source.Priority
		if pi != pj {
			return pi < pj
		}
		return result[i].Source.Name < result[j].Source.Name
	})
	return result
}

// All returns all source entries (active and inactive) in registration order.
func (r *SourceRegistry) All() []*SourceEntry {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make([]*SourceEntry, len(r.entries))
	copy(result, r.entries)
	return result
}

// Count returns the total number of registered sources.
func (r *SourceRegistry) Count() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.entries)
}

// SourcesFromDefs builds a SourceRegistry from a slice of ContextSourceDef
// values (e.g. parsed from identity.md frontmatter).
func SourcesFromDefs(defs []ContextSourceDef) (*SourceRegistry, error) {
	reg := NewSourceRegistry()
	for _, d := range defs {
		s := Source{
			Name:     d.Name,
			Kind:     SourceKind(d.Type),
			Path:     d.Path,
			When:     d.When,
			Trigger:  d.Trigger,
			Priority: d.Priority,
			Scope:    Scope(d.Scope),
			TTL:      d.TTL,
		}
		if err := reg.Add(s); err != nil {
			return nil, err
		}
	}
	return reg, nil
}

// ContextSourceDef is the serialisable form of a context source declaration,
// used in identity.md frontmatter under context.sources.
type ContextSourceDef struct {
	Name     string `yaml:"name"`
	Type     string `yaml:"type"`
	Path     string `yaml:"path"`
	When     string `yaml:"when,omitempty"`
	Trigger  string `yaml:"trigger,omitempty"`
	Priority int    `yaml:"priority,omitempty"`
	Scope    string `yaml:"scope,omitempty"`
	TTL      int    `yaml:"ttl,omitempty"`
}
