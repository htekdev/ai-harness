package evals

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/htekdev/ai-harness/agent"
	"github.com/htekdev/ai-harness/completion"
	agentctx "github.com/htekdev/ai-harness/context"
	"github.com/htekdev/ai-harness/delegation"
	"github.com/htekdev/ai-harness/harness/errs"
	"github.com/htekdev/ai-harness/hooks"
	"github.com/htekdev/ai-harness/scripting"
	"github.com/htekdev/ai-harness/tools"
)

// RunnerConfig configures the eval runner.
type RunnerConfig struct {
	// Model is the default model for all cases.
	Model string `yaml:"model"`
	// APIKeyEnv is the environment variable holding the API key.
	APIKeyEnv string `yaml:"api_key_env"`
	// Parallelism is max concurrent evals (default: 3).
	Parallelism int `yaml:"parallelism"`
	// BudgetCapUSD is max spend in USD before aborting (default: 0.50).
	BudgetCapUSD float64 `yaml:"budget_cap_usd"`
	// DefaultTimeout per case (default: 30s).
	DefaultTimeout time.Duration `yaml:"default_timeout"`
	// CasesDir is the path to eval case YAML files.
	CasesDir string `yaml:"cases_dir"`
	// RetryOnFail retries failed evals this many times (default: 1).
	RetryOnFail int `yaml:"retry_on_fail"`
	// BaseURL overrides the completion API base URL.
	BaseURL string `yaml:"base_url"`
}

// DefaultConfig returns the default runner configuration.
func DefaultConfig() RunnerConfig {
	return RunnerConfig{
		Model:          "gpt-4o-mini",
		APIKeyEnv:      "GH_TOKEN",
		Parallelism:    3,
		BudgetCapUSD:   0.50,
		DefaultTimeout: 30 * time.Second,
		CasesDir:       "evals/testdata",
		RetryOnFail:    1,
	}
}

// Runner executes eval cases against real LLM APIs.
type Runner struct {
	Config  RunnerConfig
	logger  *slog.Logger
	cost    CostTracker
	aborted atomic.Bool
}

// NewRunner creates an eval runner with the given config.
func NewRunner(cfg RunnerConfig) *Runner {
	if cfg.Model == "" {
		cfg.Model = "gpt-4o-mini"
	}
	if cfg.APIKeyEnv == "" {
		cfg.APIKeyEnv = "GH_TOKEN"
	}
	if cfg.Parallelism <= 0 {
		cfg.Parallelism = 3
	}
	if cfg.BudgetCapUSD <= 0 {
		cfg.BudgetCapUSD = 0.50
	}
	if cfg.DefaultTimeout <= 0 {
		cfg.DefaultTimeout = 30 * time.Second
	}
	if cfg.RetryOnFail < 0 {
		cfg.RetryOnFail = 0
	}

	return &Runner{
		Config: cfg,
		logger: slog.New(slog.NewTextHandler(os.Stderr, nil)).With("component", "evals"),
	}
}

// EvalResult is the outcome of running a single eval case.
type EvalResult struct {
	Case       *EvalCase
	Passed     bool
	Grades     []GradeResult
	Transcript *Transcript
	Duration   time.Duration
	Tokens     int
	Retries    int
	Error      string
}

// SuiteResult is the aggregate result of all eval cases.
type SuiteResult struct {
	Results    []EvalResult
	TotalCases int
	Passed     int
	Failed     int
	Tokens     int
	Duration   time.Duration
	Cost       float64
	Aborted    bool
}

// Run executes all eval cases from the configured directory.
func (r *Runner) Run(ctx context.Context) (*SuiteResult, error) {
	cases, err := LoadCases(r.Config.CasesDir)
	if err != nil {
		return nil, errs.Wrap(errs.KindConfig, "evals.Runner.Run", err, "load cases")
	}

	return r.RunCases(ctx, cases)
}

// RunCases executes the given eval cases.
func (r *Runner) RunCases(ctx context.Context, cases []*EvalCase) (*SuiteResult, error) {
	apiKey := os.Getenv(r.Config.APIKeyEnv)
	if apiKey == "" {
		return nil, errs.Newf(errs.KindConfig, "evals.Runner.RunCases", "environment variable %q is not set", r.Config.APIKeyEnv)
	}

	start := time.Now()
	results := make([]EvalResult, len(cases))
	sem := make(chan struct{}, r.Config.Parallelism)
	var wg sync.WaitGroup

	for i, c := range cases {
		if r.aborted.Load() {
			results[i] = EvalResult{Case: c, Error: "aborted: budget cap exceeded"}
			continue
		}

		wg.Add(1)
		sem <- struct{}{}
		go func(idx int, evalCase *EvalCase) {
			defer wg.Done()
			defer func() { <-sem }()

			result := r.runOneWithRetry(ctx, evalCase, apiKey)
			results[idx] = result

			// Check budget
			if r.cost.EstimatedUSD() >= r.Config.BudgetCapUSD {
				r.aborted.Store(true)
				r.logger.Error("budget cap reached, aborting remaining evals",
					"spent_usd", r.cost.EstimatedUSD(),
					"cap_usd", r.Config.BudgetCapUSD)
			}
		}(i, c)
	}

	wg.Wait()

	suite := &SuiteResult{
		Results:    results,
		TotalCases: len(cases),
		Duration:   time.Since(start),
		Tokens:     r.cost.TotalTokens(),
		Cost:       r.cost.EstimatedUSD(),
		Aborted:    r.aborted.Load(),
	}

	for _, res := range results {
		if res.Passed {
			suite.Passed++
		} else {
			suite.Failed++
		}
	}

	return suite, nil
}

