package context

// SourceKind defines the type of content source.
type SourceKind string

const (
	// KindFile loads content from a local file path.
	KindFile SourceKind = "file"

	// KindURL loads content from an HTTP/HTTPS URL.
	KindURL SourceKind = "url"
)

// Scope defines the caching behaviour for a context source.
type Scope string

const (
	// ScopeSession loads content once per agent session and caches it.
	ScopeSession Scope = "session"

	// ScopeTurn reloads content from the source on every agent turn.
	ScopeTurn Scope = "turn"
)

// Source is a declarative specification of content to load into the agent's
// context window. Sources are configured in identity.md (or harness.md)
// under the context.sources key and evaluated every turn.
type Source struct {
	// Name uniquely identifies this source within the registry.
	Name string

	// Kind is the source type: "file" or "url". Defaults to "file".
	Kind SourceKind

	// Path is the file path (relative to the project root) or URL.
	Path string

	// When is a Starlark expression evaluated each turn.
	// An empty string means the source is always active.
	When string

	// Trigger activates this source when the named event fires.
	// If both When and Trigger are set, When takes precedence for
	// per-turn evaluation; Trigger acts as an additional activation path.
	Trigger string

	// Priority determines injection order. Lower priority content is
	// injected first; higher priority content appears later and can
	// effectively override earlier content. Default is 0.
	Priority int

	// Scope controls caching. "session" loads content once and reuses it;
	// "turn" reloads content every turn. Defaults to "session".
	Scope Scope

	// TTL is the number of turns a source stays active after activation
	// (0 means no automatic expiry).
	TTL int
}

// SourceEntry pairs a Source declaration with its runtime state.
type SourceEntry struct {
	// Source is the original declaration.
	Source Source

	// Active reports whether this source is currently active.
	Active bool

	// Reason describes why the source is active or inactive, e.g.:
	//   "always-on"
	//   "when: ctx.get(\"mode\") == \"pull_request\""
	//   "condition false: ..."
	//   "trigger: error"
	//   "TTL expired after N turns"
	Reason string

	// Content holds the loaded file or URL content when the source is active.
	// Empty if inactive or not yet loaded.
	Content string

	// contentLoaded tracks whether content has been fetched at least once
	// (used for session-scoped caching).
	contentLoaded bool

	// activatedAt is the turn number when this source became active.
	activatedAt int
}
