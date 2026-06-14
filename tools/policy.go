package tools

import (
	"path/filepath"
	"strings"

	"github.com/htekdev/ai-harness/harness/errs"
)

// PolicyMode controls how the allow/deny lists combine when deciding whether
// a tool may be invoked.
type PolicyMode string

const (
	// PolicyModeAllowlist means a tool is allowed ONLY when it matches an
	// entry in Allow (or Allow is empty and the policy itself is empty).
	// Deny entries always take precedence.
	PolicyModeAllowlist PolicyMode = "allowlist"

	// PolicyModeDenylist means a tool is allowed UNLESS it matches an entry
	// in Deny. Allow entries are ignored in this mode (used when an operator
	// only wants to subtract from the registered set).
	PolicyModeDenylist PolicyMode = "denylist"
)

// Policy is a per-session governance surface that controls which tools the
// agent is permitted to invoke. Patterns support shell-style globs via
// filepath.Match (e.g. "fs.*", "*_admin", "exec").
//
// Resolution rules:
//
//   - An empty Policy (zero value) allows everything (no governance).
//   - Deny ALWAYS wins over Allow when a tool matches both.
//   - In allowlist mode (default when Allow is non-empty): a tool is allowed
//     only when it matches at least one Allow pattern AND no Deny pattern.
//   - In denylist mode (or when Allow is empty and Mode != allowlist): a tool
//     is allowed unless it matches a Deny pattern.
//
// Filtered tools are also hidden from List() and ToOpenAIFormat() so the
// model never sees tools it cannot call — preventing wasted turns.
type Policy struct {
	// Mode optionally pins the resolution mode. When empty, the mode is
	// inferred from the lists: non-empty Allow ⇒ allowlist, otherwise
	// denylist.
	Mode PolicyMode `json:"mode,omitempty" yaml:"mode,omitempty"`

	// Allow is the set of tool-name patterns permitted in allowlist mode.
	Allow []string `json:"allow,omitempty" yaml:"allow,omitempty"`

	// Deny is the set of tool-name patterns blocked regardless of mode.
	Deny []string `json:"deny,omitempty" yaml:"deny,omitempty"`
}

// IsEmpty reports whether the policy has any rules at all. An empty policy
// is a no-op (allows everything).
func (p *Policy) IsEmpty() bool {
	return p == nil || (len(p.Allow) == 0 && len(p.Deny) == 0 && p.Mode == "")
}

// effectiveMode resolves the mode honoring explicit Mode and falling back to
// "allowlist" when Allow is non-empty, otherwise "denylist".
func (p *Policy) effectiveMode() PolicyMode {
	if p.Mode == PolicyModeAllowlist || p.Mode == PolicyModeDenylist {
		return p.Mode
	}
	if len(p.Allow) > 0 {
		return PolicyModeAllowlist
	}
	return PolicyModeDenylist
}

// Allows reports whether the given tool name is permitted under this policy.
// A nil/empty policy allows every name.
func (p *Policy) Allows(name string) bool {
	if p.IsEmpty() {
		return true
	}
	if matchAny(p.Deny, name) {
		return false
	}
	if p.effectiveMode() == PolicyModeAllowlist {
		return matchAny(p.Allow, name)
	}
	return true
}

// Validate ensures every pattern is syntactically valid for filepath.Match.
// It also rejects empty entries (likely a config typo).
func (p *Policy) Validate() error {
	if p.IsEmpty() {
		return nil
	}
	if p.Mode != "" && p.Mode != PolicyModeAllowlist && p.Mode != PolicyModeDenylist {
		return errs.Newf(errs.KindConfig, "tools.policy.validate", "invalid mode %q (want allowlist|denylist)", string(p.Mode))
	}
	if err := validatePatterns("allow", p.Allow); err != nil {
		return err
	}
	if err := validatePatterns("deny", p.Deny); err != nil {
		return err
	}
	return nil
}

func validatePatterns(field string, pats []string) error {
	for i, raw := range pats {
		pat := strings.TrimSpace(raw)
		if pat == "" {
			return errs.Newf(errs.KindConfig, "tools.policy.validate", "%s[%d] is empty", field, i)
		}
		// filepath.Match returns ErrBadPattern only when the pattern itself
		// is malformed; any name works as a probe.
		if _, err := filepath.Match(pat, "x"); err != nil {
			return errs.Wrap(errs.KindConfig, "tools.policy.validate", err, "%s[%d] %q is not a valid glob", field, i, pat)
		}
	}
	return nil
}

// matchAny reports whether name matches any glob pattern in pats. An exact
// match short-circuits the glob walk.
func matchAny(pats []string, name string) bool {
	for _, raw := range pats {
		pat := strings.TrimSpace(raw)
		if pat == "" {
			continue
		}
		if pat == name {
			return true
		}
		ok, err := filepath.Match(pat, name)
		if err == nil && ok {
			return true
		}
	}
	return false
}

// SetPolicy attaches a policy to the registry. Subsequent List, Get,
// ToOpenAIFormat, and Execute calls will respect it. Passing nil clears the
// policy.
func (r *Registry) SetPolicy(p *Policy) error {
	if p != nil {
		if err := p.Validate(); err != nil {
			return err
		}
	}
	r.policyMu.Lock()
	r.policy = p
	r.policyMu.Unlock()
	return nil
}

// Policy returns the current policy (or nil when none set).
func (r *Registry) Policy() *Policy {
	r.policyMu.RLock()
	defer r.policyMu.RUnlock()
	return r.policy
}

// allowed evaluates the (possibly nil) policy under the registry mutex.
func (r *Registry) allowed(name string) bool {
	r.policyMu.RLock()
	p := r.policy
	r.policyMu.RUnlock()
	if p == nil {
		return true
	}
	return p.Allows(name)
}
