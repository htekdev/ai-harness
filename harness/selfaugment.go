package harness

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/htekdev/ai-harness/config"
	"github.com/htekdev/ai-harness/hooks"
	"github.com/htekdev/ai-harness/scripting"
	"github.com/htekdev/ai-harness/tools"
)

// Phase 5.8 — Self-Augmenting Harness
//
// These built-in tools let the LLM author its own harness at runtime by
// writing artifact files to the on-disk `.harness/` directory and then
// hot-reloading them through Harness.Reload(). The artifacts persist
// across restarts (they are just .md files), so the augmentation
// compounds session over session.
//
// Tools registered:
//   - harness_create_tool      ⇒  .harness/tools/<name>.md
//   - harness_create_hook      ⇒  .harness/hooks/<name>.md
//   - harness_list_artifacts   (read-only introspection)
//   - harness_remove_artifact  (deletes a .md and unregisters)
//
// Naming uses underscores (not dots) so the tool names pass the
// OpenAI / GitHub Copilot tool-name regex [^a-zA-Z0-9_-]+.

// artifactNameRe restricts artifact names to a safe filesystem-and-
// tool-call subset: lowercase letters, digits, underscore, hyphen.
// 1–64 chars. This avoids path traversal (no slashes, no dots) and
// keeps file names predictable.
var artifactNameRe = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,63}$`)

// builtinToolNames lists the meta-tools this file installs. Reload()
// must never touch them — they are owned by the binary, not the
// `.harness/` directory.
var builtinToolNames = []string{
	"harness_create_tool",
	"harness_create_hook",
	"harness_create_context",
	"harness_list_artifacts",
	"harness_remove_artifact",
}

// registerSelfAugmentTools installs the five self-augmenting meta-tools
// onto the harness's tool registry. Called once during NewFromConfig.
func registerSelfAugmentTools(h *Harness) error {
	defs := []struct {
		def     tools.Definition
		handler tools.Handler
	}{
		{createToolDefinition(), h.handleCreateTool},
		{createHookDefinition(), h.handleCreateHook},
		{createContextDefinition(), h.handleCreateContext},
		{listArtifactsDefinition(), h.handleListArtifacts},
		{removeArtifactDefinition(), h.handleRemoveArtifact},
	}
	for _, e := range defs {
		if err := h.registry.Register(e.def, e.handler); err != nil {
			return fmt.Errorf("register %s: %w", e.def.Name, err)
		}
	}
	return nil
}

// ---------- tool definitions ----------

func createToolDefinition() tools.Definition {
	return tools.Definition{
		Name: "harness_create_tool",
		Description: "Create a new tool artifact in `.harness/tools/<name>.md` and hot-reload it. " +
			"The new tool becomes callable in the very next agent step. Use this when the user asks " +
			"for a capability the current toolset cannot satisfy. " +
			"**Scripts are Starlark (a Python-subset), NOT Python.** No `with`, no f-strings, no list " +
			"comprehensions with `if`. Use the Harness Starlark stdlib: `fs.read(path)`, " +
			"`fs.write(path, content)`, `fs.append(path, content)`, `fs.exists(path)`, " +
			"`http.get(url)`, `http.post(url, body)`, `json.encode(v)`, `json.decode(s)`, `time.now()`, " +
			"`log(msg)`, `env(key)`, `re.match`, `re.find_all`, `re.replace`, `hash.sha256`, `base64.encode`. " +
			"Tools must define `def run(args)` returning a string. Tool definitions persist on disk " +
			"across restarts. The script is compile-checked before the file is written, so syntax " +
			"errors are returned to you immediately and the bad artifact never lands on disk.",
		Parameters: []tools.Parameter{
			{Name: "name", Type: tools.TypeString, Required: true,
				Description: "Tool name. Lowercase, digits, underscore, hyphen only (1-64 chars). Becomes both the file name and the tool's invocation name."},
			{Name: "description", Type: tools.TypeString, Required: true,
				Description: "Human-readable description of what the tool does. Surfaced to the LLM in tool listings — write it like prompt engineering, not an internal comment."},
			{Name: "parameters_json", Type: tools.TypeString, Required: false,
				Description: "JSON object mapping parameter name to {type, description, required}. Example: {\"city\":{\"type\":\"string\",\"description\":\"City name\",\"required\":true}}. Omit or pass \"{}\" for a no-arg tool."},
			{Name: "script", Type: tools.TypeString, Required: true,
				Description: "Starlark source code. Must define `def run(args): ...` returning a string. `args` is the parsed parameter dict."},
		},
	}
}

func createHookDefinition() tools.Definition {
	return tools.Definition{
		Name: "harness_create_hook",
		Description: "Create a new hook artifact in `.harness/hooks/<name>.md` and hot-reload it. " +
			"Hooks fire on lifecycle events (tool.pre, tool.post, completion.pre, completion.post, " +
			"turn.start, turn.end, session.start, session.end, delegation.pre, delegation.post, error). " +
			"Use them for guardrails (block tool calls, redact arguments) or for observability " +
			"(log every completion). Hook scripts receive an `event` payload via the meta context.",
		Parameters: []tools.Parameter{
			{Name: "name", Type: tools.TypeString, Required: true,
				Description: "Hook handler name. Lowercase, digits, underscore, hyphen only (1-64 chars)."},
			{Name: "event", Type: tools.TypeString, Required: true,
				Description: "Lifecycle event to fire on. One of: session.start, session.end, turn.start, turn.end, tool.pre, tool.post, completion.pre, completion.post, delegation.pre, delegation.post, error."},
			{Name: "script", Type: tools.TypeString, Required: true,
				Description: "Starlark source code. Must define `def handle(event): ...` and return one of allow(), block(reason), or modify(...)."},
			{Name: "when", Type: tools.TypeString, Required: false,
				Description: "Optional Starlark expression that evaluates to bool — the hook is skipped when it returns false. Lets you scope a hook narrowly (e.g. `event[\"name\"] == \"http_get\"`)."},
			{Name: "priority", Type: tools.TypeNumber, Required: false,
				Description: "Hook priority (lower runs first). Default 100."},
		},
	}
}

