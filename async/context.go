package async

import "context"

type executorContextKey struct{}

// WithExecutor attaches an Executor to the context so Starlark scripts
// can access it via the async.* builtins.
func WithExecutor(ctx context.Context, e *Executor) context.Context {
	return context.WithValue(ctx, executorContextKey{}, e)
}

// ExecutorFromContext retrieves the Executor attached by WithExecutor.
// Returns nil if no executor is attached.
func ExecutorFromContext(ctx context.Context) *Executor {
	e, _ := ctx.Value(executorContextKey{}).(*Executor)
	return e
}
