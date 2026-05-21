package compose

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// blockFrontmatter is the YAML frontmatter of a composable block.
// Everything configurable lives here — tools, hooks, conditions.
// The markdown body is pure context (prose for the agent).
type blockFrontmatter struct {
	Name        string    `yaml:"name"`
	Description string    `yaml:"description"`
	Condition   string    `yaml:"condition"`
	Tools       []ToolDef `yaml:"tools,omitempty"`
	Hooks       []HookDef `yaml:"hooks,omitempty"`
}

// LoadBlocks loads base composable blocks from .harness/*.md, excluding identity.md.
func LoadBlocks(baseDir string) ([]Block, error) {
	return loadBlocksFromDir(filepath.Join(baseDir, ".harness"))
}

// LoadAgentBlocks loads agent-specific composable blocks from .harness/agents/{name}/*.md.
func LoadAgentBlocks(baseDir, agentName string) ([]Block, error) {
	if strings.TrimSpace(agentName) == "" {
		return nil, nil
	}
	return loadBlocksFromDir(filepath.Join(baseDir, ".harness", "agents", agentName))
}

// LoadIdentity loads the base identity prompt from .harness/identity.md body.
func LoadIdentity(baseDir string) (string, error) {
	return loadIdentityBody(filepath.Join(baseDir, ".harness", "identity.md"))
}

// LoadAgentIdentity loads the agent identity prompt from .harness/agents/{name}/identity.md body.
func LoadAgentIdentity(baseDir, agentName string) (string, error) {
	if strings.TrimSpace(agentName) == "" {
		return "", nil
	}
	return loadIdentityBody(filepath.Join(baseDir, ".harness", "agents", agentName, "identity.md"))
}

// LoadBlockFile reads and parses a composable block file.
func LoadBlockFile(path string) (Block, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Block{}, fmt.Errorf("read block %s: %w", path, err)
	}
	return ParseBlock(data, path)
}

// ParseBlock parses a composable markdown block from bytes.
// Frontmatter contains all config (name, condition, tools, hooks).
// Body is pure context — no structured sections.
func ParseBlock(data []byte, source string) (Block, error) {
	frontmatter, body, err := splitFrontmatter(data)
	if err != nil {
		return Block{}, fmt.Errorf("parse frontmatter: %w", err)
	}

	var fm blockFrontmatter
	if err := yaml.Unmarshal(frontmatter, &fm); err != nil {
		return Block{}, fmt.Errorf("parse block frontmatter: %w", err)
	}
	if strings.TrimSpace(fm.Name) == "" {
		return Block{}, fmt.Errorf("block name is required")
	}

	return Block{
		Name:        strings.TrimSpace(fm.Name),
		Description: strings.TrimSpace(fm.Description),
		Condition:   strings.TrimSpace(fm.Condition),
		Context:     strings.TrimSpace(body),
		Tools:       fm.Tools,
		Hooks:       fm.Hooks,
		Source:      source,
	}, nil
}

func loadBlocksFromDir(dir string) ([]Block, error) {
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		return nil, nil
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read directory %s: %w", dir, err)
	}

	blocks := make([]Block, 0)
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if !strings.EqualFold(filepath.Ext(entry.Name()), ".md") || strings.EqualFold(entry.Name(), "identity.md") {
			continue
		}

		block, err := LoadBlockFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			return nil, fmt.Errorf("load block %s: %w", entry.Name(), err)
		}
		blocks = append(blocks, block)
	}

	return blocks, nil
}

func loadIdentityBody(path string) (string, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("read identity %s: %w", path, err)
	}

	// Identity files may have frontmatter (for policy) — we only return the body.
	_, body, err := splitFrontmatter(data)
	if err != nil {
		// If no frontmatter, the entire content is the identity.
		content := strings.ReplaceAll(string(data), "\r\n", "\n")
		return strings.TrimSpace(content), nil
	}
	return strings.TrimSpace(body), nil
}

func splitFrontmatter(data []byte) ([]byte, string, error) {
	content := strings.TrimPrefix(string(data), "\uFEFF")
	content = strings.ReplaceAll(content, "\r\n", "\n")

	lines := strings.Split(content, "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		return nil, "", fmt.Errorf("markdown file must start with --- frontmatter delimiter")
	}

	closing := -1
	for i := 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "---" {
			closing = i
			break
		}
	}
	if closing == -1 {
		return nil, "", fmt.Errorf("no closing --- delimiter found for frontmatter")
	}

	frontmatter := strings.Join(lines[1:closing], "\n")
	body := ""
	if closing+1 < len(lines) {
		body = strings.Join(lines[closing+1:], "\n")
	}
	return []byte(strings.TrimSpace(frontmatter)), body, nil
}
