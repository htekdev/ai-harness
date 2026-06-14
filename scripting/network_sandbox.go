// Package scripting — network sandbox for the http.* Starlark module.
//
// Phase 5.5 (Production Hardening): when a NetworkSandbox is attached to
// an Engine, the http.get / http.post built-ins consult the sandbox before
// dispatching the request. The sandbox enforces a domain allowlist with
// default-deny semantics — if the allowlist is non-empty, any host not
// matching one of the listed domains (or one of its sub-domains) is
// blocked with a typed sandbox error.
//
// An Engine with no sandbox attached, or a sandbox whose AllowedDomains
// list is empty, behaves exactly as before (unrestricted network access).
// This preserves back-compat for every existing config; sandboxing is
// strictly opt-in via the top-level `network:` block in harness config.
package scripting

import (
	"fmt"
	"net"
	"net/url"
	"strings"
)

// NetworkSandbox enforces a domain allowlist for outbound http.* calls
// from Starlark scripts.
//
// A nil *NetworkSandbox or a sandbox with no AllowedDomains is treated
// as unrestricted (Allow always returns nil). Once at least one domain
// is configured, the sandbox switches to default-deny: only URLs whose
// host matches an entry exactly, or is a sub-domain of an entry, are
// permitted.
//
// Matching rules:
//   - Comparison is case-insensitive.
//   - Port suffixes on the request URL are ignored ("api.example.com:443"
//     matches "api.example.com").
//   - A configured entry "example.com" matches "example.com" and any
//     sub-domain ("api.example.com", "a.b.example.com").
//   - An entry "*.example.com" matches sub-domains only, not the apex.
//   - An entry "*" matches every host (escape hatch for explicit
//     "allow everything" intent — different from leaving the list empty
//     because the absence of a list means "no sandbox configured", while
//     ["*"] means "sandbox configured, all hosts allowed").
//   - Schemes other than http/https are always rejected when a sandbox
//     is active (file://, gopher://, etc.).
type NetworkSandbox struct {
	allowed []sandboxRule
}

type sandboxRule struct {
	host     string // lowercase host (e.g. "example.com")
	wildcard bool   // entry was "*" or "*.example.com"
	apex     bool   // entry was "example.com" — also matches sub-domains
	matchAll bool   // entry was "*"
}

// NewNetworkSandbox builds a sandbox from a list of allowed domain
// entries. Empty / whitespace-only entries are dropped. The returned
// sandbox is safe for concurrent use.
func NewNetworkSandbox(allowedDomains []string) *NetworkSandbox {
	rules := make([]sandboxRule, 0, len(allowedDomains))
	for _, raw := range allowedDomains {
		entry := strings.TrimSpace(strings.ToLower(raw))
		if entry == "" {
			continue
		}
		switch {
		case entry == "*":
			rules = append(rules, sandboxRule{matchAll: true})
		case strings.HasPrefix(entry, "*."):
			host := strings.TrimPrefix(entry, "*.")
			if host != "" {
				rules = append(rules, sandboxRule{host: host, wildcard: true})
			}
		default:
			rules = append(rules, sandboxRule{host: entry, apex: true})
		}
	}
	return &NetworkSandbox{allowed: rules}
}

// AllowedDomains returns the original (normalized) entries this sandbox
// was built with — useful for logs and `harness context` output.
func (s *NetworkSandbox) AllowedDomains() []string {
	if s == nil {
		return nil
	}
	out := make([]string, 0, len(s.allowed))
	for _, r := range s.allowed {
		switch {
		case r.matchAll:
			out = append(out, "*")
		case r.wildcard:
			out = append(out, "*."+r.host)
		default:
			out = append(out, r.host)
		}
	}
	return out
}

// Allow returns nil if the URL is permitted by the sandbox, or a typed
// sandbox error otherwise. A nil sandbox or one with an empty allowlist
// always returns nil (back-compat: no sandbox configured = unrestricted).
func (s *NetworkSandbox) Allow(rawURL string) error {
	if s == nil || len(s.allowed) == 0 {
		return nil
	}

	u, err := url.Parse(rawURL)
	if err != nil {
		return &SandboxError{URL: rawURL, Reason: fmt.Sprintf("invalid URL: %v", err)}
	}
	scheme := strings.ToLower(u.Scheme)
	if scheme != "http" && scheme != "https" {
		return &SandboxError{URL: rawURL, Reason: fmt.Sprintf("scheme %q is not permitted (only http/https)", u.Scheme)}
	}

	host := strings.ToLower(u.Hostname())
	if host == "" {
		// net/url accepts inputs without a host; treat as denied.
		return &SandboxError{URL: rawURL, Reason: "URL has no host"}
	}
	// Strip a trailing dot so "example.com." matches "example.com".
	host = strings.TrimSuffix(host, ".")
	// Defensive: drop any IPv6 brackets (Hostname() already does this,
	// but guard against future changes).
	host = strings.TrimPrefix(strings.TrimSuffix(host, "]"), "[")
	// Reject IP literals — the allowlist is domain-based, and letting
	// raw IPs through would defeat SSRF protection. Users who genuinely
	// need IP access should set ["*"] explicitly.
	if ip := net.ParseIP(host); ip != nil {
		// Only block if no explicit "*" was configured.
		if !s.matchesAll() {
			return &SandboxError{URL: rawURL, Reason: fmt.Sprintf("IP literal %q is not in the allowlist", host)}
		}
		return nil
	}

	for _, r := range s.allowed {
		if r.matchAll {
			return nil
		}
		if r.host == "" {
			continue
		}
		if r.apex {
			if host == r.host || strings.HasSuffix(host, "."+r.host) {
				return nil
			}
		}
		if r.wildcard {
			if strings.HasSuffix(host, "."+r.host) {
				return nil
			}
		}
	}

	return &SandboxError{URL: rawURL, Reason: fmt.Sprintf("host %q is not in the allowlist", host)}
}

func (s *NetworkSandbox) matchesAll() bool {
	for _, r := range s.allowed {
		if r.matchAll {
			return true
		}
	}
	return false
}

// SandboxError is returned by NetworkSandbox.Allow when a URL is denied.
// Built-ins surface this as a Starlark error so script authors can see
// exactly which URL was rejected and why.
type SandboxError struct {
	URL    string
	Reason string
}

func (e *SandboxError) Error() string {
	return fmt.Sprintf("network sandbox: %s (url=%s)", e.Reason, e.URL)
}
