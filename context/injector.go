package context

import (
	"fmt"
	"strings"
)

// Injector prepends active context source content into the system prompt.
// Each injected section is preceded by an HTML comment marker for observability.
type Injector struct {
	loader *Loader
}

// NewInjector creates an Injector backed by the provided Loader.
func NewInjector(loader *Loader) *Injector {
	return &Injector{loader: loader}
}

// Inject prepends the content of each active source into the system prompt,
// in the order provided (the registry pre-sorts by priority ascending).
//
// Format:
//
//	<!-- context: <name> -->
//	<file/url content>
//	<original prompt>
//
// If sources is empty, prompt is returned unchanged.
// Returns ("", error) on any load failure.
func (inj *Injector) Inject(sources []ContextSource, prompt string, root string) (string, error) {
	if len(sources) == 0 {
		return prompt, nil
	}

	var sb strings.Builder
	for _, s := range sources {
		content, err := inj.loader.LoadContent(s, root)
		if err != nil {
			return "", fmt.Errorf("inject context source %q: %w", s.Name, err)
		}
		if content == "" {
			continue
		}
		sb.WriteString("<!-- context: ")
		sb.WriteString(s.Name)
		sb.WriteString(" -->\n")
		sb.WriteString(content)
		if !strings.HasSuffix(content, "\n") {
			sb.WriteByte('\n')
		}
	}
	sb.WriteString(prompt)
	return sb.String(), nil
}
