package input

import (
	"bufio"
	"context"
	"io"
	"strings"
)

// StdinSource reads user-turn events from a line-oriented io.Reader (typically os.Stdin).
//
// Each non-empty trimmed line becomes one Event with SessionKey="" (default session).
// EOF on the underlying reader is propagated as io.EOF so the serve loop can drop
// the source cleanly without terminating other sources.
type StdinSource struct {
	scanner *bufio.Scanner
	prompt  func() // optional: called before each Read to print "> "
}

// NewStdinSource wraps r in a StdinSource. If prompt is non-nil it is called
// before each blocking read (useful for interactive REPL prompts).
func NewStdinSource(r io.Reader, prompt func()) *StdinSource {
	return &StdinSource{scanner: bufio.NewScanner(r), prompt: prompt}
}

// Name returns the source identifier.
func (s *StdinSource) Name() string { return "stdin" }

// Read blocks for the next non-empty line. Returns io.EOF when the underlying
// reader is exhausted. ctx cancellation is best-effort: bufio.Scanner does not
// natively support context, so callers waiting on ctx.Done should run Read in
// a goroutine and abandon it on cancel.
func (s *StdinSource) Read(ctx context.Context) (Event, error) {
	for {
		if err := ctx.Err(); err != nil {
			return Event{}, err
		}
		if s.prompt != nil {
			s.prompt()
		}
		if !s.scanner.Scan() {
			if err := s.scanner.Err(); err != nil {
				return Event{}, err
			}
			return Event{}, io.EOF
		}
		text := strings.TrimSpace(s.scanner.Text())
		if text == "" {
			continue
		}
		return Event{
			SourceName: s.Name(),
			SessionKey: "",
			Text:       text,
		}, nil
	}
}

// Close is a no-op for stdin (the OS owns the FD lifecycle).
func (s *StdinSource) Close() error { return nil }
