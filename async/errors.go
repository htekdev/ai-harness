// Package async provides the Loop-Boundary Barrier pattern for parallel tool
// execution in the AI Harness. Goroutines are dispatched per placeholder and
// synchronized at turn boundaries — no explicit await in user Starlark code.
package async

import "errors"

// ErrUnknownPlaceholder is returned when a placeholder ID is not found in the graph.
var ErrUnknownPlaceholder = errors.New("async: unknown placeholder ID")

// ErrCyclicDependency is returned when adding a placeholder would create a cycle
// in the dependency graph.
var ErrCyclicDependency = errors.New("async: cyclic dependency detected")

// ErrNoRaceWinner is returned when a Race call completes with no successful result.
var ErrNoRaceWinner = errors.New("async: no race winner")
