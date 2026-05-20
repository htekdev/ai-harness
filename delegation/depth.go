package delegation

import "context"

type contextKey string

const depthKey contextKey = "delegation.depth"

// MaxHardDepth is the absolute maximum delegation depth regardless of configuration.
const MaxHardDepth = 5

// GetDepth returns the current delegation depth from context.
func GetDepth(ctx context.Context) int {
	if v, ok := ctx.Value(depthKey).(int); ok {
		return v
	}
	return 0
}

// WithDepth returns a new context with the delegation depth set.
func WithDepth(ctx context.Context, depth int) context.Context {
	return context.WithValue(ctx, depthKey, depth)
}
