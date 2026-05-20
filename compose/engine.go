package compose

import "fmt"

// Engine resolves composable harness configuration from a base directory.
type Engine struct {
	BaseDir string
}

// NewEngine creates a composition engine rooted at baseDir.
func NewEngine(baseDir string) *Engine {
	return &Engine{BaseDir: baseDir}
}

// Resolve resolves the base harness or an agent-specific harness when agentName is set.
func (e *Engine) Resolve(agentName string, ctx ConditionContext) (*ResolvedHarness, error) {
	if e == nil {
		return nil, fmt.Errorf("engine cannot be nil")
	}
	return ResolveHarness(e.BaseDir, agentName, ctx)
}

// ResolveHarness resolves the composed harness for the provided base directory.
func ResolveHarness(baseDir, agentName string, ctx ConditionContext) (*ResolvedHarness, error) {
	policy, err := ResolvePolicy(baseDir, agentName)
	if err != nil {
		return nil, err
	}

	identity, err := LoadIdentity(baseDir)
	if err != nil {
		return nil, err
	}
	if agentName != "" {
		agentIdentity, err := LoadAgentIdentity(baseDir, agentName)
		if err != nil {
			return nil, err
		}
		if agentIdentity != "" {
			identity = agentIdentity
		}
	}

	baseBlocks, err := LoadBlocks(baseDir)
	if err != nil {
		return nil, err
	}

	ctxCopy := ctx
	if ctxCopy.BaseDir == "" {
		ctxCopy.BaseDir = baseDir
	}

	activeBlocks, err := filterActiveBlocks(baseBlocks, ctxCopy)
	if err != nil {
		return nil, err
	}

	if agentName != "" {
		agentBlocks, err := LoadAgentBlocks(baseDir, agentName)
		if err != nil {
			return nil, err
		}
		activeAgentBlocks, err := filterActiveBlocks(agentBlocks, ctxCopy)
		if err != nil {
			return nil, err
		}
		activeBlocks = append(activeBlocks, activeAgentBlocks...)
	}

	resolved := &ResolvedHarness{
		Policy:   policy,
		Identity: identity,
		Tools:    make([]ToolDef, 0),
		Hooks:    make([]HookDef, 0),
	}

	for _, block := range activeBlocks {
		resolved.Tools = append(resolved.Tools, block.Tools...)
		resolved.Hooks = append(resolved.Hooks, block.Hooks...)
		if block.Context != "" {
			resolved.ContextBlocks = append(resolved.ContextBlocks, ContextBlock{
				Name:    block.Name,
				Content: block.Context,
				Source:  block.Source,
			})
		}
	}

	return resolved, nil
}

func filterActiveBlocks(blocks []Block, ctx ConditionContext) ([]Block, error) {
	active := make([]Block, 0, len(blocks))
	for _, block := range blocks {
		matches, err := EvaluateCondition(block.Condition, ctx)
		if err != nil {
			return nil, fmt.Errorf("evaluate block %q: %w", block.Name, err)
		}
		if matches {
			active = append(active, block)
		}
	}
	return active, nil
}
