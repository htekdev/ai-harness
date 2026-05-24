package artifact

// ComposeOptions controls how artifact composition behaves.
// Use functional options (With* functions) to configure.
type ComposeOptions struct {
	// IncludeInactive includes artifacts where Active == false in composition.
	// Default: false (only active artifacts are composed).
	IncludeInactive bool

	// TypeFilter restricts composition to specific artifact types.
	// Default: nil (all types included).
	TypeFilter []Type

	// TagFilter restricts composition to artifacts with at least one matching tag.
	// Default: nil (no tag filtering).
	TagFilter []string

	// EvalFn is called for each artifact's condition to dynamically determine activation.
	// When set, overrides the pre-computed Active field.
	// When nil, the pre-computed Active field (from EvaluateConditions) is used.
	EvalFn func(condition string) (bool, error)
}

// ComposeOption is a functional option for configuring composition behavior.
type ComposeOption func(*ComposeOptions)

// WithIncludeInactive includes artifacts that have been deactivated by EvaluateConditions.
// Useful for debugging and observability (see which artifacts were excluded and why).
func WithIncludeInactive() ComposeOption {
	return func(o *ComposeOptions) {
		o.IncludeInactive = true
	}
}

// WithTypeFilter restricts composition to only the specified artifact types.
func WithTypeFilter(types ...Type) ComposeOption {
	return func(o *ComposeOptions) {
		o.TypeFilter = types
	}
}

// WithTagFilter restricts composition to artifacts that have at least one of the given tags.
func WithTagFilter(tags ...string) ComposeOption {
	return func(o *ComposeOptions) {
		o.TagFilter = tags
	}
}

// WithEvalFn sets a condition evaluation function that overrides cached Active state.
// This re-evaluates conditions at composition time rather than using the cached result
// from EvaluateConditions.
func WithEvalFn(fn func(condition string) (bool, error)) ComposeOption {
	return func(o *ComposeOptions) {
		o.EvalFn = fn
	}
}

// defaultOptions returns the default composition options.
func defaultOptions() *ComposeOptions {
	return &ComposeOptions{
		IncludeInactive: false,
		TypeFilter:      nil,
		TagFilter:       nil,
		EvalFn:          nil,
	}
}
