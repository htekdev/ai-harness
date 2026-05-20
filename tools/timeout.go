package tools

import (
	"context"
	"encoding/json"
	"time"
)

// WithTimeout wraps a tool handler with a fixed execution timeout.
func WithTimeout(handler Handler, timeout time.Duration) Handler {
	if handler == nil || timeout <= 0 {
		return handler
	}
	return func(ctx context.Context, args json.RawMessage) (string, error) {
		ctx, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()
		return handler(ctx, args)
	}
}