func listArtifactsDefinition() tools.Definition {
	return tools.Definition{
		Name: "harness_list_artifacts",
		Description: "List every artifact this harness currently has registered: built-in tools, " +
			"file-based tools (from `.harness/tools/`), file-based hooks (from `.harness/hooks/`), and " +
			"named agents. Use this whenever the user asks `what can you do?` or before deciding " +
			"whether to create a new tool — the capability may already exist.",
		Parameters: []tools.Parameter{
			{Name: "kind", Type: tools.TypeString, Required: false,
				Description: "Optional filter: \"tools\", \"hooks\", \"agents\", or \"all\" (default)."},
		},
	}
}

func removeArtifactDefinition() tools.Definition {
	return tools.Definition{
		Name: "harness_remove_artifact",
		Description: "Delete an artifact file from `.harness/` and unregister it from the running " +
			"harness on the next turn. Refuses to remove built-in tools (delegate, harness_*). Use " +
			"carefully — this is destructive and persists across restarts because the underlying " +
			"file is deleted.",
		Parameters: []tools.Parameter{
			{Name: "kind", Type: tools.TypeString, Required: true,
				Description: "Artifact category: \"tool\", \"hook\", \"context\", or \"skill\"."},
			{Name: "name", Type: tools.TypeString, Required: true,
				Description: "Artifact name (matches the file basename, no extension)."},
		},
	}
}

// createContextDefinition exposes harness_create_context — the prose
// counterpart to create_tool / create_hook. Writes a plain `.md` file
// to `.harness/context/<name>.md`; its body is injected into the live
// system prompt by buildSystemPrompt on the next turn. Use for stable
// facts the model should remember about the user across turns
// (preferences, naming, project context, etc.).
func createContextDefinition() tools.Definition {
	return tools.Definition{
		Name: "harness_create_context",
		Description: "Persist a stable fact about the user, the project, or the environment so " +
			"future turns automatically know it. Writes `.harness/context/<name>.md`; the body is " +
			"merged into the system prompt at the start of every subsequent turn. Use for things " +
			"like \"user prefers metric units\", \"project uses TypeScript strict mode\", or any " +
			"long-lived preference. Do NOT use for transient state — use the conversation for that. " +
			"There is no compile-check (this is prose, not code), and the file persists across " +
			"restarts.",
		Parameters: []tools.Parameter{
			{Name: "name", Type: tools.TypeString, Required: true,
				Description: "Context file name. Lowercase, digits, underscore, hyphen only (1-64 chars). Becomes the section heading in the system prompt."},
			{Name: "body", Type: tools.TypeString, Required: true,
				Description: "Markdown body to inject into the system prompt. Keep it concise — every byte stays in context on every turn."},
		},
	}
}

// ---------- handler implementations ----------

type createToolArgs struct {
	Name           string `json:"name"`
	Description    string `json:"description"`
	ParametersJSON string `json:"parameters_json,omitempty"`
	Script         string `json:"script"`
}

