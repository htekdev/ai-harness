package main

import (
	"context"
	"time"
)

// contextWithTimeout5s returns a 5-second timeout context, used by main()'s
// deferred tracer shutdown so we never block process exit on a slow OTLP
// drain. Wrapped in its own helper so main.go stays free of the time import.
func contextWithTimeout5s() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 5*time.Second)
}
