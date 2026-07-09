package async

import (
	"context"
)

// Result wraps the outcome of an awaited async placeholder.
type Result struct {
	// ID is the placeholder ID.
	ID string
	// ToolName is the name of the tool that produced this result.
	ToolName string
	// Value is the tool's output string (only valid when Err == nil).
	Value string
	// Err is the error if the tool failed or was cancelled.
	Err error
}

// WaitAll blocks until all placeholders in refs resolve (complete, error, or
// cancelled). It returns one Result per placeholder in the same order as refs.
// The context is used for cancellation.
func WaitAll(ctx context.Context, refs []*Placeholder) []Result {
	results := make([]Result, len(refs))
	for i, p := range refs {
		v, err := p.Result(ctx)
		results[i] = Result{
			ID:       p.ID(),
			ToolName: p.ToolName(),
			Value:    v,
			Err:      err,
		}
	}
	return results
}

// WaitAny blocks until the first placeholder in refs resolves and returns its
// Result. All placeholders continue running; use Race if you want to cancel
// the losers.
func WaitAny(ctx context.Context, refs []*Placeholder) Result {
	if len(refs) == 0 {
		return Result{}
	}

	// Merge all Done channels into a single select.
	chosen := make(chan *Placeholder, 1)
	for _, p := range refs {
		go func(ph *Placeholder) {
			select {
			case <-ph.Done():
				select {
				case chosen <- ph:
				default:
				}
			case <-ctx.Done():
			}
		}(p)
	}

	select {
	case p := <-chosen:
		v, err := p.Result(ctx)
		return Result{ID: p.ID(), ToolName: p.ToolName(), Value: v, Err: err}
	case <-ctx.Done():
		return Result{Err: wrap(KindTimeout, "context cancelled in WaitAny", ctx.Err())}
	}
}

// Race blocks until the first placeholder in refs resolves, then cancels all
// the other (still-pending) placeholders and returns the winning Result.
func Race(ctx context.Context, refs []*Placeholder) Result {
	if len(refs) == 0 {
		return Result{}
	}

	// Merge all Done channels.
	chosen := make(chan *Placeholder, 1)
	for _, p := range refs {
		go func(ph *Placeholder) {
			select {
			case <-ph.Done():
				select {
				case chosen <- ph:
				default:
				}
			case <-ctx.Done():
			}
		}(p)
	}

	select {
	case winner := <-chosen:
		// Cancel losers.
		for _, p := range refs {
			if p.ID() != winner.ID() {
				p.cancelPlaceholder()
			}
		}
		v, err := winner.Result(ctx)
		return Result{ID: winner.ID(), ToolName: winner.ToolName(), Value: v, Err: err}
	case <-ctx.Done():
		// Cancel everyone.
		for _, p := range refs {
			p.cancelPlaceholder()
		}
		return Result{Err: wrap(KindTimeout, "context cancelled in Race", ctx.Err())}
	}
}