func (h *Harness) handleCreateTool(_ context.Context, raw json.RawMessage) (string, error) {
	var args createToolArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		return "", fmt.Errorf("invalid arguments: %w", err)
	}
	if !artifactNameRe.MatchString(args.Name) {
		return "", fmt.Errorf("invalid tool name %q: must match ^[a-z0-9][a-z0-9_-]{0,63}$", args.Name)
	}
	if isBuiltinToolName(args.Name) {
		return "", fmt.Errorf("tool name %q is reserved (built-in)", args.Name)
	}
	if strings.TrimSpace(args.Description) == "" {
		return "", fmt.Errorf("description must be non-empty")
	}
	if !strings.Contains(args.Script, "def run") {
		return "", fmt.Errorf("script must define a `def run(args)` entrypoint")
	}

	// Compile-check the Starlark script BEFORE writing the artifact to
	// disk. Without this, syntactically broken scripts (e.g. Python
	// idioms like `with open(...)` or f-strings) would land in
	// .harness/tools/, then crash the harness with a fatal load error
	// on every subsequent startup until manually deleted. The model
	// gets a precise compile error instead, which it can fix and retry.
	if _, cerr := scripting.NewToolHandler(h.engine, args.Name, args.Script); cerr != nil {
		return "", fmt.Errorf("script does not compile (remember: this is Starlark, not Python — no `with`, no f-strings; use fs.read / fs.write / http.get / json.encode etc.): %w", cerr)
	}

	// Validate parameters_json if provided.
	params := map[string]config.ParamConfig{}
	if strings.TrimSpace(args.ParametersJSON) != "" && args.ParametersJSON != "{}" {
		if err := json.Unmarshal([]byte(args.ParametersJSON), &params); err != nil {
			return "", fmt.Errorf("parameters_json: %w", err)
		}
	}

	// Render the artifact .md file.
	body, err := renderToolMarkdown(args.Name, args.Description, params, args.Script)
	if err != nil {
		return "", fmt.Errorf("render artifact: %w", err)
	}

	dir := filepath.Join(h.baseDir, ".harness", "tools")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("mkdir %s: %w", dir, err)
	}
	path := filepath.Join(dir, args.Name+".md")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		return "", fmt.Errorf("write %s: %w", path, err)
	}

	// Hot-reload is no longer triggered here. Per-turn artifact discovery
	// (Harness.onTurnStart, invoked by agent.Run before every turn)
	// reconciles the registry against on-disk state automatically. The
	// new tool will be callable on the next agent turn — write-and-go.

	resp := map[string]any{
		"status":      "ok",
		"created":     args.Name,
		"path":        relPath(h.baseDir, path),
		"description": strings.TrimSpace(args.Description),
		"next_turn":   true,
		"hint":        fmt.Sprintf("Tool %q has been written to disk and will be loaded at the start of the next turn. If you tried to call it within the SAME turn it would fail — the registry only refreshes between turns.", args.Name),
	}
	out, _ := json.MarshalIndent(resp, "", "  ")
	return string(out), nil
}

type createHookArgs struct {
	Name     string `json:"name"`
	Event    string `json:"event"`
	Script   string `json:"script"`
	When     string `json:"when,omitempty"`
	Priority int    `json:"priority,omitempty"`
}

func (h *Harness) handleCreateHook(_ context.Context, raw json.RawMessage) (string, error) {
	var args createHookArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		return "", fmt.Errorf("invalid arguments: %w", err)
	}
	if !artifactNameRe.MatchString(args.Name) {
		return "", fmt.Errorf("invalid hook name %q: must match ^[a-z0-9][a-z0-9_-]{0,63}$", args.Name)
	}
	if !hooks.IsValidEvent(args.Event) {
		return "", fmt.Errorf("invalid event %q (use session.start | session.end | turn.start | turn.end | tool.pre | tool.post | completion.pre | completion.post | delegation.pre | delegation.post | error)", args.Event)
	}
	if !strings.Contains(args.Script, "def handle") {
		return "", fmt.Errorf("script must define a `def handle(event)` entrypoint")
	}

	// Compile-check the Starlark hook script BEFORE writing the artifact
	// to disk. Same rationale as create_tool: a broken hook would brick
	// every subsequent harness load until manually removed.
	if _, cerr := scripting.NewConditionalHookHandler(h.engine, args.Name, args.When, args.Script); cerr != nil {
		return "", fmt.Errorf("hook script does not compile (Starlark, not Python — use allow()/block(reason)/modify(...)): %w", cerr)
	}

	body := renderHookMarkdown(args.Name, args.Event, args.When, args.Priority, args.Script)

	dir := filepath.Join(h.baseDir, ".harness", "hooks")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("mkdir %s: %w", dir, err)
	}
	path := filepath.Join(dir, args.Name+".md")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		return "", fmt.Errorf("write %s: %w", path, err)
	}

	// No Reload() call — per-turn discovery picks this hook up on the
	// next agent turn. Write-and-go.
	resp := map[string]any{
		"status":    "ok",
		"created":   args.Name,
		"event":     args.Event,
		"path":      relPath(h.baseDir, path),
		"next_turn": true,
		"hint":      fmt.Sprintf("Hook %q on event %q has been written to disk and will be active on the next agent turn.", args.Name, args.Event),
	}
	out, _ := json.MarshalIndent(resp, "", "  ")
	return string(out), nil
}

