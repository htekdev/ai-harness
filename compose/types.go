package compose

// Block represents a parsed composable markdown block.
// Everything is in one .md file: frontmatter has config (name, condition, tools, hooks),
// body is pure context (prose injected into the agent's knowledge).
type Block struct {
	Name        string
	Description string
	Condition   string // Starlark expression (empty = always active)
	Context     string // Markdown body (prose context for the agent)
	Tools       []ToolDef
	Hooks       []HookDef
	Source      string // File path this block was loaded from
}

// ToolDef is a tool definition from a composable block's frontmatter.
type ToolDef struct {
	Name        string              `yaml:"name"`
	Description string              `yaml:"description"`
	Parameters  map[string]ParamDef `yaml:"parameters"`
	TimeoutMS   int                 `yaml:"timeout_ms,omitempty"`
	Script      string              `yaml:"script,omitempty"`
}

// ParamDef defines a tool parameter.
type ParamDef struct {
	Type        string `yaml:"type"`
	Required    bool   `yaml:"required"`
	Description string `yaml:"description,omitempty"`
}

// HookDef is a hook definition from a composable block's frontmatter.
type HookDef struct {
	Event    string `yaml:"event"`
	Handler  string `yaml:"handler"`
	Priority int    `yaml:"priority,omitempty"`
	When     string `yaml:"when,omitempty"`
	Script   string `yaml:"script,omitempty"`
	Tool     string `yaml:"tool,omitempty"`
	Action   string `yaml:"action,omitempty"`
	Reason   string `yaml:"reason,omitempty"`
}

// Policy represents singleton parameters parsed from identity.md frontmatter.
// All config lives in .md frontmatter — there are NO separate .yaml files.
type Policy struct {
	Model      ModelPolicy      `yaml:"model"`
	Models     []ModelPolicy    `yaml:"models,omitempty"`
	Delegation DelegationPolicy `yaml:"delegation,omitempty"`
	Context    ContextPolicy    `yaml:"context,omitempty"`
	Meta       MetaPolicy       `yaml:"meta,omitempty"`
}

type ModelPolicy struct {
	Name        string  `yaml:"name"`
	Provider    string  `yaml:"provider"`
	MaxTokens   int     `yaml:"max_tokens"`
	Temperature float64 `yaml:"temperature"`
	APIKeyEnv   string  `yaml:"api_key_env"`
	BaseURL     string  `yaml:"base_url,omitempty"`
}

type DelegationPolicy struct {
	MaxDepth           int   `yaml:"max_depth,omitempty"`
	MaxConcurrent      int   `yaml:"max_concurrent,omitempty"`
	IterationsPerDepth []int `yaml:"iterations_per_depth,omitempty"`
}

type ContextPolicy struct {
	MaxHistory int                `yaml:"max_history"`
	MaxTokens  int                `yaml:"max_tokens"`
	Sources    []ContextSourceDef `yaml:"sources,omitempty"`
}

// ContextSourceDef is the serialisable declaration of a context source,
// configured in identity.md frontmatter under context.sources.
type ContextSourceDef struct {
	Name     string `yaml:"name"`
	Type     string `yaml:"type"`
	Path     string `yaml:"path"`
	When     string `yaml:"when,omitempty"`
	Trigger  string `yaml:"trigger,omitempty"`
	Priority int    `yaml:"priority,omitempty"`
	Scope    string `yaml:"scope,omitempty"`
	TTL      int    `yaml:"ttl,omitempty"`
}

type MetaPolicy struct {
	Enabled      bool `yaml:"enabled"`
	MaxTools     int  `yaml:"max_tools"`
	MaxHooks     int  `yaml:"max_hooks"`
	MaxAgents    int  `yaml:"max_agents"`
	MaxCallDepth int  `yaml:"max_call_depth"`
}

// ResolvedHarness is the output of composition — everything needed to run an agent.
type ResolvedHarness struct {
	Policy        Policy
	Identity      string
	Tools         []ToolDef
	Hooks         []HookDef
	ContextBlocks []ContextBlock
}

// ContextBlock represents injected context from an active block.
type ContextBlock struct {
	Name    string
	Content string
	Source  string
}

// ConditionContext provides data for condition evaluation.
type ConditionContext struct {
	Values  map[string]interface{}
	BaseDir string
}
