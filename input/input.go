// Package input provides the Source abstraction for AI Harness session input.
//
// A Source produces user-turn Events that the harness Serve loop selects over.
// This generalizes session input beyond os.Stdin so that Telegram, MeshWire,
// Slack, webhooks, file watchers, and cron triggers can all feed turns into a
// running harness session through a single, uniform interface.
//
// Roadmap fit: Phase 4 (Event Sources / Watcher Adapters). See
// data/specs/ai-harness-telegram-integration-v1.md in htekdev/rocha-family.
package input

import "context"

// Event is a single user-turn input produced by a Source.
type Event struct {
	// SourceName identifies which Source produced the event (e.g. "stdin", "telegram").
	SourceName string

	// SessionKey routes the event to a specific session within a serve process.
	// Empty string means the default/global session. For Telegram this is the
	// chat_id as a string so each chat gets its own conversation history.
	SessionKey string

	// Text is the user-turn input that will be passed to Harness.Run.
	Text string

	// Metadata carries source-specific fields (chat_id, message_id, user, etc.).
	Metadata map[string]string
}

// Source produces a stream of input Events for a serve loop to consume.
//
// Implementations must:
//   - Block in Read until the next event is available or ctx is cancelled.
//   - Return ctx.Err() when ctx is cancelled.
//   - Return io.EOF (or a wrapped equivalent) when no more events will come,
//     so the serve loop can drop the source cleanly without exiting.
//   - Be safe for a single goroutine pumping Read in a loop.
//
// A Source MAY implement Replier to receive routed responses for events it
// produced (e.g. TelegramSource sends Harness output back to the originating
// chat). Sources that don't implement Replier simply produce one-way input.
type Source interface {
	Name() string
	Read(ctx context.Context) (Event, error)
	Close() error
}

// Replier is an optional interface for Sources that can deliver responses
// back to their origin (e.g. Telegram chat, Slack channel).
type Replier interface {
	Reply(ctx context.Context, ev Event, text string) error
}