type listArtifactsArgs struct {
	Kind string `json:"kind,omitempty"`
}

func (h *Harness) handleListArtifacts(_ context.Context, raw json.RawMessage) (string, error) {
	var args listArtifactsArgs
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &args)
	}
	kind := strings.ToLower(strings.TrimSpace(args.Kind))
	if kind == "" {
		kind = "all"
	}

	resp := map[string]any{}

	if kind == "all" || kind == "tools" {
		var built []map[string]string
		var fileBased []map[string]string
		for _, def := range h.registry.List() {
			row := map[string]string{
				"name":        def.Name,
				"description": truncate(def.Description, 200),
			}
			if isBuiltinToolName(def.Name) || def.Name == "delegate" || def.Name == "delegate_async" || def.Name == "delegate_status" || def.Name == "delegate_result" {
				built = append(built, row)
			} else if _, fromFile := h.fileTools[def.Name]; fromFile {
				fileBased = append(fileBased, row)
			} else {
				// Inline-config tools (defined directly in harness.md frontmatter).
				row["source"] = "inline"
				built = append(built, row)
			}
		}
		sort.Slice(built, func(i, j int) bool { return built[i]["name"] < built[j]["name"] })
		sort.Slice(fileBased, func(i, j int) bool { return fileBased[i]["name"] < fileBased[j]["name"] })
		resp["tools"] = map[string]any{
			"builtin":    built,
			"file_based": fileBased,
			"total":      h.registry.Count(),
		}
	}

	if kind == "all" || kind == "hooks" {
		var hookRows []map[string]string
		for name := range h.fileHooks {
			hookRows = append(hookRows, map[string]string{"name": name, "source": "file"})
		}
		sort.Slice(hookRows, func(i, j int) bool { return hookRows[i]["name"] < hookRows[j]["name"] })
		resp["hooks"] = hookRows
	}

	if kind == "all" || kind == "context" || kind == "contexts" {
		arts, _ := loadProseArtifacts(filepath.Join(h.baseDir, ".harness", "context"))
		var rows []map[string]string
		for _, a := range arts {
			rows = append(rows, map[string]string{
				"name":    a.Name,
				"preview": truncate(a.Body, 200),
			})
		}
		resp["context"] = rows
	}

	if kind == "all" || kind == "skill" || kind == "skills" {
		arts, _ := loadProseArtifacts(filepath.Join(h.baseDir, ".harness", "skills"))
		var rows []map[string]string
		for _, a := range arts {
			rows = append(rows, map[string]string{
				"name":    a.Name,
				"preview": truncate(a.Body, 200),
			})
		}
		resp["skills"] = rows
	}

	if kind == "all" || kind == "agents" {
		var agentRows []map[string]string
		for name, ac := range h.agents {
			row := map[string]string{"name": name}
			if ac != nil {
				row["description"] = truncate(ac.Description, 200)
			}
			agentRows = append(agentRows, row)
		}
		sort.Slice(agentRows, func(i, j int) bool { return agentRows[i]["name"] < agentRows[j]["name"] })
		resp["agents"] = agentRows
	}

	resp["base_dir"] = h.baseDir
	out, _ := json.MarshalIndent(resp, "", "  ")
	return string(out), nil
}

type createContextArgs struct {
	Name string `json:"name"`
	Body string `json:"body"`
}

func (h *Harness) handleCreateContext(_ context.Context, raw json.RawMessage) (string, error) {
	var args createContextArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		return "", fmt.Errorf("invalid arguments: %w", err)
	}
	if !artifactNameRe.MatchString(args.Name) {
		return "", fmt.Errorf("invalid context name %q: must match ^[a-z0-9][a-z0-9_-]{0,63}$", args.Name)
	}
	if strings.TrimSpace(args.Body) == "" {
		return "", fmt.Errorf("body must be non-empty")
	}

	dir := filepath.Join(h.baseDir, ".harness", "context")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("mkdir %s: %w", dir, err)
	}
	path := filepath.Join(dir, args.Name+".md")
	contents := "# " + args.Name + "\n\n" + strings.TrimSpace(args.Body) + "\n\n" +
		"<!-- Authored by harness_create_context at " + time.Now().UTC().Format(time.RFC3339) + " -->\n"
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		return "", fmt.Errorf("write %s: %w", path, err)
	}

	// No reload — per-turn discovery picks this up on the next turn,
	// at which point the body is automatically merged into the live
	// system prompt by buildSystemPrompt.
	resp := map[string]any{
		"status":    "ok",
		"created":   args.Name,
		"path":      relPath(h.baseDir, path),
		"next_turn": true,
		"hint":      "This context is now persisted to disk. On the next agent turn (and forever after), it will be injected into the system prompt under `## Active Context Artifacts`.",
	}
	out, _ := json.MarshalIndent(resp, "", "  ")
	return string(out), nil
}

