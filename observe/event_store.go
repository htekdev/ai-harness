package observe

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Event is the canonical envelope used for runtime persistence and replay.
type Event struct {
	ID       string         `json:"id"`
	Stream   string         `json:"stream"`
	Type     string         `json:"type"`
	Source   string         `json:"source"`
	Time     time.Time      `json:"time"`
	Sequence int            `json:"sequence"`
	Data     map[string]any `json:"data,omitempty"`
	Meta     map[string]any `json:"meta,omitempty"`
}

// Store is an append-only event store with replay and in-process subscriptions.
//
// Sequence ordering is globally deterministic per store:
// every successful Append increments Sequence by exactly one.
//
// Replay semantics:
//   - from is inclusive and references Sequence
//   - from <= 0 replays from the first event
//   - stream == "" replays all streams
//
// Subscription semantics:
//   - stream == "" subscribes to all streams
//   - publish is non-blocking
//   - if a subscriber buffer is full, the new event is dropped for that subscriber
type Store struct {
	mu       sync.RWMutex
	nextSeq  int
	all      []Event
	byStream map[string][]Event
	subs     map[string]map[int]chan Event
	nextSub  int
	filePath string
}

// NewStore creates an in-memory append-only event store.
func NewStore() *Store {
	return &Store{
		byStream: make(map[string][]Event),
		subs:     make(map[string]map[int]chan Event),
	}
}

// NewFileStore creates a file-backed store that persists events as JSONL.
// Existing events are loaded on startup.
func NewFileStore(path string) (*Store, error) {
	if path == "" {
		return nil, fmt.Errorf("path is required")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	store := NewStore()
	store.filePath = path
	if err := store.loadFromFile(); err != nil {
		return nil, err
	}
	return store, nil
}

// Append validates, persists (if file-backed), and appends an event.
func (s *Store) Append(e Event) (Event, error) {
	if err := validateEventInput(e); err != nil {
		return Event{}, err
	}

	stored := cloneEvent(e)
	if stored.Time.IsZero() {
		stored.Time = time.Now().UTC()
	} else {
		stored.Time = stored.Time.UTC()
	}

	s.mu.Lock()
	stored.Sequence = s.nextSeq + 1
	if stored.ID == "" {
		stored.ID = fmt.Sprintf("evt_%d", stored.Sequence)
	}

	if s.filePath != "" {
		if err := appendJSONL(s.filePath, stored); err != nil {
			s.mu.Unlock()
			return Event{}, err
		}
	}

	s.nextSeq = stored.Sequence
	s.all = append(s.all, stored)
	s.byStream[stored.Stream] = append(s.byStream[stored.Stream], stored)
	streamSubs := copySubscribers(s.subs[stored.Stream])
	allSubs := copySubscribers(s.subs[""])
	s.mu.Unlock()

	publishNonBlocking(streamSubs, stored)
	publishNonBlocking(allSubs, stored)
	return cloneEvent(stored), nil
}

// Replay returns events in deterministic sequence order.
func (s *Store) Replay(stream string, from, limit int) []Event {
	if from <= 0 {
		from = 1
	}
	s.mu.RLock()
	defer s.mu.RUnlock()

	src := s.all
	if stream != "" {
		src = s.byStream[stream]
	}

	out := make([]Event, 0, len(src))
	for _, e := range src {
		if e.Sequence < from {
			continue
		}
		out = append(out, cloneEvent(e))
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out
}

// Subscribe creates an in-process subscription.
func (s *Store) Subscribe(stream string, buffer int) (<-chan Event, func()) {
	if buffer <= 0 {
		buffer = 32
	}
	ch := make(chan Event, buffer)

	s.mu.Lock()
	s.nextSub++
	id := s.nextSub
	if s.subs[stream] == nil {
		s.subs[stream] = map[int]chan Event{}
	}
	s.subs[stream][id] = ch
	s.mu.Unlock()

	var once sync.Once
	unsub := func() {
		once.Do(func() {
			s.mu.Lock()
			defer s.mu.Unlock()
			subMap := s.subs[stream]
			if subMap == nil {
				return
			}
			sub, ok := subMap[id]
			if !ok {
				return
			}
			delete(subMap, id)
			close(sub)
			if len(subMap) == 0 {
				delete(s.subs, stream)
			}
		})
	}
	return ch, unsub
}

// Count returns the number of events in the store.
func (s *Store) Count() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.all)
}

// AsJSONL renders events into JSONL (one JSON object per line).
func AsJSONL(events []Event) (string, error) {
	if len(events) == 0 {
		return "", nil
	}
	buf := make([]byte, 0, len(events)*128)
	for _, e := range events {
		line, err := json.Marshal(e)
		if err != nil {
			return "", err
		}
		buf = append(buf, line...)
		buf = append(buf, '\n')
	}
	return string(buf), nil
}

func (s *Store) loadFromFile() error {
	file, err := os.OpenFile(s.filePath, os.O_RDONLY|os.O_CREATE, 0o644)
	if err != nil {
		return err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 1024), 1024*1024)
	lastSeq := 0

	for line := 1; scanner.Scan(); line++ {
		raw := scanner.Bytes()
		if len(raw) == 0 {
			continue
		}
		var e Event
		if err := json.Unmarshal(raw, &e); err != nil {
			return fmt.Errorf("decode %s line %d: %w", s.filePath, line, err)
		}
		if err := validateLoadedEvent(e, lastSeq); err != nil {
			return fmt.Errorf("invalid %s line %d: %w", s.filePath, line, err)
		}
		e.Time = e.Time.UTC()
		e = cloneEvent(e)
		lastSeq = e.Sequence
		s.all = append(s.all, e)
		s.byStream[e.Stream] = append(s.byStream[e.Stream], e)
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	s.nextSeq = lastSeq
	return nil
}

func appendJSONL(path string, e Event) error {
	file, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer file.Close()

	line, err := json.Marshal(e)
	if err != nil {
		return err
	}
	if _, err := file.Write(append(line, '\n')); err != nil {
		return err
	}
	return nil
}

func validateEventInput(e Event) error {
	if e.Stream == "" {
		return fmt.Errorf("stream is required")
	}
	if e.Type == "" {
		return fmt.Errorf("type is required")
	}
	if e.Source == "" {
		return fmt.Errorf("source is required")
	}
	return nil
}

func validateLoadedEvent(e Event, lastSeq int) error {
	if err := validateEventInput(e); err != nil {
		return err
	}
	if e.ID == "" {
		return fmt.Errorf("id is required")
	}
	if e.Time.IsZero() {
		return fmt.Errorf("time is required")
	}
	if e.Sequence <= 0 {
		return fmt.Errorf("sequence must be > 0")
	}
	if e.Sequence <= lastSeq {
		return fmt.Errorf("sequence must increase monotonically")
	}
	return nil
}

func cloneEvent(e Event) Event {
	e.Data = cloneMap(e.Data)
	e.Meta = cloneMap(e.Meta)
	return e
}

func cloneMap(in map[string]any) map[string]any {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func copySubscribers(m map[int]chan Event) []chan Event {
	if len(m) == 0 {
		return nil
	}
	out := make([]chan Event, 0, len(m))
	for _, ch := range m {
		out = append(out, ch)
	}
	return out
}

func publishNonBlocking(subs []chan Event, e Event) {
	for _, ch := range subs {
		select {
		case ch <- cloneEvent(e):
		default:
			// drop by design under subscriber backpressure
		}
	}
}
