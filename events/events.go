package events

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Event is the durable fact emitted by the harness runtime.
type Event struct {
	ID       string            `json:"id"`
	Stream   string            `json:"stream"`
	Type     string            `json:"type"`
	Source   string            `json:"source,omitempty"`
	Time     time.Time         `json:"time"`
	Sequence uint64            `json:"sequence"`
	Data     json.RawMessage   `json:"data,omitempty"`
	Meta     map[string]string `json:"meta,omitempty"`
}

// Store is the minimal contract needed to persist and replay long-running facts.
type Store interface {
	Append(ctx context.Context, event Event) (Event, error)
	Replay(ctx context.Context, stream string, fn func(Event) error) error
	Subscribe(ctx context.Context, stream string) (<-chan Event, func())
}

const maxEventBytes = 8 * 1024 * 1024

type subscription struct {
	id     int
	stream string
	ctx    context.Context
	ch     chan Event
}

// FileStore persists events as JSONL while supporting in-process subscriptions.
type FileStore struct {
	path          string
	mu            sync.RWMutex
	nextSequence  uint64
	nextSubID     int
	subscriptions map[int]*subscription
}

// OpenFileStore opens or creates a JSONL-backed event store.
func OpenFileStore(path string) (*FileStore, error) {
	if path == "" {
		return nil, fmt.Errorf("event store path is required")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("create event store directory: %w", err)
	}
	file, err := os.OpenFile(path, os.O_CREATE, 0o644)
	if err != nil {
		return nil, fmt.Errorf("open event store: %w", err)
	}
	_ = file.Close()

	store := &FileStore{
		path:          path,
		subscriptions: make(map[int]*subscription),
	}
	if err := store.initializeSequence(); err != nil {
		return nil, err
	}
	return store, nil
}

// Append writes an event to the store, assigns sequence metadata, and notifies subscribers.
func (s *FileStore) Append(ctx context.Context, event Event) (Event, error) {
	if err := validateEvent(event); err != nil {
		return Event{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	s.nextSequence++
	persisted := event
	persisted.Sequence = s.nextSequence
	if persisted.Time.IsZero() {
		persisted.Time = time.Now().UTC()
	} else {
		persisted.Time = persisted.Time.UTC()
	}
	if persisted.ID == "" {
		persisted.ID = fmt.Sprintf("%d-%d", persisted.Time.UnixNano(), persisted.Sequence)
	}

	file, err := os.OpenFile(s.path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return Event{}, fmt.Errorf("open event store for append: %w", err)
	}
	defer file.Close()

	encoded, err := json.Marshal(persisted)
	if err != nil {
		return Event{}, fmt.Errorf("encode event: %w", err)
	}
	if _, err := file.Write(append(encoded, '\n')); err != nil {
		return Event{}, fmt.Errorf("append event: %w", err)
	}

	s.broadcastLocked(ctx, persisted)
	return persisted, nil
}

// Replay iterates over persisted events in order. If stream is empty, all events are replayed.
func (s *FileStore) Replay(ctx context.Context, stream string, fn func(Event) error) error {
	file, err := os.Open(s.path)
	if err != nil {
		return fmt.Errorf("open event store for replay: %w", err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64*1024), maxEventBytes)
	for scanner.Scan() {
		if err := ctx.Err(); err != nil {
			return err
		}
		var event Event
		if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
			return fmt.Errorf("decode event: %w", err)
		}
		if stream != "" && event.Stream != stream {
			continue
		}
		if err := fn(event); err != nil {
			return err
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("scan event store: %w", err)
	}
	return nil
}

// Subscribe returns an in-process event stream for a specific stream or for all streams when empty.
func (s *FileStore) Subscribe(ctx context.Context, stream string) (<-chan Event, func()) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.nextSubID++
	sub := &subscription{
		id:     s.nextSubID,
		stream: stream,
		ctx:    ctx,
		ch:     make(chan Event, 32),
	}
	s.subscriptions[sub.id] = sub

	closeFn := func() {
		s.removeSubscription(sub.id)
	}

	go func() {
		<-ctx.Done()
		closeFn()
	}()

	return sub.ch, closeFn
}

func (s *FileStore) initializeSequence() error {
	file, err := os.Open(s.path)
	if err != nil {
		return fmt.Errorf("open event store for init: %w", err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64*1024), maxEventBytes)
	var maxSequence uint64
	for scanner.Scan() {
		var event Event
		if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
			return fmt.Errorf("decode event during init: %w", err)
		}
		if event.Sequence > maxSequence {
			maxSequence = event.Sequence
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("scan event store during init: %w", err)
	}
	s.nextSequence = maxSequence
	return nil
}

func (s *FileStore) broadcastLocked(ctx context.Context, event Event) {
	for id, sub := range s.subscriptions {
		if sub.stream != "" && sub.stream != event.Stream {
			continue
		}
		select {
		case <-sub.ctx.Done():
			close(sub.ch)
			delete(s.subscriptions, id)
		case <-ctx.Done():
			return
		case sub.ch <- event:
		default:
			close(sub.ch)
			delete(s.subscriptions, id)
		}
	}
}

func (s *FileStore) removeSubscription(id int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sub, ok := s.subscriptions[id]
	if !ok {
		return
	}
	close(sub.ch)
	delete(s.subscriptions, id)
}

func validateEvent(event Event) error {
	if event.Stream == "" {
		return fmt.Errorf("event stream is required")
	}
	if event.Type == "" {
		return fmt.Errorf("event type is required")
	}
	return nil
}