func (r *Runner) runOneWithRetry(ctx context.Context, c *EvalCase, apiKey string) EvalResult {
	result := r.runOne(ctx, c, apiKey)
	if result.Passed || r.Config.RetryOnFail <= 0 {
		return result
	}

	// Retry once (safety net for API hiccups — NOT for prompting harder)
	for retry := 0; retry < r.Config.RetryOnFail; retry++ {
		r.logger.Info("retrying eval",
			"case", c.Name, "attempt", retry+2, "max_attempts", r.Config.RetryOnFail+1)
		result = r.runOne(ctx, c, apiKey)
		result.Retries = retry + 1
		if result.Passed {
			return result
		}
	}

	return result
}

func (r *Runner) runOne(ctx context.Context, c *EvalCase, apiKey string) EvalResult {
	start := time.Now()

	// Apply timeout
	timeout := c.Timeout
	if timeout == 0 {
		timeout = r.Config.DefaultTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// Build the harness for this eval case
	transcript, err := r.execute(ctx, c, apiKey)
	duration := time.Since(start)

	if err != nil {
		return EvalResult{
			Case:       c,
			Passed:     false,
			Transcript: transcript,
			Duration:   duration,
			Error:      err.Error(),
		}
	}

	transcript.TotalDuration = duration

	// Track tokens
	r.cost.Add(transcript.TotalTokens)

	// Grade
	grades := Grade(c.Grade, transcript)
	passed := AllPassed(grades)

	return EvalResult{
		Case:       c,
		Passed:     passed,
		Grades:     grades,
		Transcript: transcript,
		Duration:   duration,
		Tokens:     transcript.TotalTokens,
	}
}

func (r *Runner) execute(ctx context.Context, c *EvalCase, apiKey string) (*Transcript, error) {
	transcript := &Transcript{}

	// Determine model and base URL
	model := c.Model
	if model == "" {
		model = r.Config.Model
	}
	baseURL := r.Config.BaseURL
	if baseURL == "" {
		baseURL = "https://api.githubcopilot.com"
	}

	// Create completion client
	client := completion.NewClient(completion.ClientConfig{
		BaseURL:    baseURL,
		APIKey:     apiKey,
		Model:      model,
		MaxRetries: 2,
		Timeout:    30 * time.Second,
	})

	// Build tool registry
	registry := tools.NewRegistry()
	engine := scripting.NewEngine()

	// Track tool call counts for error_on_call simulation
	toolCallCounts := make(map[string]int)
	var toolMu sync.Mutex

	for _, toolCfg := range c.Setup.Tools {
		tc := toolCfg // capture
		def := tools.Definition{
			Name:        tc.Name,
			Description: tc.Description,
			Parameters:  convertParams(tc.Parameters),
		}

		var handler tools.Handler
		if tc.Script != "" {
			h, err := scripting.NewToolHandler(engine, tc.Name, tc.Script)
			if err != nil {
				return transcript, errs.Wrap(errs.KindTool, "evals.runCase.compileTool", err, "compile tool %q", tc.Name)
			}
			handler = h
		} else {
			handler = func(_ context.Context, _ json.RawMessage) (string, error) {
				return "ok", nil
			}
		}

		// Wrap for error simulation
		if tc.ErrorOnCall > 0 {
			origHandler := handler
			errorCall := tc.ErrorOnCall
			handler = func(hCtx context.Context, args json.RawMessage) (string, error) {
				toolMu.Lock()
				toolCallCounts[tc.Name]++
				count := toolCallCounts[tc.Name]
				toolMu.Unlock()
				if count <= errorCall {
					transcript.Errors = append(transcript.Errors, fmt.Sprintf("tool %q simulated error on call %d", tc.Name, count))
					return "", fmt.Errorf("simulated error on call %d", count)
				}
				return origHandler(hCtx, args)
			}
		}

		if err := registry.Register(def, handler); err != nil {
			return transcript, errs.Wrap(errs.KindTool, "evals.runCase.registerTool", err, "register tool %q", tc.Name)
		}
	}

	// Build hook system
	hookSystem := hooks.NewSystem()
	for _, hookCfg := range c.Setup.Hooks {
		hc := hookCfg
		var handler hooks.Handler
		if hc.Script != "" {
			h, err := scripting.NewConditionalHookHandler(engine, hc.Name, hc.When, hc.Script)
			if err != nil {
				return transcript, errs.Wrap(errs.KindConfig, "evals.runCase.compileHook", err, "compile hook %q", hc.Name)
			}
			handler = h
		} else {
			handler = func(_ context.Context, _ hooks.Event, _ any) hooks.Result {
				return hooks.Result{Action: hooks.ActionContinue}
			}
		}

		// Wrap to track hook events
		origHandler := handler
		handler = func(hCtx context.Context, evt hooks.Event, payload any) hooks.Result {
			result := origHandler(hCtx, evt, payload)
			actionStr := "continue"
			switch result.Action {
			case hooks.ActionBlock:
				actionStr = "block"
			case hooks.ActionModify:
				actionStr = "modify"
			}
			transcript.HookEvents = append(transcript.HookEvents, HookEvent{
				Name:   hc.Name,
				Event:  hc.Event,
				Action: actionStr,
				Reason: result.Reason,
			})
			return result
		}

		priority := hc.Priority
		if priority == 0 {
			priority = 100
		}
		hookSystem.Register(hooks.Registration{
			Name:     hc.Name,
			Event:    hooks.Event(hc.Event),
			Priority: priority,
			Handler:  handler,
		})
	}

	// Build context manager
	ctxMgr := agentctx.NewManager(agentctx.Config{
		SystemPrompt: c.Setup.SystemPrompt,
		MaxMessages:  50,
		MaxTokens:    128000,
	})

	// Set up delegation if configured
	if c.Setup.Delegation != nil {
		maxDepth := c.Setup.Delegation.MaxDepth
		if maxDepth == 0 {
			maxDepth = 2
		}
		itersPerDepth := c.Setup.Delegation.IterationsPerDepth
		if itersPerDepth == nil {
			itersPerDepth = []int{10, 5, 3}
		}

		delegator := delegation.NewDelegator(delegation.DelegatorConfig{
			Client:             client,
			Engine:             engine,
			HookSystem:         hookSystem,
			SystemPrompt:       c.Setup.SystemPrompt,
			Logger:             slog.Default().With("component", "eval-delegate"),
			MaxDepth:           maxDepth,
			IterationsPerDepth: itersPerDepth,
		})

		delegateDef := delegation.DelegateToolDefinition()
		delegateHandler := delegator.CreateDelegateToolHandler()

		// Wrap to track delegation events
		wrappedDelegate := func(dCtx context.Context, args json.RawMessage) (string, error) {
			result, err := delegateHandler(dCtx, args)
			var req struct {
				Task string `json:"task"`
			}
			_ = json.Unmarshal(args, &req)
			transcript.DelegationEvents = append(transcript.DelegationEvents, DelegationEvent{
				Task:   req.Task,
				Result: truncate(result, 200),
			})
			return result, err
		}

		if err := registry.Register(delegateDef, wrappedDelegate); err != nil {
			return transcript, errs.Wrap(errs.KindDelegation, "evals.runCase.registerDelegate", err, "register delegate")
		}
	}

	// Build agent
	maxTokens := c.MaxTokens
	if maxTokens == 0 {
		maxTokens = 500
	}

	ag := agent.New(agent.Options{
		Client:            client,
		Tools:             registry,
		Hooks:             hookSystem,
		Context:           ctxMgr,
		Logger:            slog.Default().With("component", "eval", "case", c.Name),
		MaxToolIterations: 10,
		StopDelegate: func(ctx context.Context, request any) (*agent.TurnResult, error) {
			if delegator == nil {
				return nil, fmt.Errorf("agent.stop requested delegation but no delegator is configured")
			}
			result, err := delegator.ExecuteControlFlow(ctx, request)
			if err != nil {
				return nil, err
			}
			return &agent.TurnResult{
				Response:    result.Response,
				ToolCalls:   result.ToolCalls,
				ToolResults: result.ToolResults,
			}, nil
		},
	})

	// Start session
	if err := ag.RunSession(ctx); err != nil {
		return transcript, errs.Wrap(errs.KindPersistence, "evals.runCase.startSession", err, "start session")
	}
	defer ag.EndSession(ctx)

	// Execute turns
	for _, turn := range c.Turns {
		if turn.Role != "user" {
			continue
		}

		turnStart := time.Now()
		result, err := ag.Run(ctx, turn.Content)
		turnDuration := time.Since(turnStart)

		if err != nil {
			transcript.Errors = append(transcript.Errors, err.Error())
			transcript.Turns = append(transcript.Turns, TranscriptTurn{
				UserMessage: turn.Content,
				Duration:    turnDuration,
			})
			continue
		}

		tt := TranscriptTurn{
			UserMessage: turn.Content,
			Response:    result.Response,
			ToolCalls:   result.ToolCalls,
			ToolResults: result.ToolResults,
			Tokens:      result.Usage.TotalTokens,
			Duration:    turnDuration,
		}
		transcript.Turns = append(transcript.Turns, tt)
		transcript.TotalTokens += result.Usage.TotalTokens
	}

	return transcript, nil
}

func convertParams(params map[string]CaseParam) []tools.Parameter {
	result := make([]tools.Parameter, 0, len(params))
	for name, p := range params {
		result = append(result, tools.Parameter{
			Name:        name,
			Type:        tools.ParameterType(p.Type),
			Description: p.Description,
			Required:    p.Required,
		})
	}
	return result
}