type removeArtifactArgs struct {
	Kind string `json:"kind"`
	Name string `json:"name"`
}

func (h *Harness) handleRemoveArtifact(_ context.Context, raw json.RawMessage) (string, error) {
	var args removeArtifactArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		return "", fmt.Errorf("invalid arguments: %w", err)
	}
	if !artifactNameRe.MatchString(args.Name) {
		return "", fmt.Errorf("invalid name %q", args.Name)
	}
	if isBuiltinToolName(args.Name) || args.Name == "delegate" {
		return "", fmt.Errorf("cannot remove built-in artifact %q", args.Name)
	}

	var subdir string
	switch strings.ToLower(strings.TrimSpace(args.Kind)) {
	case "tool", "tools":
		subdir = "tools"
	case "hook", "hooks":
		subdir = "hooks"
	case "context", "contexts":
		subdir = "context"
	case "skill", "skills":
		subdir = "skills"
	default:
		return "", fmt.Errorf("kind must be \"tool\", \"hook\", \"context\", or \"skill\"; got %q", args.Kind)
	}

	path := filepath.Join(h.baseDir, ".harness", subdir, args.Name+".md")
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return "", fmt.Errorf("no such artifact: %s", relPath(h.baseDir, path))
	} else if err != nil {
		return "", fmt.Errorf("stat %s: %w", path, err)
	}
	if err := os.Remove(path); err != nil {
		return "", fmt.Errorf("remove %s: %w", path, err)
	}

	// No Reload() call — per-turn discovery will drop the unregistered
	// artifact at the start of the next turn.
	resp := map[string]any{
		"status":    "ok",
		"removed":   args.Name,
		"kind":      subdir,
		"path":      relPath(h.baseDir, path),
		"next_turn": true,
	}
	out, _ := json.MarshalIndent(resp, "", "  ")
	return string(out), nil
}

// ---------- per-turn artifact discovery ----------

// onTurnStart is wired into agent.Options.OnTurnStart so that every
// turn begins with a fresh reconciliation of `.harness/` artifacts.
// This is the heart of the "agent-as-code" model: there is no concept
// of explicit reload — files on disk are the source of truth, and the
// runtime catches up automatically before each turn. Scan failures
// fail the turn so the user gets a clear error rather than a silently
// stale registry.
func (h *Harness) onTurnStart(ctx context.Context) error {
	return h.scanAndApply(ctx)
}

// Reload performs a one-shot reconciliation of disk → runtime state.
// Kept as a public method for embedders that drive the harness outside
// the agent loop, but normal `harness serve` usage no longer needs to
// call this — onTurnStart handles it per turn.
func (h *Harness) Reload() error {
	return h.scanAndApply(context.Background())
}

// scanAndApply is the single source of truth for translating
// `.harness/` directory state into the live runtime. It:
//
//  1. Scans .harness/tools/*.md and .harness/hooks/*.md; replaces or
//     registers each entry; unregisters anything that used to be on
//     disk but no longer is.
//  2. Scans .harness/context/*.md and .harness/skills/*.md (free-form
//     prose artifacts that augment the system prompt).
//  3. Rebuilds the live system prompt from h.originalSystemPrompt +
//     the self-augment suffix + each context artifact body + each
//     skill artifact body, in stable filename order, and pushes it to
//     the context manager.
//
// Mutex-serialized so concurrent meta-tool calls and the per-turn
// callback cannot race.
func (h *Harness) scanAndApply(_ context.Context) error {
	h.reloadMu.Lock()
	defer h.reloadMu.Unlock()

	dirResult, err := config.LoadDirectory(h.baseDir)
	if err != nil {
		return fmt.Errorf("scan .harness: %w", err)
	}
	if dirResult != nil && dirResult.Config != nil {
		if rerr := h.reconcileToolsHooks(dirResult); rerr != nil {
			return rerr
		}
	}

	// Prose artifacts that augment the system prompt.
	contextArts, err := loadProseArtifacts(filepath.Join(h.baseDir, ".harness", "context"))
	if err != nil {
		return fmt.Errorf("scan .harness/context: %w", err)
	}
	skillArts, err := loadProseArtifacts(filepath.Join(h.baseDir, ".harness", "skills"))
	if err != nil {
		return fmt.Errorf("scan .harness/skills: %w", err)
	}

	h.ctxMgr.SetSystemPrompt(buildSystemPrompt(h.originalSystemPrompt, contextArts, skillArts))
	return nil
}

