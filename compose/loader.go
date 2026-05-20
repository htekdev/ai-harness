package compose

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

type blockFrontmatter struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
	Condition   string `yaml:"condition"`
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

// LoadIdentity loads the base identity prompt from .harness/identity.md.
func LoadIdentity(baseDir string) (string, error) {
	return loadIdentityFile(filepath.Join(baseDir, ".harness", "identity.md"))
}

// LoadAgentIdentity loads the agent identity prompt from .harness/agents/{name}/identity.md.
func LoadAgentIdentity(baseDir, agentName string) (string, error) {
	if strings.TrimSpace(agentName) == "" {
		return "", nil
	}
	return loadIdentityFile(filepath.Join(baseDir, ".harness", "agents", agentName, "identity.md"))
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

	contextText, tools, hooks, err := parseStructuredSections(body)
	if err != nil {
		return Block{}, err
	}

	return Block{
		Name:        strings.TrimSpace(fm.Name),
		Description: strings.TrimSpace(fm.Description),
		Condition:   strings.TrimSpace(fm.Condition),
		Context:     strings.TrimSpace(contextText),
		Tools:       tools,
		Hooks:       hooks,
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

func loadIdentityFile(path string) (string, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("read identity %s: %w", path, err)
	}
	content := strings.ReplaceAll(string(data), "\r\n", "\n")
	return strings.TrimSpace(content), nil
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

func parseStructuredSections(body string) (string, []ToolDef, []HookDef, error) {
	body = strings.ReplaceAll(body, "\r\n", "\n")
	lines := strings.Split(body, "\n")

	kept := make([]string, 0, len(lines))
	tools := make([]ToolDef, 0)
	hooks := make([]HookDef, 0)

	for i := 0; i < len(lines); {
		switch strings.TrimSpace(lines[i]) {
		case "## Tools":
			yamlBlock, next, err := extractFencedYAML(lines, i+1, "Tools")
			if err != nil {
				return "", nil, nil, err
			}
			var defs []ToolDef
			if strings.TrimSpace(yamlBlock) != "" {
				if err := yaml.Unmarshal([]byte(yamlBlock), &defs); err != nil {
					return "", nil, nil, fmt.Errorf("parse tools section: %w", err)
				}
			}
			tools = append(tools, defs...)
			i = next
		case "## Hooks":
			yamlBlock, next, err := extractFencedYAML(lines, i+1, "Hooks")
			if err != nil {
				return "", nil, nil, err
			}
			var defs []HookDef
			if strings.TrimSpace(yamlBlock) != "" {
				if err := yaml.Unmarshal([]byte(yamlBlock), &defs); err != nil {
					return "", nil, nil, fmt.Errorf("parse hooks section: %w", err)
				}
			}
			hooks = append(hooks, defs...)
			i = next
		default:
			kept = append(kept, lines[i])
			i++
		}
	}

	return strings.TrimSpace(strings.Join(kept, "\n")), tools, hooks, nil
}

func extractFencedYAML(lines []string, start int, section string) (string, int, error) {
	i := start
	for i < len(lines) && strings.TrimSpace(lines[i]) == "" {
		i++
	}
	if i >= len(lines) {
		return "", i, fmt.Errorf("%s section missing fenced YAML block", section)
	}

	fence := strings.TrimSpace(lines[i])
	if !strings.HasPrefix(fence, "```") {
		return "", i, fmt.Errorf("%s section must start with a fenced YAML block", section)
	}
	lang := strings.TrimSpace(strings.TrimPrefix(fence, "```"))
	if lang != "" && lang != "yaml" && lang != "yml" {
		return "", i, fmt.Errorf("%s section must use a YAML fenced block", section)
	}

	end := -1
	for j := i + 1; j < len(lines); j++ {
		if strings.HasPrefix(strings.TrimSpace(lines[j]), "```") {
			end = j
			break
		}
	}
	if end == -1 {
		return "", i, fmt.Errorf("%s section fenced YAML block is not closed", section)
	}

	return strings.Join(lines[i+1:end], "\n"), end + 1, nil
}
