package artifact

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveSourceRootsLocal(t *testing.T) {
	project := t.TempDir()
	root := filepath.Join(project, ".harness")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	roots, err := ResolveSourceRoots(project, []SourceSpec{{Type: "local", Path: ".harness"}}, ResolveOptions{})
	if err != nil {
		t.Fatalf("ResolveSourceRoots: %v", err)
	}
	if len(roots) != 1 {
		t.Fatalf("expected 1 root, got %d", len(roots))
	}
	if roots[0] != root {
		t.Fatalf("root=%q want %q", roots[0], root)
	}
}

func TestResolveSourceRootsGitTrustedRequired(t *testing.T) {
	project := t.TempDir()
	_, err := ResolveSourceRoots(project, []SourceSpec{{Type: "git", URL: "https://example.com/repo.git", Ref: "v1.0.0"}}, ResolveOptions{})
	if err == nil {
		t.Fatal("expected trust validation error")
	}
}

func TestIsPinnedGitRef(t *testing.T) {
	cases := []struct {
		ref  string
		want bool
	}{
		{ref: "main", want: false},
		{ref: "feature/x", want: false},
		{ref: "v1.2.3", want: true},
		{ref: "9fceb02", want: true},
	}
	for _, tc := range cases {
		if got := IsPinnedGitRef(tc.ref); got != tc.want {
			t.Fatalf("IsPinnedGitRef(%q)=%v want %v", tc.ref, got, tc.want)
		}
	}
}
