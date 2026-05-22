// Markdown frontmatter parsing for the AI harness configuration system.
// Markdown files use YAML frontmatter (between --- delimiters) for configuration
// and the markdown body becomes the system prompt.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// MarkdownDoc represents a parsed markdown file with frontmatter.
type MarkdownDoc struct {
	// Frontmatter is the raw YAML content between --- delimiters.
	Frontmatter []byte
	// Body is the markdown content after the frontmatter.
	Body string
}

// ParseMarkdown splits a markdown file into YAML frontmatter and body.
// The frontmatter must be delimited by --- at the start and end.
func ParseMarkdown(data []byte) (*MarkdownDoc, error) {
	content := string(data)

	// Must start with ---
	if !strings.HasPrefix(strings.TrimSpace(content), "---") {
		return nil, fmt.Errorf("markdown file must start with --- frontmatter delimiter")
	}

	// Find the opening ---
	trimmed := strings.TrimSpace(content)
	// Skip the first ---
	afterFirst := trimmed[3:]
	// Skip any trailing characters on the --- line (e.g., newline)
	if idx := strings.IndexByte(afterFirst, '\n'); idx >= 0 {
		afterFirst = afterFirst[idx+1:]
	} else {
		return nil, fmt.Errorf("no content after opening --- delimiter")
	}

	// Find the closing ---
	closingIdx := strings.Index(afterFirst, "\n---")
	if closingIdx < 0 {
		// Check if it ends with --- (no trailing newline after frontmatter)
		if strings.HasSuffix(strings.TrimRight(afterFirst, "\r\n"), "---") {
			// All frontmatter, no body
			fm := strings.TrimRight(afterFirst, "\r\n")
			fm = fm[:len(fm)-3]
			return &MarkdownDoc{
				Frontmatter: []byte(strings.TrimRight(fm, "\r\n")),
				Body:        "",
			}, nil
		}
		return nil, fmt.Errorf("no closing --- delimiter found for frontmatter")
	}

	frontmatter := afterFirst[:closingIdx]
	// Body starts after the closing --- line
	rest := afterFirst[closingIdx+4:] // skip \n---
	// Skip any trailing characters on the --- line
	if idx := strings.IndexByte(rest, '\n'); idx >= 0 {
		rest = rest[idx+1:]
	} else {
		rest = ""
	}

	body := strings.TrimSpace(rest)

	return &MarkdownDoc{
		Frontmatter: []byte(frontmatter),
		Body:        body,
	}, nil
}

// LoadMarkdown loads a markdown file and parses it into a Config with the body as system prompt.
func LoadMarkdown(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read markdown file: %w", err)
	}

	doc, err := ParseMarkdown(data)
	if err != nil {
		return nil, fmt.Errorf("parse markdown %s: %w", path, err)
	}

	cfg, err := Parse(doc.Frontmatter)
	if err != nil {
		return nil, fmt.Errorf("parse frontmatter in %s: %w", path, err)
	}

	// Markdown body becomes the system prompt
	if doc.Body != "" {
		cfg.Context.SystemPrompt = doc.Body
	}

	return cfg, nil
}

// LoadAuto detects file format by extension and loads accordingly.
// Supports .md (markdown with frontmatter) and .yaml/.yml (plain YAML).
func LoadAuto(path string) (*Config, error) {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".md":
		return LoadMarkdown(path)
	case ".yaml", ".yml":
		return Load(path)
	default:
		return nil, fmt.Errorf("unsupported config file extension %q (use .md, .yaml, or .yml)", ext)
	}
}

// ParseToolMarkdown parses a tool .md file. The filename is the tool name,
// frontmatter has parameters/script, body is the description.
func ParseToolMarkdown(data []byte, name string) (*ToolConfig, error) {
	doc, err := ParseMarkdown(data)
	if err != nil {
		return nil, err
	}

	// Parse frontmatter as a partial tool config (no name/description)
	type toolFrontmatter struct {
		Parameters map[string]ParamConfig `yaml:"parameters"`
		Script     string                 `yaml:"script"`
		TimeoutMS  int                    `yaml:"timeout_ms,omitempty"`
		Async      bool                   `yaml:"async,omitempty"`
	}

	var fm toolFrontmatter
	if err := yamlUnmarshal(doc.Frontmatter, &fm); err != nil {
		return nil, fmt.Errorf("parse tool frontmatter: %w", err)
	}

	description := doc.Body
	if description == "" {
		description = name
	}

	return &ToolConfig{
		Name:        name,
		Description: description,
		Parameters:  fm.Parameters,
		Script:      fm.Script,
		TimeoutMS:   fm.TimeoutMS,
	}, nil
}

