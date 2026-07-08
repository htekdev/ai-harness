package async

import (
	"context"
	"sync"
)

// WaitAll executes all pending nodes in g (including transitive deps), then
// returns the results of the specified ref IDs in order.
//
// If any referenced placeholder fails, WaitAll returns the first error.
// An empty refs slice is valid and returns an empty slice with no error.
func WaitAll(ctx context.Context, g *Graph, refs []string, dispatch DispatchFunc) ([]string, error) {
	// Validate all refs exist before doing any work.
	for _, ref := range refs {
		if _, ok := g.Get(ref); !ok {
			return nil, wrapUnknown(ref)
		}
	}

	// Run the entire graph to completion. This handles dependency ordering
	// and ensures all transitive deps of the requested refs are resolved.
	e := NewExecutor(DefaultMaxConcurrent)
	if err := e.RunGraph(ctx, g, dispatch); err != nil {
		return nil, err
	}

	// Collect results in ref order.
	results := make([]string, len(refs))
	for i, ref := range refs {
		p, ok := g.Get(ref)
		if !ok {
			return nil, wrapUnknown(ref)
		}
		result, err := p.Result()
		if err != nil {
			return nil, err
		}
		results[i] = result
	}
	return results, nil
}

// Race executes all refs in g concurrently and returns the result of the first
// placeholder that completes successfully. The other placeholders are
// cancelled via context cancellation.
//
// Returns ErrNoRaceWinner if refs is empty or if all competitors fail.
func Race(ctx context.Context, g *Graph, refs []string, dispatch DispatchFunc) (string, error) {
	if len(refs) == 0 {
		return "", ErrNoRaceWinner
	}

	// Validate all refs exist.
	for _, ref := range refs {
		if _, ok := g.Get(ref); !ok {
			return "", wrapUnknown(ref)
		}
	}

	raceCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	type outcome struct {
		result string
		err    error
	}

	ch := make(chan outcome, len(refs))
	var wg sync.WaitGroup

	for _, ref := range refs {
		p, _ := g.Get(ref)
		if !p.trySetRunning() {
			// Already running or terminal — read current result.
			wg.Add(1)
			p := p
			go func() {
				defer wg.Done()
				select {
				case <-p.Done():
				case <-raceCtx.Done():
					ch <- outcome{err: raceCtx.Err()}
					return
				}
				result, err := p.Result()
				ch <- outcome{result: result, err: err}
			}()
			continue
		}
		pLocal := p
		wg.Add(1)
		go func() {
			defer wg.Done()
			result, err := dispatch(raceCtx, pLocal.Tool, pLocal.Args)
			if err != nil {
				pLocal.setFailed(err)
				ch <- outcome{err: err}
			} else {
				pLocal.setDone(result)
				ch <- outcome{result: result}
			}
		}()
	}

	// Close channel when all goroutines finish.
	go func() {
		wg.Wait()
		close(ch)
	}()

	// Return the first successful result; collect all errors as fallback.
	var lastErr error
	received := 0
	for o := range ch {
		received++
		if o.err == nil {
			cancel() // signal losers to stop
			return o.result, nil
		}
		lastErr = o.err
		if received == len(refs) {
			break
		}
	}

	if lastErr != nil {
		return "", lastErr
	}
	return "", ErrNoRaceWinner
}

func wrapUnknown(id string) error {
	return &unknownPlaceholderError{id: id}
}

type unknownPlaceholderError struct {
	id string
}

func (e *unknownPlaceholderError) Error() string {
	return "async: unknown placeholder ID: " + e.id
}

func (e *unknownPlaceholderError) Is(target error) bool {
	return target == ErrUnknownPlaceholder
}
