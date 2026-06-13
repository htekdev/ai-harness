// Serve-mode configuration parsing for the AI harness.
//
// The `serve:` block under harness.md frontmatter declaratively configures the
// input sources that `harness serve` will activate, replacing the repeated
// `--source/--telegram-*` CLI flags. Secrets (bot tokens, MeshWire tokens) are
// always loaded from env vars referenced by `token_env` — never embedded.
package config

import (
	"fmt"
	"strings"
)

// ServeConfig is the declarative configuration for `harness serve`.
//
// Shape (under harness.md frontmatter):
//
//	serve:
//	  sources:
//	    - type: stdin
//	    - type: telegram
//	      token_env: TELEGRAM_BOT_TOKEN
//	      poll_timeout_seconds: 25
//	      chat_allowlist: [7729308746]
//	    - type: meshwire
//	      token_env: MESHWIRE_TOKEN
//	      mesh_id: family-mesh
//	      agent_id: harness-bot
//	      sender_allowlist: [peer-reviewer]
//	      poll_timeout_seconds: 30
//	      base_url: https://meshwire.io
type ServeConfig struct {
	Sources []ServeSourceConfig `yaml:"sources" json:"sources"`
}

// ServeSourceConfig is one entry under `serve.sources`. Fields are unioned
// across all source types; only the fields relevant to the chosen Type are
// honored, others are ignored.
type ServeSourceConfig struct {
	// Type is the source kind: "stdin", "telegram", or "meshwire".
	Type string `yaml:"type" json:"type"`

	// TokenEnv is the env var name holding the bearer token / bot token.
	// Used by "telegram" (Bot API token) and "meshwire" (auth token).
	TokenEnv string `yaml:"token_env,omitempty" json:"token_env,omitempty"`

	// PollTimeoutSeconds is the long-poll timeout for sources that support it
	// (telegram: max 50, meshwire: max 60). Zero means "use source default".
	PollTimeoutSeconds int `yaml:"poll_timeout_seconds,omitempty" json:"poll_timeout_seconds,omitempty"`

	// --- Telegram-specific ---

	// ChatAllowlist is the set of Telegram chat IDs allowed to invoke the
	// harness. Required (non-empty) for type=telegram.
	ChatAllowlist []int64 `yaml:"chat_allowlist,omitempty" json:"chat_allowlist,omitempty"`

	// OffsetPath is an optional file path used by FileOffsetStore to durably
	// persist the last-acked Telegram update_id across restarts. If empty,
	// offsets live only in memory.
	OffsetPath string `yaml:"offset_path,omitempty" json:"offset_path,omitempty"`

	// --- MeshWire-specific (forward-compat with PR #75) ---

	// MeshID is the MeshWire mesh this harness joins. Required for type=meshwire.
	MeshID string `yaml:"mesh_id,omitempty" json:"mesh_id,omitempty"`

	// AgentID is this harness's agent_id within the mesh. Required for type=meshwire.
	AgentID string `yaml:"agent_id,omitempty" json:"agent_id,omitempty"`

	// SenderAllowlist is the set of peer agent_ids whose messages this harness
	// will accept. Required (non-empty) for type=meshwire.
	SenderAllowlist []string `yaml:"sender_allowlist,omitempty" json:"sender_allowlist,omitempty"`

	// BaseURL overrides the MeshWire API base URL (default: https://meshwire.io).
	BaseURL string `yaml:"base_url,omitempty" json:"base_url,omitempty"`
}

// KnownServeSourceTypes lists every source type the parser recognizes. The
// `harness serve` command may not yet have an implementation for every type
// here (e.g. meshwire only lights up after PR #75 lands), but the config
// surface accepts them so harness.md is forward-compatible.
var KnownServeSourceTypes = []string{"stdin", "telegram", "meshwire"}

// Validate checks the serve block for required fields per source type. It
// returns an error describing every issue found, joined by ";".
//
// Validate is intentionally permissive about *unknown* source types — those
// are flagged so a stale binary running new config gets a clean error rather
// than silently dropping the source.
func (s *ServeConfig) Validate() error {
	if s == nil {
		return nil
	}
	if len(s.Sources) == 0 {
		return fmt.Errorf("serve.sources must contain at least one entry")
	}
	var issues []string
	seen := make(map[string]int, len(s.Sources))
	for i, src := range s.Sources {
		t := strings.ToLower(strings.TrimSpace(src.Type))
		if t == "" {
			issues = append(issues, fmt.Sprintf("serve.sources[%d].type cannot be empty", i))
			continue
		}
		if !isKnownServeSourceType(t) {
			issues = append(issues, fmt.Sprintf("serve.sources[%d].type %q is not recognized (supported: %s)", i, src.Type, strings.Join(KnownServeSourceTypes, ", ")))
			continue
		}
		seen[t]++
		switch t {
		case "stdin":
			// no required fields
		case "telegram":
			if strings.TrimSpace(src.TokenEnv) == "" {
				issues = append(issues, fmt.Sprintf("serve.sources[%d] (telegram): token_env is required", i))
			}
			if len(src.ChatAllowlist) == 0 {
				issues = append(issues, fmt.Sprintf("serve.sources[%d] (telegram): chat_allowlist must be non-empty", i))
			}
			if src.PollTimeoutSeconds < 0 || src.PollTimeoutSeconds > 50 {
				issues = append(issues, fmt.Sprintf("serve.sources[%d] (telegram): poll_timeout_seconds must be between 0 and 50, got %d", i, src.PollTimeoutSeconds))
			}
		case "meshwire":
			if strings.TrimSpace(src.TokenEnv) == "" {
				issues = append(issues, fmt.Sprintf("serve.sources[%d] (meshwire): token_env is required", i))
			}
			if strings.TrimSpace(src.MeshID) == "" {
				issues = append(issues, fmt.Sprintf("serve.sources[%d] (meshwire): mesh_id is required", i))
			}
			if strings.TrimSpace(src.AgentID) == "" {
				issues = append(issues, fmt.Sprintf("serve.sources[%d] (meshwire): agent_id is required", i))
			}
			if len(src.SenderAllowlist) == 0 {
				issues = append(issues, fmt.Sprintf("serve.sources[%d] (meshwire): sender_allowlist must be non-empty", i))
			}
			if src.PollTimeoutSeconds < 0 || src.PollTimeoutSeconds > 60 {
				issues = append(issues, fmt.Sprintf("serve.sources[%d] (meshwire): poll_timeout_seconds must be between 0 and 60, got %d", i, src.PollTimeoutSeconds))
			}
		}
	}
	for t, count := range seen {
		if count > 1 {
			issues = append(issues, fmt.Sprintf("serve.sources contains %d entries of type %q (duplicates are not supported in v1)", count, t))
		}
	}
	if len(issues) > 0 {
		return fmt.Errorf("serve config invalid: %s", strings.Join(issues, "; "))
	}
	return nil
}

// NormalizedType returns the lowercased, trimmed type string.
func (s ServeSourceConfig) NormalizedType() string {
	return strings.ToLower(strings.TrimSpace(s.Type))
}

func isKnownServeSourceType(t string) bool {
	for _, k := range KnownServeSourceTypes {
		if k == t {
			return true
		}
	}
	return false
}
