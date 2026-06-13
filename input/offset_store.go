package input

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"sync"
)

// OffsetStore persists the last-acknowledged Telegram update_id across process
// restarts. Without it, after a crash or redeploy the bot would re-process any
// updates Telegram still has buffered (up to ~24h) before it eventually rolls
// them off — leading to duplicate work.
//
// Implementations must be safe for concurrent use within a single TelegramSource;
// TelegramSource itself does not call Load/Save concurrently, but tooling around
// it may inspect the store independently.
type OffsetStore interface {
	// Load returns the last persisted offset (== last seen update_id + 1).
	// Returns 0, nil if no offset has been stored yet.
	Load() (int64, error)
	// Save persists offset durably. It must be safe against partial writes
	// (a crash mid-Save must not corrupt a previously stored offset).
	Save(offset int64) error
}

// MemoryOffsetStore is an in-memory OffsetStore for tests and ephemeral runs.
type MemoryOffsetStore struct {
	mu     sync.Mutex
	offset int64
	loaded bool
}

// NewMemoryOffsetStore returns a fresh in-memory store.
func NewMemoryOffsetStore() *MemoryOffsetStore { return &MemoryOffsetStore{} }

// Load returns the stored offset (0 if never set).
func (m *MemoryOffsetStore) Load() (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.offset, nil
}

// Save records the offset.
func (m *MemoryOffsetStore) Save(offset int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.offset = offset
	m.loaded = true
	return nil
}

// FileOffsetStore persists the offset to a JSON file using an atomic
// write-then-rename so a crash mid-Save cannot corrupt the existing value.
//
// The file format is intentionally tiny and forward-compatible:
//
//	{"offset": 12345}
//
// Plain integers are also accepted on Load for back-compat with simpler stores.
type FileOffsetStore struct {
	path string
	mu   sync.Mutex
}

// NewFileOffsetStore returns a FileOffsetStore that reads/writes path.
// The parent directory is created lazily on first Save.
func NewFileOffsetStore(path string) *FileOffsetStore {
	return &FileOffsetStore{path: path}
}

type fileOffsetPayload struct {
	Offset int64 `json:"offset"`
}

// Load reads the persisted offset. Returns 0, nil if the file does not exist.
func (f *FileOffsetStore) Load() (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.path == "" {
		return 0, errors.New("telegram offset store: empty path")
	}
	data, err := os.ReadFile(f.path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return 0, nil
		}
		return 0, fmt.Errorf("telegram offset store: read %s: %w", f.path, err)
	}
	if len(data) == 0 {
		return 0, nil
	}
	// Try JSON first.
	var payload fileOffsetPayload
	if jerr := json.Unmarshal(data, &payload); jerr == nil {
		return payload.Offset, nil
	}
	// Fall back to a plain integer.
	if n, perr := strconv.ParseInt(string(trimSpace(data)), 10, 64); perr == nil {
		return n, nil
	}
	return 0, fmt.Errorf("telegram offset store: %s does not contain a valid offset", f.path)
}

// Save writes the offset atomically: write to a sibling .tmp file, fsync, then rename.
func (f *FileOffsetStore) Save(offset int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.path == "" {
		return errors.New("telegram offset store: empty path")
	}
	dir := filepath.Dir(f.path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("telegram offset store: mkdir %s: %w", dir, err)
	}
	payload, err := json.Marshal(fileOffsetPayload{Offset: offset})
	if err != nil {
		return fmt.Errorf("telegram offset store: marshal: %w", err)
	}
	tmp, err := os.CreateTemp(dir, ".offset-*.tmp")
	if err != nil {
		return fmt.Errorf("telegram offset store: create temp: %w", err)
	}
	tmpPath := tmp.Name()
	cleanup := func() { _ = os.Remove(tmpPath) }
	if _, err := tmp.Write(payload); err != nil {
		_ = tmp.Close()
		cleanup()
		return fmt.Errorf("telegram offset store: write temp: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		cleanup()
		return fmt.Errorf("telegram offset store: fsync temp: %w", err)
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return fmt.Errorf("telegram offset store: close temp: %w", err)
	}
	if err := os.Rename(tmpPath, f.path); err != nil {
		cleanup()
		return fmt.Errorf("telegram offset store: rename %s -> %s: %w", tmpPath, f.path, err)
	}
	return nil
}

// trimSpace is a tiny helper to avoid importing strings in this small file.
func trimSpace(b []byte) []byte {
	i, j := 0, len(b)
	for i < j && isSpace(b[i]) {
		i++
	}
	for j > i && isSpace(b[j-1]) {
		j--
	}
	return b[i:j]
}

func isSpace(c byte) bool {
	return c == ' ' || c == '\t' || c == '\n' || c == '\r'
}
