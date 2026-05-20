// Package evals provides an evaluation framework for validating the AI harness
// against real LLM models. Eval cases are defined in YAML files and executed
// against real completion APIs to verify tool use, delegation, hooks, and
// self-healing behavior.
//
// This package uses the "eval" build tag and is NOT included in normal
// go test ./... runs. Run with: go test -tags=eval ./evals/
package evals
