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
	"harness_list_artifacts",
	"harness_remove_artifact",
}

// registerSelfAugmentTools installs the four self-augmenting meta-tools
// onto the harness's tool registry. Called once during NewFromConfig.
func registerSelfAugmentTools(h *Harness) error {
	defs := []struct {
		def     tools.Definition
		handler tools.Handler
	}{
		{createToolDefinition(), h.handleCreateTool},
		{createHookDefinition(), h.handleCreateHook},
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
			"for a capability the current toolset cannot satisfy. Tools are written as Starlark scripts " +
			"with a `def run(args)` entrypoint and have access to all Starlark built-ins (http, fs, json, " +
			"time, log, etc.). Tool definitions persist on disk across restarts.",
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
			"harness. Refuses to remove built-in tools (delegate, harness_*). Use carefully — this " +
			"is destructive and persists across restarts because the underlying file is deleted.",
		Parameters: []tools.Parameter{
			{Name: "kind", Type: tools.TypeString, Required: true,
				Description: "Artifact category: \"tool\" or \"hook\"."},
			{Name: "name", Type: tools.TypeString, Required: true,
				Description: "Artifact name (matches the file basename, no extension)."},
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

	// Hot-reload so the tool becomes immediately callable.
	if err := h.Reload(); err != nil {
		return "", fmt.Errorf("reload after create_tool: %w", err)
	}

	resp := map[string]any{
		"status":      "ok",
		"created":     args.Name,
		"path":        relPath(h.baseDir, path),
		"description": strings.TrimSpace(args.Description),
		"reloaded":    true,
		"hint":        fmt.Sprintf("Tool %q is now callable. You can invoke it on your next step.", args.Name),
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

	body := renderHookMarkdown(args.Name, args.Event, args.When, args.Priority, args.Script)

	dir := filepath.Join(h.baseDir, ".harness", "hooks")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("mkdir %s: %w", dir, err)
	}
	path := filepath.Join(dir, args.Name+".md")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		return "", fmt.Errorf("write %s: %w", path, err)
	}

	if err := h.Reload(); err != nil {
		return "", fmt.Errorf("reload after create_hook: %w", err)
	}

	resp := map[string]any{
		"status":   "ok",
		"created":  args.Name,
		"event":    args.Event,
		"path":     relPath(h.baseDir, path),
		"reloaded": true,
		"hint":     fmt.Sprintf("Hook %q on event %q is now active.", args.Name, args.Event),
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
	default:
		return "", fmt.Errorf("kind must be \"tool\" or \"hook\", got %q", args.Kind)
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

	if err := h.Reload(); err != nil {
		return "", fmt.Errorf("reload after remove_artifact: %w", err)
	}

	resp := map[string]any{
		"status":   "ok",
		"removed":  args.Name,
		"kind":     subdir,
		"path":     relPath(h.baseDir, path),
		"reloaded": true,
	}
	out, _ := json.MarshalIndent(resp, "", "  ")
	return string(out), nil
}

// ---------- Reload ----------

// Reload rescans `.harness/tools/` and `.harness/hooks/` and reconciles the
// running registry / hook system against disk state. New artifact files are
// registered, modified files are re-registered (the `Replace` path), and
// files that have been deleted on disk are unregistered.
//
// Built-in tools (`delegate`, `harness_*`) and inline-config tools/hooks
// declared directly in the main harness.md frontmatter are never touched.
//
// Safe for concurrent use; serialized by reloadMu so multiple meta-tool
// calls in the same agent turn cannot race.
func (h *Harness) Reload() error {
	h.reloadMu.Lock()
	defer h.reloadMu.Unlock()

	dirResult, err := config.LoadDirectory(h.baseDir)
	if err != nil {
		return fmt.Errorf("scan .harness: %w", err)
	}
	if dirResult == nil || dirResult.Config == nil {
		return nil
	}

	// --- Tools ---
	newToolNames := make(map[string]struct{}, len(dirResult.Config.Tools))
	for _, tc := range dirResult.Config.Tools {
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

		// Replace overwrites if the tool already exists, otherwise registers.
		// Refuse to clobber built-ins by accident — a user could create a
		// `.harness/tools/delegate.md` and break the harness.
		if isBuiltinToolName(tc.Name) || tc.Name == "delegate" || tc.Name == "delegate_async" || tc.Name == "delegate_status" || tc.Name == "delegate_result" {
			Logger().Warn("ignoring file-based tool with reserved name", "name", tc.Name)
			delete(newToolNames, tc.Name)
			continue
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
	// Drop hooks that used to be on disk but no longer are. We don't know
	// the original event for unregister, so iterate every event.
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

// allHookEvents enumerates every event Hook.Unregister might need to clear
// during Reload. Kept in sync with hooks.IsValidEvent.
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

## Self-Augmenting Harness (Phase 5.8)

You are running on AI Harness — a Go agent runtime where YOU can author
your own harness at runtime. You have four built-in meta-tools:

- **harness_list_artifacts** — call this whenever the user asks "what can
  you do?" or before deciding to create a new tool. It returns every
  registered tool, hook, and agent.
- **harness_create_tool** — author a new tool by writing its Starlark
  script and a clear description. The new tool is hot-reloaded and
  callable on your very next step.
- **harness_create_hook** — author a new hook (guardrails, logging,
  redaction) on a lifecycle event. Active immediately after creation.
- **harness_remove_artifact** — delete an artifact you created earlier.

When the user asks for a capability you don't have, prefer **creating a
tool** over saying "I can't do that". The capability persists on disk
(.harness/tools/<name>.md) and survives restarts. Tools have access to
the full Starlark stdlib (http, fs, json, time, log, regex, hash, ...).

Keep created tools small, focused, and well-described. The description
field is what the model (you, on a future turn) will see when deciding
whether to call the tool — write it like prompt engineering, not an
implementation comment.
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
