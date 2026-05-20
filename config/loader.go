package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// LoadResult holds the fully assembled configuration including file-based additions.
type LoadResult struct {
	Config *Config
	Agents map[string]*AgentConfig
}

// LoadDirectory scans a .harness/ directory structure and returns additional
// tools, hooks, and agents to be merged with the main config.
// This is ADDITIVE — it never replaces inline definitions.
func LoadDirectory(baseDir string) (*LoadResult, error) {
	result := &LoadResult{
		Config: &Config{},
		Agents: make(map[string]*AgentConfig),
	}

	harnessDir := filepath.Join(baseDir, ".harness")
	if _, err := os.Stat(harnessDir); os.IsNotExist(err) {
		return result, nil // .harness/ doesn't exist, that's fine
	}

	// Load tools from .harness/tools/
	toolsDir := filepath.Join(harnessDir, "tools")
	if tools, err := loadToolsFromDir(toolsDir); err != nil {
		return nil, fmt.Errorf("load .harness/tools: %w", err)
	} else {
		result.Config.Tools = tools
	}

	// Load hooks from .harness/hooks/
	hooksDir := filepath.Join(harnessDir, "hooks")
	if hooks, err := loadHooksFromDir(hooksDir); err != nil {
		return nil, fmt.Errorf("load .harness/hooks: %w", err)
	} else {
		result.Config.Hooks = hooks
	}

	// Load agents from .harness/agents/
	agentsDir := filepath.Join(harnessDir, "agents")
	if agents, err := loadAgentsFromDir(agentsDir); err != nil {
		return nil, fmt.Errorf("load .harness/agents: %w", err)
	} else {
		result.Agents = agents
	}

	return result, nil
}

// loadToolsFromDir scans a directory for .md files and parses each as a tool.
func loadToolsFromDir(dir string) ([]ToolConfig, error) {
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		return nil, nil
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read directory %s: %w", dir, err)
	}

	var tools []ToolConfig
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}

		name := strings.TrimSuffix(entry.Name(), ".md")
		data, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			return nil, fmt.Errorf("read tool file %s: %w", entry.Name(), err)
		}

		tool, err := ParseToolMarkdown(data, name)
		if err != nil {
			return nil, fmt.Errorf("parse tool %s: %w", entry.Name(), err)
		}

		tools = append(tools, *tool)
	}

	return tools, nil
}

// loadHooksFromDir scans a directory for .md files and parses each as a hook.
func loadHooksFromDir(dir string) ([]HookConfig, error) {
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		return nil, nil
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read directory %s: %w", dir, err)
	}

	var hooks []HookConfig
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}

		name := strings.TrimSuffix(entry.Name(), ".md")
		data, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			return nil, fmt.Errorf("read hook file %s: %w", entry.Name(), err)
		}

		hook, err := ParseHookMarkdown(data, name)
		if err != nil {
			return nil, fmt.Errorf("parse hook %s: %w", entry.Name(), err)
		}

		hooks = append(hooks, *hook)
	}

	return hooks, nil
}

// loadAgentsFromDir scans a directory for .md files and parses each as an agent.
func loadAgentsFromDir(dir string) (map[string]*AgentConfig, error) {
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		return nil, nil
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read directory %s: %w", dir, err)
	}

	agents := make(map[string]*AgentConfig)
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}

		name := strings.TrimSuffix(entry.Name(), ".md")
		data, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			return nil, fmt.Errorf("read agent file %s: %w", entry.Name(), err)
		}

		agent, err := ParseAgentMarkdown(data, name)
		if err != nil {
			return nil, fmt.Errorf("parse agent %s: %w", entry.Name(), err)
		}

		if _, exists := agents[name]; exists {
			return nil, fmt.Errorf("duplicate agent name %q", name)
		}
		agents[name] = agent
	}

	return agents, nil
}

// MergeDirectoryResult merges file-based tools/hooks into the main config.
// File-based definitions are additive. On name collision, file definition wins.
func MergeDirectoryResult(cfg *Config, dirResult *LoadResult) {
	if dirResult == nil || dirResult.Config == nil {
		return
	}

	// Merge tools: file-based tools override inline on name collision
	existingTools := make(map[string]int, len(cfg.Tools))
	for i, t := range cfg.Tools {
		existingTools[t.Name] = i
	}
	for _, fileTool := range dirResult.Config.Tools {
		if idx, exists := existingTools[fileTool.Name]; exists {
			cfg.Tools[idx] = fileTool // override
		} else {
			cfg.Tools = append(cfg.Tools, fileTool)
		}
	}

	// Merge hooks: file-based hooks override inline on handler name collision
	existingHooks := make(map[string]int, len(cfg.Hooks))
	for i, h := range cfg.Hooks {
		existingHooks[h.Handler] = i
	}
	for _, fileHook := range dirResult.Config.Hooks {
		if idx, exists := existingHooks[fileHook.Handler]; exists {
			cfg.Hooks[idx] = fileHook // override
		} else {
			cfg.Hooks = append(cfg.Hooks, fileHook)
		}
	}
}

// LoadFull loads a harness.md (or .yaml) and merges in .harness/ directory contents.
// This is the primary entry point for loading a complete harness configuration.
func LoadFull(configPath string) (*Config, map[string]*AgentConfig, error) {
	cfg, err := LoadAuto(configPath)
	if err != nil {
		return nil, nil, err
	}

	baseDir := filepath.Dir(configPath)
	dirResult, err := LoadDirectory(baseDir)
	if err != nil {
		return nil, nil, fmt.Errorf("load .harness directory: %w", err)
	}

	MergeDirectoryResult(cfg, dirResult)

	return cfg, dirResult.Agents, nil
}
