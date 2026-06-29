package context

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// LoadContent reads the content for a context source.
// For KindFile sources, it reads from the local filesystem (path may be
// relative to baseDir). For KindURL sources, it issues an HTTP GET request.
// Returns the raw string content or an error.
func LoadContent(s Source, baseDir string) (string, error) {
	switch s.Kind {
	case KindFile, "":
		return loadFile(s.Path, baseDir)
	case KindURL:
		return loadURL(s.Path)
	default:
		return "", fmt.Errorf("unknown source kind %q for %q", s.Kind, s.Name)
	}
}

// FileLoader returns a loader function that resolves file paths relative to
// baseDir. This is the standard loader to pass to SourceRegistry.Evaluate and
// SourceRegistry.ActivateTrigger.
func FileLoader(baseDir string) func(Source) (string, error) {
	return func(s Source) (string, error) {
		return LoadContent(s, baseDir)
	}
}

func loadFile(path, baseDir string) (string, error) {
	resolved := path
	if !filepath.IsAbs(resolved) {
		resolved = filepath.Join(baseDir, resolved)
	}
	data, err := os.ReadFile(resolved)
	if err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("context source file not found: %s", resolved)
		}
		return "", fmt.Errorf("read context source file %s: %w", resolved, err)
	}
	return strings.TrimSpace(string(data)), nil
}

func loadURL(rawURL string) (string, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", fmt.Errorf("parse URL %q: %w", rawURL, err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", fmt.Errorf("URL scheme must be http or https, got %q", u.Scheme)
	}

	client := &http.Client{
		Timeout: 15 * time.Second,
		// Validate redirect targets to prevent SSRF: only follow redirects
		// to http/https URLs.
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return fmt.Errorf("too many redirects fetching %s", rawURL)
			}
			if req.URL.Scheme != "http" && req.URL.Scheme != "https" {
				return fmt.Errorf("redirect to non-HTTP scheme %q denied", req.URL.Scheme)
			}
			return nil
		},
	}
	resp, err := client.Get(rawURL)
	if err != nil {
		return "", fmt.Errorf("fetch %s: %w", rawURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("fetch %s: unexpected status %d", rawURL, resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read response body %s: %w", rawURL, err)
	}
	return strings.TrimSpace(string(body)), nil
}