// reconcileToolsHooks performs the diff-against-disk update for tools
// and hooks. Extracted from the old Reload() body so scanAndApply can
// call it without duplicating the logic.
func (h *Harness) reconcileToolsHooks(dirResult *config.LoadResult) error {
	// --- Tools ---
	newToolNames := make(map[string]struct{}, len(dirResult.Config.Tools))
	for _, tc := range dirResult.Config.Tools {
		// Refuse to clobber built-ins by accident — a user could create
		// a `.harness/tools/delegate.md` and break the harness.
		if isBuiltinToolName(tc.Name) || tc.Name == "delegate" ||
			tc.Name == "delegate_async" || tc.Name == "delegate_status" ||
			tc.Name == "delegate_result" {
			Logger().Warn("ignoring file-based tool with reserved name", "name", tc.Name)
			continue
		}
		newToolNames[tc.Name] = struct{}{}

		def := definitionFromConfig(tc)
		var handler tools.Handler
		if tc.Script != "" {
			th, herr := scripting.NewToolHandler(h.engine, tc.Name, tc.Script)
			if herr != nil {
				return fmt.Errorf("compile reloaded tool %q: %w", tc.Name, herr)
			}
			handler = th
		} else {
			handler = unimplementedToolHandler(tc.Name)
		}
		if tc.TimeoutMS > 0 {
			handler = tools.WithTimeout(handler, time.Duration(tc.TimeoutMS)*time.Millisecond)
		}
		if err := h.registry.Replace(def, handler); err != nil {
			return fmt.Errorf("replace tool %q: %w", tc.Name, err)
		}
	}
	// Drop tools that used to be on disk but no longer are.
	for name := range h.fileTools {
		if _, stillThere := newToolNames[name]; !stillThere {
			h.registry.Unregister(name)
		}
	}
	h.fileTools = newToolNames

	// --- Hooks ---
	newHookNames := make(map[string]struct{}, len(dirResult.Config.Hooks))
	for _, hc := range dirResult.Config.Hooks {
		newHookNames[hc.Handler] = struct{}{}

		var handler hooks.Handler
		if hc.Script != "" {
			hh, herr := scripting.NewConditionalHookHandler(h.engine, hc.Handler, hc.When, hc.Script)
			if herr != nil {
				return fmt.Errorf("compile reloaded hook %q: %w", hc.Handler, herr)
			}
			handler = hh
		} else {
			handler = unimplementedHookHandler(hc.Handler)
		}
		priority := hc.Priority
		if priority == 0 {
			priority = 100
		}
		// Unregister-then-Register so we don't get duplicate handlers.
		h.hookSystem.Unregister(hc.Handler, hooks.Event(hc.Event))
		h.hookSystem.Register(hooks.Registration{
			Name:     hc.Handler,
			Event:    hooks.Event(hc.Event),
			Priority: priority,
			Handler:  handler,
		})
	}
	// Drop hooks that used to be on disk but no longer are. We don't
	// know the original event for unregister, so iterate every event.
	for name := range h.fileHooks {
		if _, stillThere := newHookNames[name]; !stillThere {
			for _, ev := range allHookEvents {
				h.hookSystem.Unregister(name, ev)
			}
		}
	}
	h.fileHooks = newHookNames

	return nil
}

// proseArtifact is a free-form `.md` file under `.harness/context/` or
// `.harness/skills/` whose body is injected into the live system
// prompt. Frontmatter (if any) is stripped — only the markdown body is
// surfaced to the model.
type proseArtifact struct {
	Name string
	Body string
}

