package artifact

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// artifactFrontmatter is the YAML frontmatter for an artifact file.
type artifactFrontmatter struct {
	Name             string                        `yaml:"name"`
	Type             string                        `yaml:"type"`
	Version          string                        `yaml:"version,omitempty"`
	Description      string                        `yaml:"description,omitempty"`
	Author           string                        `yaml:"author,omitempty"`
	Tags             []string                      `yaml:"tags,omitempty"`
	DependsOn        []string                      `yaml:"depends_on,omitempty"`
	Condition        string                        `yaml:"condition,omitempty"`
	Tools            []ToolDef                     `yaml:"tools,omitempty"`
	Hooks            []HookDef                     `yaml:"hooks,omitempty"`
	Models           []ModelDef                    `yaml:"models,omitempty"`
	Compaction       CompactionDef                 `yaml:"compaction,omitempty"`
	Triggers         []CompactionTrigger           `yaml:"triggers,omitempty"`
	Retention        CompactionRetention           `yaml:"retention,omitempty"`
	Strategies       map[string]CompactionStrategy `yaml:"strategies,omitempty"`
	PriorityOverride int                           `yaml:"priority,omitempty"`
}

// LoadFile reads and parses a single artifact file.
func LoadFile(path string) (*Artifact, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read artifact %s: %w", path, err)
	}
	return Parse(data, path)
}

// Parse parses an artifact from markdown bytes.
// The file must have YAML frontmatter (--- delimited) containing artifact metadata.
// The markdown body becomes the artifact's context.
func Parse(data []byte, source string) (*Artifact, error) {
	frontmatter, body, err := splitFrontmatter(data)
	if err != nil {
		return nil, fmt.Errorf("parse artifact frontmatter: %w", err)
	}

	var fm artifactFrontmatter
	if err := yaml.Unmarshal(frontmatter, &fm); err != nil {
		return nil, fmt.Errorf("parse artifact YAML: %w", err)
	}

	artifactType, err := ParseType(fm.Type)
	if err != nil {
		return nil, fmt.Errorf("artifact %q: %w", fm.Name, err)
	}

	a := &Artifact{
		Metadata: Metadata{
			Name:        strings.TrimSpace(fm.Name),
			Type:        artifactType,
			Version:     strings.TrimSpace(fm.Version),
			Description: strings.TrimSpace(fm.Description),
			Author:      strings.TrimSpace(fm.Author),
			Tags:        fm.Tags,
			DependsOn:   fm.DependsOn,
			CreatedAt:   time.Now(),
		},
		Condition:        strings.TrimSpace(fm.Condition),
		Context:          strings.TrimSpace(body),
		Tools:            fm.Tools,
		Hooks:            fm.Hooks,
		Models:           fm.Models,
		Compaction:       fm.Compaction,
		Source:           source,
		PriorityOverride: fm.PriorityOverride,
	}
	if len(fm.Triggers) > 0 {
		a.Compaction.Triggers = fm.Triggers
	}
	if len(fm.Retention.AlwaysKeep) > 0 || len(fm.Retention.Summarize) > 0 || len(fm.Retention.Drop) > 0 {
		a.Compaction.Retention = fm.Retention
	}
	if len(fm.Strategies) > 0 {
		a.Compaction.Strategies = fm.Strategies
	}

	return a, nil
}

// LoadDir loads all artifact .md files from a directory (non-recursive).
func LoadDir(dir string) ([]*Artifact, error) {
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		return nil, nil
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read artifact dir %s: %w", dir, err)
	}

	artifacts := make([]*Artifact, 0)
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if !strings.EqualFold(filepath.Ext(entry.Name()), ".md") {
			continue
		}

		a, err := LoadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			return nil, fmt.Errorf("load artifact %s: %w", entry.Name(), err)
		}
		artifacts = append(artifacts, a)
	}

	return artifacts, nil
}

// LoadTree loads artifacts from a directory tree with convention-based paths:
//
//	baseDir/
//	  identity.md      → type: harness (auto-detected)
//	  builtins/*.md    → type: builtin
//	  plugins/*.md     → type: plugin
//	  models/*.md      → type: model
//	  overrides/*.md   → type: override
//
// Files declare their own type in frontmatter; the directory is advisory.
func LoadTree(baseDir string) ([]*Artifact, error) {
	dirs := []string{
		baseDir,
	}
	subdirs := []string{"builtins", "plugins", "models", "compaction", "overrides"}
	for _, sub := range subdirs {
		dirs = append(dirs, filepath.Join(baseDir, sub))
	}

	all := make([]*Artifact, 0)
	seen := make(map[string]bool)

	for _, dir := range dirs {
		artifacts, err := LoadDir(dir)
		if err != nil {
			return nil, err
		}
		for _, a := range artifacts {
			if seen[a.Source] {
				continue
			}
			seen[a.Source] = true
			all = append(all, a)
		}
	}

	return all, nil
}

// LoadAndRegister loads all artifacts from a directory tree and registers them.
func LoadAndRegister(baseDir string) (*Registry, error) {
	artifacts, err := LoadTree(baseDir)
	if err != nil {
		return nil, err
	}

	reg := NewRegistry()
	for _, a := range artifacts {
		if err := reg.Register(a); err != nil {
			return nil, fmt.Errorf("register %s: %w", a.Source, err)
		}
	}

	if err := reg.ValidateDependencies(); err != nil {
		return nil, err
	}

	return reg, nil
}

// splitFrontmatter extracts YAML frontmatter and markdown body.
func splitFrontmatter(data []byte) ([]byte, string, error) {
	content := strings.TrimPrefix(string(data), "\uFEFF")
	content = strings.ReplaceAll(content, "\r\n", "\n")

	lines := strings.Split(content, "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		return nil, "", fmt.Errorf("artifact file must start with --- frontmatter delimiter")
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
