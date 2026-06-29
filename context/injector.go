package context

import "strings"

// InjectionResult holds the ordered content to be injected into the system
// prompt, plus a provenance record for observability.
type InjectionResult struct {
	// Content is the concatenated, priority-ordered content of all active sources.
	// Each source's content is separated by a blank line.
	Content string

	// Sources lists the active source entries that contributed to Content,
	// in the same order they appear in Content.
	Sources []*SourceEntry
}

// BuildInjection returns the priority-ordered content of all active sources
// suitable for prepending or appending to the system prompt.
//
// Sources are ordered by Priority (ascending) then Name (alphabetical).
// An empty InjectionResult is returned when no sources are active.
func BuildInjection(reg *SourceRegistry) InjectionResult {
	active := reg.Active()
	if len(active) == 0 {
		return InjectionResult{}
	}

	parts := make([]string, 0, len(active))
	for _, e := range active {
		if e.Content != "" {
			parts = append(parts, e.Content)
		}
	}

	return InjectionResult{
		Content: strings.Join(parts, "\n\n"),
		Sources: active,
	}
}

// InjectIntoPrompt returns a new system prompt string with the injected
// context sources prepended before the existing prompt content.
// When the injection result is empty, the original prompt is returned unchanged.
func InjectIntoPrompt(systemPrompt string, result InjectionResult) string {
	if result.Content == "" {
		return systemPrompt
	}
	if systemPrompt == "" {
		return result.Content
	}
	return result.Content + "\n\n" + systemPrompt
}
