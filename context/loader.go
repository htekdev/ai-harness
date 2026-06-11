package context

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sync"
)

// Loader loads content from context sources, caching file reads
// across the session to avoid redundant disk I/O.
type Loader struct {
	mu    sync.RWMutex
	cache map[string]string // keyed by absolute file path
}

// NewLoader creates a new Loader with an empty session cache.
func NewLoader() *Loader {
	return &Loader{
		cache: make(map[string]string),
	}
}

// LoadContent reads content for the given context source.
//   - type "file" (or ""): reads path relative to root; caches by absolute path.
//   - type "url":           fetches the URL via HTTP GET; not cached.
//
// Returns ("", error) on failure.
func (l *Loader) LoadContent(source ContextSource, root string) (string, error) {
	switch source.Type {
	case "file", "":
		return l.loadFile(source.Path, root)
	case "url":
		return l.loadURL(source.URL)
	default:
		return "", fmt.Errorf("unsupported context source type %q", source.Type)
	}
}

// loadFile reads a file relative to root, caching the result by absolute path.
func (l *Loader) loadFile(path, root string) (string, error) {
	absPath := path
	if !filepath.IsAbs(absPath) {
		absPath = filepath.Join(root, path)
	}
	absPath = filepath.Clean(absPath)

	l.mu.RLock()
	if cached, ok := l.cache[absPath]; ok {
		l.mu.RUnlock()
		return cached, nil
	}
	l.mu.RUnlock()

	data, err := os.ReadFile(absPath)
	if err != nil {
		return "", fmt.Errorf("read context file %q: %w", absPath, err)
	}
	content := string(data)

	l.mu.Lock()
	l.cache[absPath] = content
	l.mu.Unlock()

	return content, nil
}

// loadURL fetches the given URL via HTTP GET and returns the response body.
func (l *Loader) loadURL(rawURL string) (string, error) {
	if rawURL == "" {
		return "", fmt.Errorf("context source url is empty")
	}
	resp, err := http.Get(rawURL) //nolint:noctx
	if err != nil {
		return "", fmt.Errorf("fetch context url %q: %w", rawURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("fetch context url %q: HTTP %d", rawURL, resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read context url body %q: %w", rawURL, err)
	}
	return string(body), nil
}
