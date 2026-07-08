package async

import (
	"encoding/json"
	"fmt"
	"sync"
)

// Graph is a directed acyclic graph of Placeholder nodes. Cycle detection runs
// at Add() time so runtime execution is always cycle-free.
//
// Graph is safe for concurrent use.
type Graph struct {
	mu    sync.Mutex
	nodes map[string]*Placeholder
	seq   int64
}

// NewGraph returns an empty Graph ready for use.
func NewGraph() *Graph {
	return &Graph{
		nodes: make(map[string]*Placeholder),
	}
}

// Add registers a new placeholder for the given tool+args with optional
// dependency IDs. Returns ErrUnknownPlaceholder if any dep ID is not in the
// graph, and ErrCyclicDependency if the new node would create a cycle.
func (g *Graph) Add(tool string, args json.RawMessage, depsOn []string) (*Placeholder, error) {
	g.mu.Lock()
	defer g.mu.Unlock()

	// Validate all dependency IDs exist in the graph.
	for _, dep := range depsOn {
		if _, ok := g.nodes[dep]; !ok {
			return nil, fmt.Errorf("%w: %q", ErrUnknownPlaceholder, dep)
		}
	}

	// Generate a monotonic ID for this placeholder.
	g.seq++
	newID := fmt.Sprintf("async-%d", g.seq)

	// Cycle detection: verify no dep transitively reaches newID.
	// Since newID is freshly generated it cannot appear in the existing graph,
	// so this is a robustness check against any ID collisions or API misuse.
	if hasCycle(g.nodes, newID, depsOn) {
		return nil, ErrCyclicDependency
	}

	p := newPlaceholder(newID, tool, args, depsOn)
	g.nodes[newID] = p
	return p, nil
}

// ReadyNodes returns all Pending placeholders whose dependencies are all Done.
func (g *Graph) ReadyNodes() []*Placeholder {
	g.mu.Lock()
	defer g.mu.Unlock()

	var ready []*Placeholder
	for _, p := range g.nodes {
		if p.Status() != StatusPending {
			continue
		}
		allDone := true
		for _, depID := range p.DepsOn {
			dep := g.nodes[depID]
			if dep == nil || dep.Status() != StatusDone {
				allDone = false
				break
			}
		}
		if allDone {
			ready = append(ready, p)
		}
	}
	return ready
}

// AllDone reports whether every node has reached a terminal state
// (Done, Failed, or Cancelled).
func (g *Graph) AllDone() bool {
	g.mu.Lock()
	defer g.mu.Unlock()

	for _, p := range g.nodes {
		s := p.Status()
		if s != StatusDone && s != StatusFailed && s != StatusCancelled {
			return false
		}
	}
	return true
}

// Nodes returns all placeholders in the graph (order is non-deterministic).
func (g *Graph) Nodes() []*Placeholder {
	g.mu.Lock()
	defer g.mu.Unlock()

	out := make([]*Placeholder, 0, len(g.nodes))
	for _, p := range g.nodes {
		out = append(out, p)
	}
	return out
}

// Get returns the placeholder for the given ID, and whether it was found.
func (g *Graph) Get(id string) (*Placeholder, bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	p, ok := g.nodes[id]
	return p, ok
}

// Size returns the number of nodes in the graph.
func (g *Graph) Size() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return len(g.nodes)
}

// --- cycle detection helpers ---

// hasCycle checks whether adding a new node with the given ID and deps would
// create a cycle. Since the new ID cannot exist in nodes yet this checks
// whether any dep transitively reaches newID (which would only happen on
// collision, i.e. never with sequential IDs).
func hasCycle(nodes map[string]*Placeholder, newID string, deps []string) bool {
	visited := make(map[string]bool, len(nodes))
	for _, dep := range deps {
		if reaches(nodes, dep, newID, visited) {
			return true
		}
	}
	return false
}

// reaches performs a DFS from 'from' to check if 'target' is reachable.
func reaches(nodes map[string]*Placeholder, from, target string, visited map[string]bool) bool {
	if from == target {
		return true
	}
	if visited[from] {
		return false
	}
	visited[from] = true
	p, ok := nodes[from]
	if !ok {
		return false
	}
	for _, dep := range p.DepsOn {
		if reaches(nodes, dep, target, visited) {
			return true
		}
	}
	return false
}
