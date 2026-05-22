package artifact

import (
	"fmt"
	"sort"
	"strings"
	"sync"
)

// Registry is the central artifact store. It loads, validates, orders,
// and resolves artifacts for harness composition.
//
// The registry enforces:
//   - Uniqueness: no two artifacts share the same name
//   - Type constraints: e.g., exactly one harness artifact
//   - Dependency ordering: depends_on must resolve to registered artifacts
//   - Priority ordering: composition order is deterministic
type Registry struct {
	mu        sync.RWMutex
	artifacts map[string]*Artifact // keyed by name
	ordered   []*Artifact          // sorted by priority (ascending, lowest first)
	dirty     bool                 // true if ordered needs rebuild
}

// NewRegistry creates an empty artifact registry.
func NewRegistry() *Registry {
	return &Registry{
		artifacts: make(map[string]*Artifact),
	}
}

// Register adds an artifact to the registry after validation.
// Returns an error if the artifact is invalid or a name collision exists.
func (r *Registry) Register(a *Artifact) error {
	if err := Validate(a); err != nil {
		return err
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if existing, ok := r.artifacts[a.Metadata.Name]; ok {
		return fmt.Errorf("artifact %q already registered (type: %s, source: %s)",
			a.Metadata.Name, existing.Metadata.Type, existing.Source)
	}

	// Enforce singleton constraint for harness type
	if a.Metadata.Type == TypeHarness {
		for _, existing := range r.artifacts {
			if existing.Metadata.Type == TypeHarness {
				return fmt.Errorf("only one harness artifact allowed; %q already registered",
					existing.Metadata.Name)
			}
		}
	}

	r.artifacts[a.Metadata.Name] = a
	r.dirty = true
	return nil
}

// Get retrieves an artifact by name.
func (r *Registry) Get(name string) (*Artifact, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	a, ok := r.artifacts[name]
	return a, ok
}

// All returns all registered artifacts in composition order
// (lowest priority first, highest last — so overrides are applied last).
func (r *Registry) All() []*Artifact {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.rebuildOrder()
	result := make([]*Artifact, len(r.ordered))
	copy(result, r.ordered)
	return result
}

// ByType returns all artifacts of a given type in composition order.
func (r *Registry) ByType(t Type) []*Artifact {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.rebuildOrder()

	result := make([]*Artifact, 0)
	for _, a := range r.ordered {
		if a.Metadata.Type == t {
			result = append(result, a)
		}
	}
	return result
}

// ByTag returns all artifacts that have the given tag.
func (r *Registry) ByTag(tag string) []*Artifact {
	r.mu.RLock()
	defer r.mu.RUnlock()

	tag = strings.TrimSpace(strings.ToLower(tag))
	result := make([]*Artifact, 0)
	for _, a := range r.artifacts {
		for _, t := range a.Metadata.Tags {
			if strings.TrimSpace(strings.ToLower(t)) == tag {
				result = append(result, a)
				break
			}
		}
	}
	return result
}

// Remove removes an artifact by name. Returns true if it was found and removed.
func (r *Registry) Remove(name string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.artifacts[name]; !ok {
		return false
	}
	delete(r.artifacts, name)
	r.dirty = true
	return true
}

// Count returns the number of registered artifacts.
func (r *Registry) Count() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.artifacts)
}

// ValidateDependencies checks that all depends_on references resolve.
func (r *Registry) ValidateDependencies() error {
	r.mu.RLock()
	defer r.mu.RUnlock()

	issues := make([]string, 0)
	for _, a := range r.artifacts {
		for _, dep := range a.Metadata.DependsOn {
			if _, ok := r.artifacts[dep]; !ok {
				issues = append(issues, fmt.Sprintf("%q depends on %q which is not registered", a.Metadata.Name, dep))
			}
		}
	}
	if len(issues) > 0 {
		return fmt.Errorf("unresolved dependencies: %s", strings.Join(issues, "; "))
	}
	return nil
}

// Resolve returns the composition-ordered list of active artifacts
// after evaluating conditions against the provided context.
// Artifacts whose conditions evaluate to false are excluded.
func (r *Registry) Resolve(evalFn func(condition string) (bool, error)) ([]*Artifact, error) {
	all := r.All() // already sorted by priority

	active := make([]*Artifact, 0, len(all))
	for _, a := range all {
		if a.Condition == "" {
			active = append(active, a)
			continue
		}
		match, err := evalFn(a.Condition)
		if err != nil {
			return nil, fmt.Errorf("evaluate condition for %q: %w", a.Metadata.Name, err)
		}
		if match {
			active = append(active, a)
		}
	}
	return active, nil
}

// rebuildOrder sorts artifacts by effective priority (ascending).
// Must be called with r.mu held.
func (r *Registry) rebuildOrder() {
	if !r.dirty {
		return
	}
	r.ordered = make([]*Artifact, 0, len(r.artifacts))
	for _, a := range r.artifacts {
		r.ordered = append(r.ordered, a)
	}
	sort.Slice(r.ordered, func(i, j int) bool {
		pi := r.ordered[i].EffectivePriority()
		pj := r.ordered[j].EffectivePriority()
		if pi != pj {
			return pi < pj
		}
		// Stable tie-break: alphabetical by name
		return r.ordered[i].Metadata.Name < r.ordered[j].Metadata.Name
	})
	r.dirty = false
}
