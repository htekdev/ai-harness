package delegation

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/htekdev/ai-harness/tools"
)

// TaskStatus represents the lifecycle state of an async delegate task.
type TaskStatus string

const (
	TaskStatusRunning   TaskStatus = "running"
	TaskStatusCompleted TaskStatus = "completed"
	TaskStatusFailed    TaskStatus = "failed"
)

// TaskEntry holds the state of an async delegate execution.
type TaskEntry struct {
	ID        string
	Status    TaskStatus
	Result    *Result
	Error     error
	CreatedAt time.Time
	DoneAt    time.Time
	done      chan struct{}
}

// TaskStore manages async delegate tasks with concurrency limits and TTL cleanup.
type TaskStore struct {
	mu         sync.RWMutex
	tasks      map[string]*TaskEntry
	maxConc    int
	semaphore  chan struct{}
	ttl        time.Duration
	stopReaper context.CancelFunc
}

// NewTaskStore creates a task store with the given concurrency limit and result TTL.
func NewTaskStore(maxConcurrent int, resultTTL time.Duration) *TaskStore {
	if maxConcurrent <= 0 {
		maxConcurrent = 5
	}
	if resultTTL <= 0 {
		resultTTL = 5 * time.Minute
	}

	ctx, cancel := context.WithCancel(context.Background())
	ts := &TaskStore{
		tasks:      make(map[string]*TaskEntry),
		maxConc:    maxConcurrent,
		semaphore:  make(chan struct{}, maxConcurrent),
		ttl:        resultTTL,
		stopReaper: cancel,
	}

	go ts.reaper(ctx)
	return ts
}

// Submit adds a new task in running state and returns its entry.
// Blocks if max concurrent tasks are already running.
func (ts *TaskStore) Submit(id string) (*TaskEntry, error) {
	// Acquire semaphore slot (blocks if at capacity)
	ts.semaphore <- struct{}{}

	entry := &TaskEntry{
		ID:        id,
		Status:    TaskStatusRunning,
		CreatedAt: time.Now(),
		done:      make(chan struct{}),
	}

	ts.mu.Lock()
	ts.tasks[id] = entry
	ts.mu.Unlock()

	return entry, nil
}

// Complete marks a task as completed with the given result.
func (ts *TaskStore) Complete(id string, result *Result) {
	ts.mu.Lock()
	entry, ok := ts.tasks[id]
	ts.mu.Unlock()

	if !ok {
		return
	}

	entry.Status = TaskStatusCompleted
	entry.Result = result
	entry.DoneAt = time.Now()
	close(entry.done)

	// Release semaphore
	<-ts.semaphore
}

// Fail marks a task as failed with the given error.
func (ts *TaskStore) Fail(id string, err error) {
	ts.mu.Lock()
	entry, ok := ts.tasks[id]
	ts.mu.Unlock()

	if !ok {
		return
	}

	entry.Status = TaskStatusFailed
	entry.Error = err
	entry.DoneAt = time.Now()
	close(entry.done)

	// Release semaphore
	<-ts.semaphore
}

// Get retrieves a task entry by ID.
func (ts *TaskStore) Get(id string) (*TaskEntry, bool) {
	ts.mu.RLock()
	defer ts.mu.RUnlock()
	entry, ok := ts.tasks[id]
	return entry, ok
}

// Wait blocks until the task with the given ID completes or the context is cancelled.
func (ts *TaskStore) Wait(ctx context.Context, id string) (*TaskEntry, error) {
	ts.mu.RLock()
	entry, ok := ts.tasks[id]
	ts.mu.RUnlock()

	if !ok {
		return nil, fmt.Errorf("task %q not found", id)
	}

	select {
	case <-entry.done:
		return entry, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// WaitMultiple blocks until all specified tasks complete.
func (ts *TaskStore) WaitMultiple(ctx context.Context, ids []string) ([]*TaskEntry, error) {
	results := make([]*TaskEntry, len(ids))
	for i, id := range ids {
		entry, err := ts.Wait(ctx, id)
		if err != nil {
			return results, err
		}
		results[i] = entry
	}
	return results, nil
}

// Close stops the reaper and cleans up.
func (ts *TaskStore) Close() {
	ts.stopReaper()
}

// reaper periodically cleans up completed tasks older than TTL.
func (ts *TaskStore) reaper(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			ts.cleanup()
		}
	}
}

func (ts *TaskStore) cleanup() {
	ts.mu.Lock()
	defer ts.mu.Unlock()

	cutoff := time.Now().Add(-ts.ttl)
	for id, entry := range ts.tasks {
		if entry.Status != TaskStatusRunning && entry.DoneAt.Before(cutoff) {
			delete(ts.tasks, id)
		}
	}
}

// AsyncDelegateToolDefinitions returns tool definitions for async delegation.
func AsyncDelegateToolDefinitions() []tools.Definition {
	return []tools.Definition{
		{
			Name:        "delegate_async",
			Description: "Spin up a sub-agent asynchronously. Returns a task_id immediately while the delegate runs in the background.",
			Parameters: []tools.Parameter{
				{Name: "task", Type: tools.TypeString, Description: "What you want the delegate to accomplish", Required: true},
				{Name: "agent", Type: tools.TypeString, Description: "Name of a custom agent to use (optional)", Required: false},
				{Name: "model", Type: tools.TypeString, Description: "Override model for this delegate (optional)", Required: false},
				{
					Name:        "tools",
					Type:        tools.TypeArray,
					Description: "Array of tool definitions (required if no agent specified)",
					Required:    false,
					Items: &tools.ParameterSchema{
						Type: tools.TypeObject,
						Properties: map[string]*tools.ParameterSchema{
							"name":        {Type: tools.TypeString, Description: "Tool name"},
							"description": {Type: tools.TypeString, Description: "What the tool does"},
							"parameters":  {Type: tools.TypeObject, Description: "Tool parameters"},
							"script":      {Type: tools.TypeString, Description: "Starlark script"},
						},
						Required: []string{"name", "script"},
					},
				},
				{
					Name:        "hooks",
					Type:        tools.TypeArray,
					Description: "Array of hook definitions",
					Required:    false,
					Items: &tools.ParameterSchema{
						Type: tools.TypeObject,
						Properties: map[string]*tools.ParameterSchema{
							"event":   {Type: tools.TypeString, Description: "Hook event"},
							"handler": {Type: tools.TypeString, Description: "Handler name"},
							"script":  {Type: tools.TypeString, Description: "Starlark script"},
						},
						Required: []string{"event", "handler", "script"},
					},
				},
			},
		},
		{
			Name:        "delegate_status",
			Description: "Check the status of an async delegate task.",
			Parameters: []tools.Parameter{
				{Name: "task_id", Type: tools.TypeString, Description: "The task ID returned by delegate_async", Required: true},
			},
		},
		{
			Name:        "delegate_result",
			Description: "Get the result of a completed async delegate task.",
			Parameters: []tools.Parameter{
				{Name: "task_id", Type: tools.TypeString, Description: "The task ID to get results for", Required: true},
			},
		},
		{
			Name:        "delegate_await",
			Description: "Wait for one or more async delegate tasks to complete and return their results.",
			Parameters: []tools.Parameter{
				{Name: "task_ids", Type: tools.TypeString, Description: "Comma-separated task IDs to wait for", Required: true},
			},
		},
	}
}
