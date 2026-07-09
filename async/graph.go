package async

import (
	"fmt"
	"sync"
)

// node is a vertex in the dependency graph.
type node struct {
	id   string
	deps []string // IDs of placeholders that must complete before this one runs
}

// Graph is a directed acyclic graph of async placeholders.
// It detects cycles at declaration time.
// All methods are safe for concurrent use.
type Graph struct {
	mu    sync.Mutex
	nodes map[string]*node
}

// NewGraph creates an empty dependency graph.
func NewGraph() *Graph {
	return &Graph{nodes: make(map[string]*node)}
}

// Add inserts a new node with the given ID and dependencies.
// Returns ErrCycleDetected if adding this node would create a cycle.
// Returns an error if id is already in the graph.
func (g *Graph) Add(id string, deps []string) error {
	g.mu.Lock()
	defer g.mu.Unlock()

	if _, exists := g.nodes[id]; exists {
		return fmt.Errorf("async: node %q already in graph", id)
	}

	// Validate dep IDs exist (they must be registered before the dependent).
	for _, dep := range deps {
		if _, ok := g.nodes[dep]; !ok {
			return newf(KindCycle, "dependency %q not found in graph (add deps before dependents)", dep)
		}
	}

	g.nodes[id] = &node{id: id, deps: deps}

	// Cycle check: attempt topological sort.
	if err := g.cycleCheck(); err != nil {
		delete(g.nodes, id)
		return err
	}

	return nil
}

// cycleCheck performs a DFS cycle detection (caller must hold g.mu).
func (g *Graph) cycleCheck() error {
	// 0 = white (unvisited), 1 = grey (in stack), 2 = black (done)
	color := make(map[string]int, len(g.nodes))

	var visit func(id string) error
	visit = func(id string) error {
		if color[id] == 2 {
			return nil
		}
		if color[id] == 1 {
			return fmt.Errorf("%w: node %q is part of a cycle", ErrCycleDetected, id)
		}
		color[id] = 1
		n, ok := g.nodes[id]
		if !ok {
			color[id] = 2
			return nil
		}
		for _, dep := range n.deps {
			if err := visit(dep); err != nil {
				return err
			}
		}
		color[id] = 2
		return nil
	}

	for id := range g.nodes {
		if err := visit(id); err != nil {
			return err
		}
	}
	return nil
}

// Deps returns the dependency IDs for the given node.
// Returns nil if the node is not in the graph.
func (g *Graph) Deps(id string) []string {
	g.mu.Lock()
	defer g.mu.Unlock()
	n, ok := g.nodes[id]
	if !ok {
		return nil
	}
	out := make([]string, len(n.deps))
	copy(out, n.deps)
	return out
}

// Remove deletes a node from the graph (called after a placeholder completes).
func (g *Graph) Remove(id string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	delete(g.nodes, id)
}
