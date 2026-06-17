package artifact

import "github.com/htekdev/ai-harness/artifactsource"

// SourceSpec describes an external artifact source declaration from config.
type SourceSpec = artifactsource.SourceSpec

// ResolveOptions controls source resolution behavior.
type ResolveOptions = artifactsource.ResolveOptions

// IsPinnedGitRef returns true when a git ref looks immutable enough for config use.
func IsPinnedGitRef(ref string) bool {
	return artifactsource.IsPinnedGitRef(ref)
}

// DefaultCacheDir returns the source cache path.
func DefaultCacheDir() string {
	return artifactsource.DefaultCacheDir()
}

// ResolveSourceRoots resolves each source to a local .harness root directory.
func ResolveSourceRoots(projectDir string, sources []SourceSpec, opts ResolveOptions) ([]string, error) {
	return artifactsource.ResolveSourceRoots(projectDir, sources, opts)
}