// loadProseArtifacts reads every `.md` file in dir, strips optional
// frontmatter via config.ParseMarkdown, and returns the bodies in
// stable filename order. Missing dir is not an error (returns nil).
func loadProseArtifacts(dir string) ([]proseArtifact, error) {
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []proseArtifact
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		data, rerr := os.ReadFile(filepath.Join(dir, e.Name()))
		if rerr != nil {
			return nil, fmt.Errorf("read %s: %w", e.Name(), rerr)
		}
		body := string(data)
		if doc, perr := config.ParseMarkdown(data); perr == nil && doc != nil {
			if strings.TrimSpace(doc.Body) != "" {
				body = doc.Body
			}
		}
		out = append(out, proseArtifact{
			Name: strings.TrimSuffix(e.Name(), ".md"),
			Body: strings.TrimSpace(body),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// buildSystemPrompt composes the live system prompt for the next turn.
// Stable, idempotent: the same inputs always produce the same output,
// so a per-turn rescan that finds no changes produces no churn in the
// model's context.
func buildSystemPrompt(base string, contextArts, skillArts []proseArtifact) string {
	var sb strings.Builder
	sb.WriteString(strings.TrimRight(base, "\n"))
	sb.WriteString(selfAugmentPromptSuffix)

	if len(contextArts) > 0 {
		sb.WriteString("\n\n## Active Context Artifacts\n\n")
		sb.WriteString("These are user-authored notes loaded from `.harness/context/`. Add, edit, or remove files there to update what you remember about the user.\n")
		for _, a := range contextArts {
			sb.WriteString("\n### ")
			sb.WriteString(a.Name)
			sb.WriteString("\n\n")
			sb.WriteString(a.Body)
			sb.WriteString("\n")
		}
	}

	if len(skillArts) > 0 {
		sb.WriteString("\n\n## Active Skills\n\n")
		sb.WriteString("These are procedures loaded from `.harness/skills/`. Follow them when the situation matches.\n")
		for _, a := range skillArts {
			sb.WriteString("\n### ")
			sb.WriteString(a.Name)
			sb.WriteString("\n\n")
			sb.WriteString(a.Body)
			sb.WriteString("\n")
		}
	}

	return sb.String()
}

// allHookEvents enumerates every event Hook.Unregister might need to clear
// during scanAndApply. Kept in sync with hooks.IsValidEvent.
var allHookEvents = []hooks.Event{
	hooks.EventSessionStart,
	hooks.EventSessionEnd,
	hooks.EventTurnStart,
	hooks.EventTurnEnd,
	hooks.EventToolPre,
	hooks.EventToolPost,
	hooks.EventCompletionPre,
	hooks.EventCompletionPost,
	hooks.EventDelegatePre,
	hooks.EventDelegatePost,
	hooks.EventError,
}

// ---------- system prompt augmentation ----------

const selfAugmentPromptSuffix = `

## Self-Augmenting Harness — Agent-as-Code

You are running on AI Harness, a Go agent runtime built on the
**agent-as-code** principle: the ` + "`.harness/`" + ` directory on disk is the
source of truth, and the runtime reconciles against it at the start of
EVERY turn. There is no concept of "reloading" — adding, editing, or
removing any file in ` + "`.harness/`" + ` takes effect on the very next turn,
automatically.

You have FIVE built-in meta-tools for authoring your own harness:

- **harness_list_artifacts** — what tools, hooks, context, and skills
  are loaded right now. Call this when the user asks "what can you do?"
  or before deciding whether a capability already exists.
- **harness_create_tool** — write ` + "`.harness/tools/<name>.md`" + ` (Starlark
  script + parameters + description). Callable on the **next** turn.
- **harness_create_hook** — write ` + "`.harness/hooks/<name>.md`" + ` (Starlark
  ` + "`def handle(event)`" + ` on a lifecycle event). Active on the **next** turn.
- **harness_create_context** — write ` + "`.harness/context/<name>.md`" + ` (plain
  markdown body). Merged into the system prompt on the **next** turn
  and on every turn forever after. Use for stable preferences the user
  wants you to remember.
- **harness_remove_artifact** — delete a ` + "`.md`" + ` you wrote earlier. Gone
  on the next turn. Refuses to touch built-ins.

When the user asks for a capability you don't have, prefer **creating
an artifact** over saying "I can't do that". Artifacts persist on disk
and survive restarts.

### IMPORTANT: artifacts written this turn are NOT callable this turn

The registry refreshes BETWEEN turns. If you call ` + "`harness_create_tool`" + `
in turn N, the new tool is callable in turn N+1, not in turn N. So:

- ✅ Good: "Sure, I've created ` + "`weather_now`" + ` — ask me again and I'll use it."
- ❌ Bad: Create the tool then try to call it immediately. It will not
  be in the registry yet.

### CRITICAL — Scripts are Starlark, NOT Python

Starlark is a Python-subset. These Python idioms DO NOT WORK and will
fail compile-check (the harness rejects the tool before writing it):

- ❌ ` + "`with open(path) as f: ...`" + ` — no ` + "`with`" + ` statement
- ❌ ` + "`f\"hello {name}\"`" + ` — no f-strings; use ` + "`\"hello \" + name`" + `
- ❌ ` + "`[x for x in items if cond]`" + ` — no ` + "`if`" + ` in comprehensions
- ❌ ` + "`try: ... except: ...`" + ` — no exceptions; check return values
- ❌ ` + "`import os`" + ` — no imports; built-ins are pre-bound

Use the Harness Starlark stdlib instead:

| Need | Use |
|---|---|
| Read a file | ` + "`fs.read(path)`" + ` |
| Write a file | ` + "`fs.write(path, content)`" + ` |
| Append to a file | ` + "`fs.append(path, content)`" + ` |
| Check existence | ` + "`fs.exists(path)`" + ` |
| List directory | ` + "`fs.list(path)`" + ` |
| HTTP GET | ` + "`http.get(url, headers=..., timeout_seconds=...)`" + ` |
| HTTP POST | ` + "`http.post(url, body=..., headers=...)`" + ` |
| JSON encode | ` + "`json.encode(value)`" + ` |
| JSON decode | ` + "`json.decode(string)`" + ` |
| Current time | ` + "`time.now()`" + ` |
| Log a message | ` + "`log(msg)`" + ` |
| Env var | ` + "`env(\"KEY\")`" + ` |
| Regex match | ` + "`re.match(pattern, text)`" + ` |
| String concat | ` + "`a + b`" + ` (not f-string) |

Example correct tool:

` + "```" + `
def run(args):
    content = fs.read(args["path"])
    return "file has " + str(len(content)) + " bytes"
` + "```" + `

Keep created tools small, focused, and well-described. The description
field is what the model (you, on a future turn) will see when deciding
whether to call the tool — write it like prompt engineering.
`

// augmentSystemPromptForSelfAugment appends the self-augmentation note
// to a system prompt unless it's already present (idempotent). Returns
// the original string unchanged if it already mentions the meta-tools.
func augmentSystemPromptForSelfAugment(original string) string {
	if strings.Contains(original, "harness_create_tool") {
		return original
	}
	return strings.TrimRight(original, "\n") + selfAugmentPromptSuffix
}

// ---------- helpers ----------

func isBuiltinToolName(name string) bool {
	for _, b := range builtinToolNames {
		if b == name {
			return true
		}
	}
	return false
}

func relPath(base, full string) string {
	if rp, err := filepath.Rel(base, full); err == nil {
		return filepath.ToSlash(rp)
	}
	return filepath.ToSlash(full)
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "…"
}

// renderToolMarkdown produces a `.harness/tools/<name>.md` artifact
// matching the format ParseToolMarkdown expects: YAML frontmatter
// (parameters + script) followed by a markdown body (description).
func renderToolMarkdown(name, description string, params map[string]config.ParamConfig, script string) (string, error) {
	var sb strings.Builder
	sb.WriteString("---\n")
	if len(params) > 0 {
		sb.WriteString("parameters:\n")
		// Stable order so the file is reproducible.
		keys := make([]string, 0, len(params))
		for k := range params {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			p := params[k]
			sb.WriteString(fmt.Sprintf("  %s:\n", yamlKey(k)))
			if p.Type != "" {
				sb.WriteString(fmt.Sprintf("    type: %s\n", yamlString(p.Type)))
			}
			if p.Description != "" {
				sb.WriteString(fmt.Sprintf("    description: %s\n", yamlString(p.Description)))
			}
			if p.Required {
				sb.WriteString("    required: true\n")
			}
		}
	}
	sb.WriteString("script: |\n")
	for _, line := range strings.Split(strings.ReplaceAll(script, "\r\n", "\n"), "\n") {
		sb.WriteString("  ")
		sb.WriteString(line)
		sb.WriteString("\n")
	}
	sb.WriteString("---\n\n")
	sb.WriteString("# ")
	sb.WriteString(name)
	sb.WriteString("\n\n")
	sb.WriteString(strings.TrimSpace(description))
	sb.WriteString("\n\n")
	sb.WriteString("<!-- Authored by harness_create_tool at ")
	sb.WriteString(time.Now().UTC().Format(time.RFC3339))
	sb.WriteString(" -->\n")

	// Sanity check: the rendered output must round-trip through the
	// existing parser. Catch malformed Starlark indentation early so
	// the LLM gets a useful error rather than a broken tool that
	// blows up at compile time.
	if _, err := config.ParseToolMarkdown([]byte(sb.String()), name); err != nil {
		return "", fmt.Errorf("rendered artifact failed self-parse: %w", err)
	}
	return sb.String(), nil
}

func renderHookMarkdown(name, event, when string, priority int, script string) string {
	var sb strings.Builder
	sb.WriteString("---\n")
	sb.WriteString(fmt.Sprintf("event: %s\n", yamlString(event)))
	if priority > 0 {
		sb.WriteString(fmt.Sprintf("priority: %d\n", priority))
	}
	if strings.TrimSpace(when) != "" {
		sb.WriteString(fmt.Sprintf("when: %s\n", yamlString(when)))
	}
	sb.WriteString("script: |\n")
	for _, line := range strings.Split(strings.ReplaceAll(script, "\r\n", "\n"), "\n") {
		sb.WriteString("  ")
		sb.WriteString(line)
		sb.WriteString("\n")
	}
	sb.WriteString("---\n\n")
	sb.WriteString("# ")
	sb.WriteString(name)
	sb.WriteString("\n\nAuthored by `harness_create_hook` at ")
	sb.WriteString(time.Now().UTC().Format(time.RFC3339))
	sb.WriteString(".\n")
	return sb.String()
}

// yamlKey returns key, quoting if it contains characters that would
// confuse the YAML parser. Most artifact param names are simple
// identifiers so this is rarely triggered.
func yamlKey(s string) string {
	if regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`).MatchString(s) {
		return s
	}
	return yamlString(s)
}

// yamlString returns a single-line YAML scalar, double-quoted with
// minimal escaping. Adequate for descriptions and event names that
// never contain control characters.
func yamlString(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	return `"` + s + `"`
}
