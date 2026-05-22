package evals

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"gopkg.in/yaml.v3"
)

// EvalCase represents a single evaluation scenario loaded from a YAML file.
type EvalCase struct {
	Name        string           `yaml:"name"`
	Description string           `yaml:"description"`
	Category    string           `yaml:"category"`
	Model       string           `yaml:"model"`
	MaxTokens   int              `yaml:"max_tokens"`
	Timeout     time.Duration    `yaml:"timeout"`
	Setup       CaseSetup        `yaml:"setup"`
	Turns       []CaseTurn       `yaml:"turns"`
	Grade       []GradeCriterion `yaml:"grade"`
}

// CaseSetup defines the harness configuration for an eval case.
type CaseSetup struct {
	SystemPrompt string          `yaml:"system_prompt"`
	Tools        []CaseTool      `yaml:"tools"`
	Hooks        []CaseHook      `yaml:"hooks"`
	Delegation   *CaseDelegation `yaml:"delegation,omitempty"`
}

// CaseTool defines a tool available in the eval harness.
type CaseTool struct {
	Name        string               `yaml:"name"`
	Description string               `yaml:"description"`
	Parameters  map[string]CaseParam `yaml:"parameters"`
	Script      string               `yaml:"script"`
	// ErrorOnCall makes the tool return an error (for testing error recovery).
	ErrorOnCall int `yaml:"error_on_call,omitempty"`
}

// CaseParam defines a tool parameter.
type CaseParam struct {
	Type        string `yaml:"type"`
	Description string `yaml:"description,omitempty"`
	Required    bool   `yaml:"required"`
}

// CaseHook defines a hook for the eval harness.
type CaseHook struct {
	Name     string `yaml:"name"`
	Event    string `yaml:"event"`
	When     string `yaml:"when,omitempty"`
	Script   string `yaml:"script"`
	Priority int    `yaml:"priority,omitempty"`
}

// CaseDelegation configures delegation behavior.
type CaseDelegation struct {
	MaxDepth           int   `yaml:"max_depth"`
	IterationsPerDepth []int `yaml:"iterations_per_depth"`
}

// CaseTurn represents a single conversation turn in an eval.
type CaseTurn struct {
	Role    string `yaml:"role"`
	Content string `yaml:"content"`
}

// GradeCriterion defines a single grading check.
type GradeCriterion struct {
	Type  string `yaml:"type"`
	Tool  string `yaml:"tool,omitempty"`
	Value string `yaml:"value,omitempty"`
	// ArgsContain checks that tool call arguments contain these key-value pairs.
	ArgsContain map[string]interface{} `yaml:"args_contain,omitempty"`
	// Count for tool_call_count grader.
	Count int `yaml:"count,omitempty"`
	// MaxValue for max_tool_iterations grader.
	MaxValue int `yaml:"max_value,omitempty"`
}

// UnmarshalYAML implements custom unmarshaling for Duration fields.
func (e *EvalCase) UnmarshalYAML(node *yaml.Node) error {
	// Use a type alias to avoid infinite recursion
	type rawCase struct {
		Name        string           `yaml:"name"`
		Description string           `yaml:"description"`
		Category    string           `yaml:"category"`
		Model       string           `yaml:"model"`
		MaxTokens   int              `yaml:"max_tokens"`
		Timeout     string           `yaml:"timeout"`
		Setup       CaseSetup        `yaml:"setup"`
		Turns       []CaseTurn       `yaml:"turns"`
		Grade       []GradeCriterion `yaml:"grade"`
	}

	var raw rawCase
	if err := node.Decode(&raw); err != nil {
		return err
	}

	e.Name = raw.Name
	e.Description = raw.Description
	e.Category = raw.Category
	e.Model = raw.Model
	e.MaxTokens = raw.MaxTokens
	e.Setup = raw.Setup
	e.Turns = raw.Turns
	e.Grade = raw.Grade

	if raw.Timeout != "" {
		d, err := time.ParseDuration(raw.Timeout)
		if err != nil {
			return fmt.Errorf("invalid timeout %q: %w", raw.Timeout, err)
		}
		e.Timeout = d
	}

	return nil
}

// UnmarshalYAML for GradeCriterion to handle flexible value types.
func (g *GradeCriterion) UnmarshalYAML(node *yaml.Node) error {
	type rawGrade struct {
		Type        string                 `yaml:"type"`
		Tool        string                 `yaml:"tool,omitempty"`
		Value       interface{}            `yaml:"value,omitempty"`
		ArgsContain map[string]interface{} `yaml:"args_contain,omitempty"`
		Count       int                    `yaml:"count,omitempty"`
		MaxValue    int                    `yaml:"max_value,omitempty"`
	}

	var raw rawGrade
	if err := node.Decode(&raw); err != nil {
		return err
	}

	g.Type = raw.Type
	g.Tool = raw.Tool
	g.ArgsContain = raw.ArgsContain
	g.Count = raw.Count
	g.MaxValue = raw.MaxValue

	// Convert value to string regardless of YAML type
	switch v := raw.Value.(type) {
	case string:
		g.Value = v
	case int:
		g.Value = fmt.Sprintf("%d", v)
	case float64:
		g.Value = fmt.Sprintf("%v", v)
	case bool:
		g.Value = fmt.Sprintf("%t", v)
	case nil:
		g.Value = ""
	default:
		data, _ := json.Marshal(v)
		g.Value = string(data)
	}

	return nil
}

// LoadCase reads and parses a single eval case YAML file.
func LoadCase(path string) (*EvalCase, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read case %s: %w", path, err)
	}

	var c EvalCase
	if err := yaml.Unmarshal(data, &c); err != nil {
		return nil, fmt.Errorf("parse case %s: %w", path, err)
	}

	// Apply defaults
	if c.MaxTokens == 0 {
		c.MaxTokens = 500
	}
	if c.Timeout == 0 {
		c.Timeout = 30 * time.Second
	}
	if c.Model == "" {
		c.Model = "gpt-4o-mini"
	}

	return &c, nil
}

// LoadCases reads all YAML eval cases from a directory.
func LoadCases(dir string) ([]*EvalCase, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read cases dir %s: %w", dir, err)
	}

	var cases []*EvalCase
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		ext := filepath.Ext(entry.Name())
		if ext != ".yaml" && ext != ".yml" {
			continue
		}
		c, err := LoadCase(filepath.Join(dir, entry.Name()))
		if err != nil {
			return nil, err
		}
		cases = append(cases, c)
	}

	return cases, nil
}
