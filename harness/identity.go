package harness

import (
	_ "embed"
	"strings"
)

//go:embed identity_default.md
var coreIdentityDefault string

const coreIdentityMinimal = "You are an AI Harness agent. See the docs at https://htekdev.github.io/ai-harness/ for how to extend yourself."

// CoreIdentity returns the shipped baseline identity for a configured level.
// Supported levels: enabled (default), minimal, disabled.
func CoreIdentity(level string) string {
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "", "enabled":
		return strings.TrimSpace(coreIdentityDefault)
	case "minimal":
		return coreIdentityMinimal
	case "disabled":
		return ""
	default:
		return strings.TrimSpace(coreIdentityDefault)
	}
}

// ComposeSystemPrompt prepends the baseline identity to a user prompt when
// enabled, separated by "\n\n---\n\n".
func ComposeSystemPrompt(userPrompt, level string) string {
	core := CoreIdentity(level)
	if core == "" {
		return userPrompt
	}
	if strings.TrimSpace(userPrompt) == "" {
		return core
	}
	return core + "\n\n---\n\n" + userPrompt
}