// ParseHookMarkdown parses a hook .md file. The filename is the hook handler name,
// frontmatter has event/priority/when/script, body is documentation.
func ParseHookMarkdown(data []byte, name string) (*HookConfig, error) {
	doc, err := ParseMarkdown(data)
	if err != nil {
		return nil, err
	}

	type hookFrontmatter struct {
		Event    string `yaml:"event"`
		Priority int    `yaml:"priority,omitempty"`
		When     string `yaml:"when,omitempty"`
		Script   string `yaml:"script"`
	}

	var fm hookFrontmatter
	if err := yamlUnmarshal(doc.Frontmatter, &fm); err != nil {
		return nil, fmt.Errorf("parse hook frontmatter: %w", err)
	}

	if fm.Event == "" {
		return nil, fmt.Errorf("hook %q: event field is required in frontmatter", name)
	}

	return &HookConfig{
		Event:    fm.Event,
		Handler:  name,
		Script:   fm.Script,
		When:     fm.When,
		Priority: fm.Priority,
	}, nil
}

// AgentConfig defines a custom agent loaded from a .md file.
type AgentConfig struct {
	Name         string      `yaml:"name" json:"name"`
	Description  string      `yaml:"description" json:"description"`
	Model        string      `yaml:"model" json:"model"`
	SystemPrompt string      `yaml:"-" json:"-"` // from markdown body
	Tools        []AgentTool `yaml:"tools" json:"tools"`
	Hooks        []AgentHook `yaml:"hooks" json:"hooks"`
}

// AgentTool can be either a string (reference) or an inline ToolConfig.
type AgentTool struct {
	// Ref is a reference to a tool by name (loaded from .harness/tools/).
	Ref string
	// Inline is a full inline tool definition.
	Inline *ToolConfig
}

// UnmarshalYAML handles both string references and inline tool objects.
func (at *AgentTool) UnmarshalYAML(unmarshal func(interface{}) error) error {
	// Try string first
	var ref string
	if err := unmarshal(&ref); err == nil {
		at.Ref = ref
		return nil
	}

	// Try inline tool config
	var tc ToolConfig
	if err := unmarshal(&tc); err != nil {
		return fmt.Errorf("agent tool must be a string reference or inline tool config: %w", err)
	}
	at.Inline = &tc
	return nil
}

// AgentHook can be either a string (reference) or an inline HookConfig.
type AgentHook struct {
	// Ref is a reference to a hook by name (loaded from .harness/hooks/).
	Ref string
	// Inline is a full inline hook definition.
	Inline *HookConfig
}

// UnmarshalYAML handles both string references and inline hook objects.
func (ah *AgentHook) UnmarshalYAML(unmarshal func(interface{}) error) error {
	// Try string first
	var ref string
	if err := unmarshal(&ref); err == nil {
		ah.Ref = ref
		return nil
	}

	// Try inline hook config
	var hc HookConfig
	if err := unmarshal(&hc); err != nil {
		return fmt.Errorf("agent hook must be a string reference or inline hook config: %w", err)
	}
	ah.Inline = &hc
	return nil
}

// ParseAgentMarkdown parses an agent .md file. The filename is the agent name,
// frontmatter has model/tools/hooks, body is the system prompt.
func ParseAgentMarkdown(data []byte, name string) (*AgentConfig, error) {
	doc, err := ParseMarkdown(data)
	if err != nil {
		return nil, err
	}

	type agentFrontmatter struct {
		Description string      `yaml:"description"`
		Model       string      `yaml:"model"`
		Tools       []AgentTool `yaml:"tools"`
		Hooks       []AgentHook `yaml:"hooks"`
	}

	var fm agentFrontmatter
	if err := yamlUnmarshal(doc.Frontmatter, &fm); err != nil {
		return nil, fmt.Errorf("parse agent frontmatter: %w", err)
	}

	return &AgentConfig{
		Name:         name,
		Description:  fm.Description,
		Model:        fm.Model,
		SystemPrompt: doc.Body,
		Tools:        fm.Tools,
		Hooks:        fm.Hooks,
	}, nil
}

// yamlUnmarshal is a helper wrapping yaml.Unmarshal.
func yamlUnmarshal(data []byte, v interface{}) error {
	return yaml.Unmarshal(data, v)
}
