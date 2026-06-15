package harness

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"sync"
)

// Logging environment variables.
//
//	HARNESS_LOG_LEVEL  = debug | info | warn | error  (default: info)
//	HARNESS_LOG_FORMAT = text  | json                  (default: text)
//
// Phase 5.1: structured logging via Go's stdlib slog. Every internal logger in
// AI Harness (agent, delegation, evals, serve) flows through the package-level
// Logger() accessor so a single configuration knob controls the entire runtime.
// CLI flags override env vars; env vars override the default.
const (
	EnvLogLevel  = "HARNESS_LOG_LEVEL"
	EnvLogFormat = "HARNESS_LOG_FORMAT"
)

var (
	loggerMu     sync.RWMutex
	globalLogger *slog.Logger
)

// Logger returns the process-wide structured logger. The first call lazily
// constructs one from HARNESS_LOG_LEVEL / HARNESS_LOG_FORMAT environment
// variables. Subsequent calls reuse that logger unless SetLogger has been
// called.
//
// Returned logger is always non-nil and safe for concurrent use.
func Logger() *slog.Logger {
	loggerMu.RLock()
	l := globalLogger
	loggerMu.RUnlock()
	if l != nil {
		return l
	}

	loggerMu.Lock()
	defer loggerMu.Unlock()
	if globalLogger == nil {
		built, err := NewLogger(os.Getenv(EnvLogFormat), os.Getenv(EnvLogLevel), os.Stderr)
		if err != nil {
			// Fall back to default text/info logger; never panic at runtime
			// because of a malformed env var.
			built, _ = NewLogger("text", "info", os.Stderr)
		}
		globalLogger = built
		slog.SetDefault(built)
	}
	return globalLogger
}

// SetLogger installs the process-wide logger. Used by the CLI after parsing
// --log-level / --log-format flags. Passing nil resets to the env-derived
// default on the next Logger() call.
//
// SetLogger also calls slog.SetDefault(l) so packages that hold a reference to
// slog.Default() (agent, delegation, evals, scripting) automatically pick up
// the configured handler without needing to import the harness package
// directly — which would create an import cycle.
func SetLogger(l *slog.Logger) {
	loggerMu.Lock()
	defer loggerMu.Unlock()
	globalLogger = l
	if l != nil {
		slog.SetDefault(l)
	}
}

// NewLogger constructs a slog.Logger with the requested format and level,
// writing to w. Empty format/level fall back to the documented defaults
// ("text" / "info"). Unknown values return an error so the CLI can surface
// the misconfiguration instead of silently falling back.
func NewLogger(format, level string, w io.Writer) (*slog.Logger, error) {
	if w == nil {
		w = os.Stderr
	}
	lvl, err := parseLevel(level)
	if err != nil {
		return nil, err
	}
	opts := &slog.HandlerOptions{Level: lvl}

	switch normalizeFormat(format) {
	case "json":
		return slog.New(NewTraceContextHandler(slog.NewJSONHandler(w, opts))), nil
	case "text":
		return slog.New(NewTraceContextHandler(slog.NewTextHandler(w, opts))), nil
	default:
		return nil, fmt.Errorf("invalid %s=%q (want text|json)", EnvLogFormat, format)
	}
}

// ConfigureLoggerFromFlags applies the parsed --log-level / --log-format
// values (or env defaults) and installs the resulting logger globally.
// Empty arguments mean "use the env var / default" for that knob.
func ConfigureLoggerFromFlags(format, level string) error {
	if format == "" {
		format = os.Getenv(EnvLogFormat)
	}
	if level == "" {
		level = os.Getenv(EnvLogLevel)
	}
	l, err := NewLogger(format, level, os.Stderr)
	if err != nil {
		return err
	}
	SetLogger(l)
	return nil
}

func parseLevel(s string) (slog.Level, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "info":
		return slog.LevelInfo, nil
	case "debug":
		return slog.LevelDebug, nil
	case "warn", "warning":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	default:
		return 0, fmt.Errorf("invalid %s=%q (want debug|info|warn|error)", EnvLogLevel, s)
	}
}

func normalizeFormat(s string) string {
	v := strings.ToLower(strings.TrimSpace(s))
	if v == "" {
		return "text"
	}
	return v
}
