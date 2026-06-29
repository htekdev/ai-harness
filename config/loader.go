package config

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/htekdev/ai-harness/artifactsource"
	"github.com/htekdev/ai-harness/harness/errs"
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
	return LoadDirectoryWithSources(baseDir, nil, nil, false)
}

// LoadDirectoryWithSources scans .harness plus any configured artifact sources.
func LoadDirectoryWithSources(baseDir string, sources []artifactsource.SourceSpec, trustedSources []string, refresh bool) (*LoadResult, error) {
	result := &LoadResult{
		Config: &Config{},
		Agents: make(map[string]*AgentConfig),
	}

	roots, err := artifactsource.ResolveSourceRoots(baseDir, sources, artifactsource.ResolveOptions{
		TrustedSources: trustedSources,
		Offline:        strings.EqualFold(os.Getenv("HARNESS_OFFLINE"), "1") || strings.EqualFold(os.Getenv("HARNESS_OFFLINE"), "true"),
		Refresh:        refresh,
	})
	if err != nil {
		return nil, errs.Wrap(errs.KindConfig, "config.loaddir", err, "resolve artifact sources")
	}

	for _, harnessDir := range roots {
		if _, err := os.Stat(harnessDir); os.IsNotExist(err) {
			continue
		}

		// Load tools from <root>/tools/
		toolsDir := filepath.Join(harnessDir, "tools")
		if tools, err := loadToolsFromDir(toolsDir); err != nil {
			return nil, errs.Wrap(errs.KindConfig, "config.loaddir", err, "load tools from %s", harnessDir)
		} else {
			result.Config.Tools = append(result.Config.Tools, tools...)
		}

		// Load hooks from <root>/hooks/
		hooksDir := filepath.Join(harnessDir, "hooks")
		if hooks, err := loadHooksFromDir(hooksDir); err != nil {
			return nil, errs.Wrap(errs.KindConfig, "config.loaddir", err, "load hooks from %s", harnessDir)
		} else {
			result.Config.Hooks = append(result.Config.Hooks, hooks...)
		}

		// Load bundles directly from <root>/ (useful when source.path points
		// at a plugins/builtins/overrides directory itself).
		rootTools, rootHooks, err := loadBundlesFromDir(harnessDir)
		if err != nil {
			return nil, errs.Wrap(errs.KindConfig, "config.loaddir", err, "load bundles from %s", harnessDir)
		}
		result.Config.Tools = append(result.Config.Tools, rootTools...)
		result.Config.Hooks = append(result.Config.Hooks, rootHooks...)

		// Load Shape A bundles from plugins/builtins/overrides.
		for _, sub := range []string{"plugins", "builtins", "overrides"} {
			bundleDir := filepath.Join(harnessDir, sub)
			bundleTools, bundleHooks, err := loadBundlesFromDir(bundleDir)
			if err != nil {
				return nil, errs.Wrap(errs.KindConfig, "config.loaddir", err, "load %s/%s", harnessDir, sub)
			}
			result.Config.Tools = append(result.Config.Tools, bundleTools...)
			result.Config.Hooks = append(result.Config.Hooks, bundleHooks...)
		}

		// Load agents from <root>/agents/
		agentsDir := filepath.Join(harnessDir, "agents")
		agents, err := loadAgentsFromDir(agentsDir)
		if err != nil {
			return nil, errs.Wrap(errs.KindConfig, "config.loaddir", err, "load agents from %s", harnessDir)
		}
		for name, agent := range agents {
			result.Agents[name] = agent
		}
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
		return nil, errs.Wrap(errs.KindConfig, "config.loadtools", err, "read directory %s", dir)
	}

	var tools []ToolConfig
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}

		name := strings.TrimSuffix(entry.Name(), ".md")
		data, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			return nil, errs.Wrap(errs.KindConfig, "config.loadtools", err, "read tool file %s", entry.Name())
		}

		tool, err := ParseToolMarkdown(data, name)
		if err != nil {
			return nil, errs.Wrap(errs.KindConfig, "config.loadtools", err, "parse tool %s", entry.Name())
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
		return nil, errs.Wrap(errs.KindConfig, "config.loadhooks", err, "read directory %s", dir)
	}

	var hooks []HookConfig
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}

		name := strings.TrimSuffix(entry.Name(), ".md")
		data, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			return nil, errs.Wrap(errs.KindConfig, "config.loadhooks", err, "read hook file %s", entry.Name())
		}

		hook, err := ParseHookMarkdown(data, name)
		if err != nil {
			return nil, errs.Wrap(errs.KindConfig, "config.loadhooks", err, "parse hook %s", entry.Name())
		}

		hooks = append(hooks, *hook)
	}

	return hooks, nil
}

// loadBundlesFromDir scans a directory for Shape A typed artifact
// bundle .md files. Each file's frontmatter may declare a `tools:`
// list, a `hooks:` list, or both — the loader returns the flat union
// of all of them so they can be merged into the main config.
//
// Bundles differ from `.harness/tools/` and `.harness/hooks/` files
// in that those directories assume one capability per file (with the
// filename providing the name); bundles are self-naming via the
// embedded YAML and may carry multiple capabilities per file.
func loadBundlesFromDir(dir string) ([]ToolConfig, []HookConfig, error) {
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		return nil, nil, nil
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, nil, errs.Wrap(errs.KindConfig, "config.loadbundles", err, "read directory %s", dir)
	}

	var tools []ToolConfig
	var hooks []HookConfig
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}

		data, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			return nil, nil, errs.Wrap(errs.KindConfig, "config.loadbundles", err, "read bundle file %s", entry.Name())
		}

		bundleTools, bundleHooks, err := ParseBundleMarkdown(data)
		if err != nil {
			return nil, nil, errs.Wrap(errs.KindConfig, "config.loadbundles", err, "parse bundle %s", entry.Name())
		}

		tools = append(tools, bundleTools...)
		hooks = append(hooks, bundleHooks...)
	}

	return tools, hooks, nil
}

// loadAgentsFromDir scans a directory for .md files and parses each as an agent.
func loadAgentsFromDir(dir string) (map[string]*AgentConfig, error) {
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		return nil, nil
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, errs.Wrap(errs.KindConfig, "config.loadagents", err, "read directory %s", dir)
	}

	agents := make(map[string]*AgentConfig)
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}

		name := strings.TrimSuffix(entry.Name(), ".md")
		data, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			return nil, errs.Wrap(errs.KindConfig, "config.loadagents", err, "read agent file %s", entry.Name())
		}

		agent, err := ParseAgentMarkdown(data, name)
		if err != nil {
			return nil, errs.Wrap(errs.KindConfig, "config.loadagents", err, "parse agent %s", entry.Name())
		}

		if _, exists := agents[name]; exists {
			return nil, errs.Newf(errs.KindConfig, "config.loadagents", "duplicate agent name %q", name)
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
	dirResult, err := LoadDirectoryWithSources(baseDir, cfg.ArtifactSources, cfg.TrustedSources, false)
	if err != nil {
		return nil, nil, errs.Wrap(errs.KindConfig, "config.loadfull", err, "load .harness directory")
	}

	MergeDirectoryResult(cfg, dirResult)

	return cfg, dirResult.Agents, nil
}
