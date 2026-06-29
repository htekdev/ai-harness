package artifactsource

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// SourceSpec describes an external artifact source declaration from config.
type SourceSpec struct {
	Type     string `yaml:"type" json:"type"`
	Path     string `yaml:"path,omitempty" json:"path,omitempty"`
	URL      string `yaml:"url,omitempty" json:"url,omitempty"`
	Ref      string `yaml:"ref,omitempty" json:"ref,omitempty"`
	Checksum string `yaml:"checksum,omitempty" json:"checksum,omitempty"`
}

// ResolveOptions controls source resolution behavior.
type ResolveOptions struct {
	TrustedSources []string
	CacheDir       string
	Offline        bool
	Refresh        bool
}

var commitSHARe = regexp.MustCompile(`^[a-fA-F0-9]{7,40}$`)

// IsPinnedGitRef returns true when a git ref looks immutable enough for config use.
// Commit SHAs are always accepted. Common mutable branch names are rejected.
func IsPinnedGitRef(ref string) bool {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return false
	}
	if commitSHARe.MatchString(ref) {
		return true
	}
	switch strings.ToLower(ref) {
	case "main", "master", "develop", "development", "dev", "trunk", "head":
		return false
	}
	if strings.Contains(ref, "/") {
		return false
	}
	return true
}

// DefaultCacheDir returns the source cache path.
func DefaultCacheDir() string {
	if xdg := strings.TrimSpace(os.Getenv("XDG_CACHE_HOME")); xdg != "" {
		return filepath.Join(xdg, "ai-harness", "sources")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".", ".cache", "ai-harness", "sources")
	}
	return filepath.Join(home, ".cache", "ai-harness", "sources")
}

// ResolveSourceRoots resolves each source to a local .harness root directory.
func ResolveSourceRoots(projectDir string, sources []SourceSpec, opts ResolveOptions) ([]string, error) {
	if len(sources) == 0 {
		return []string{filepath.Join(projectDir, ".harness")}, nil
	}

	cacheDir := opts.CacheDir
	if strings.TrimSpace(cacheDir) == "" {
		cacheDir = DefaultCacheDir()
	}
	trusted := make(map[string]struct{}, len(opts.TrustedSources))
	for _, src := range opts.TrustedSources {
		trusted[strings.TrimSpace(src)] = struct{}{}
	}

	roots := make([]string, 0, len(sources)+1)
	seen := map[string]struct{}{}
	addRoot := func(root string) {
		clean := filepath.Clean(root)
		if _, ok := seen[clean]; ok {
			return
		}
		seen[clean] = struct{}{}
		roots = append(roots, clean)
	}

	for _, source := range sources {
		typeName := strings.ToLower(strings.TrimSpace(source.Type))
		switch typeName {
		case "local":
			if strings.TrimSpace(source.Path) == "" {
				return nil, fmt.Errorf("artifact_sources local source requires path")
			}
			root := source.Path
			if !filepath.IsAbs(root) {
				root = filepath.Join(projectDir, root)
			}
			if err := verifyChecksum(root, source.Checksum); err != nil {
				return nil, err
			}
			addRoot(root)
		case "git":
			if strings.TrimSpace(source.URL) == "" {
				return nil, fmt.Errorf("artifact_sources git source requires url")
			}
			if _, ok := trusted[strings.TrimSpace(source.URL)]; !ok {
				return nil, fmt.Errorf("artifact source %q is not trusted; add it to trusted_sources", source.URL)
			}
			repoRoot, err := fetchGitSource(cacheDir, source, opts)
			if err != nil {
				return nil, err
			}
			if err := verifyChecksum(repoRoot, source.Checksum); err != nil {
				return nil, err
			}
			if strings.TrimSpace(source.Path) != "" {
				repoRoot = filepath.Join(repoRoot, source.Path)
			}
			addRoot(repoRoot)
		default:
			return nil, fmt.Errorf("unsupported artifact source type %q", source.Type)
		}
	}

	return roots, nil
}

func fetchGitSource(cacheDir string, source SourceSpec, opts ResolveOptions) (string, error) {
	if strings.TrimSpace(source.Ref) == "" {
		return "", fmt.Errorf("artifact source %q requires ref", source.URL)
	}
	if !IsPinnedGitRef(source.Ref) {
		return "", fmt.Errorf("artifact source %q uses mutable ref %q; use a tag or commit SHA", source.URL, source.Ref)
	}

	key := strings.ToLower(strings.TrimSpace(source.Type)) + "\n" + strings.TrimSpace(source.URL) + "\n" + strings.TrimSpace(source.Ref)
	hash := sha256.Sum256([]byte(key))
	dir := filepath.Join(cacheDir, hex.EncodeToString(hash[:]))
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		return "", fmt.Errorf("create source cache dir: %w", err)
	}

	if opts.Offline {
		if st, err := os.Stat(filepath.Join(dir, ".git")); err == nil && st.IsDir() {
			return dir, nil
		}
		return "", fmt.Errorf("offline mode: cache miss for source %q @ %q", source.URL, source.Ref)
	}

	if opts.Refresh {
		_ = os.RemoveAll(dir)
	}

	if _, err := os.Stat(filepath.Join(dir, ".git")); os.IsNotExist(err) {
		tmp := dir + ".tmp"
		_ = os.RemoveAll(tmp)
		if err := runCmd("", "git", "clone", source.URL, tmp); err != nil {
			return "", fmt.Errorf("clone source %q: %w", source.URL, err)
		}
		if err := os.Rename(tmp, dir); err != nil {
			_ = os.RemoveAll(tmp)
			return "", fmt.Errorf("finalize source cache %q: %w", dir, err)
		}
	} else if err == nil {
		if err := runCmd(dir, "git", "fetch", "--all", "--tags", "--prune"); err != nil {
			return "", fmt.Errorf("fetch source %q: %w", source.URL, err)
		}
	}

	if err := runCmd(dir, "git", "checkout", "--detach", source.Ref); err != nil {
		return "", fmt.Errorf("checkout %q in %q: %w", source.Ref, source.URL, err)
	}

	return dir, nil
}

func runCmd(dir, name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s %s: %w (%s)", name, strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return nil
}

func verifyChecksum(root, checksum string) error {
	checksum = strings.TrimSpace(checksum)
	if checksum == "" {
		return nil
	}
	if !strings.HasPrefix(checksum, "sha256:") {
		return fmt.Errorf("unsupported checksum format %q", checksum)
	}
	want := strings.TrimPrefix(checksum, "sha256:")
	got, err := hashDir(root)
	if err != nil {
		return fmt.Errorf("checksum source %q: %w", root, err)
	}
	if !strings.EqualFold(want, got) {
		return fmt.Errorf("checksum mismatch for %q: want sha256:%s got sha256:%s", root, want, got)
	}
	return nil
}

func hashDir(root string) (string, error) {
	h := sha256.New()
	paths := make([]string, 0)
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		if strings.HasPrefix(rel, ".git") {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			return nil
		}
		paths = append(paths, rel)
		return nil
	})
	if err != nil {
		return "", err
	}
	sort.Strings(paths)
	for _, rel := range paths {
		if _, err := io.WriteString(h, rel+"\n"); err != nil {
			return "", err
		}
		f, err := os.Open(filepath.Join(root, rel))
		if err != nil {
			return "", err
		}
		_, copyErr := io.Copy(h, f)
		closeErr := f.Close()
		if copyErr != nil {
			return "", copyErr
		}
		if closeErr != nil {
			return "", closeErr
		}
		if _, err := io.WriteString(h, "\n"); err != nil {
			return "", err
		}
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
