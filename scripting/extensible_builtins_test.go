package scripting

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEngine_SetAndTemplateBuiltins(t *testing.T) {
	engine := NewEngine()
	runner, err := engine.CompileToolScript("set_template_tool", `
def run(args):
    merged = set.union(set.new(["alpha", "beta", "alpha"]), ["gamma", "beta"])
    overlap = set.intersect(merged, ["beta", "gamma", "missing"])
    remainder = set.diff(merged, ["beta"])
    rendered = template.render("Owner: {{owner.name}} | First: {{labels.0}} | Count: {{count}}", {
        "owner": {"name": "Ada"},
        "labels": set.values(overlap),
        "count": set.size(remainder),
    })
    return json.encode({
        "contains_gamma": set.contains(merged, "gamma"),
        "merged": set.values(merged),
        "overlap": set.values(overlap),
        "remainder_size": set.size(remainder),
        "rendered": rendered,
    })
`)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	result, err := runner.Run(context.Background(), json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("run: %v", err)
	}

	var payload struct {
		ContainsGamma bool     `json:"contains_gamma"`
		Merged        []string `json:"merged"`
		Overlap       []string `json:"overlap"`
		RemainderSize int      `json:"remainder_size"`
		Rendered      string   `json:"rendered"`
	}
	if err := json.Unmarshal([]byte(result), &payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !payload.ContainsGamma {
		t.Fatal("expected gamma to be present in merged set")
	}
	if got, want := strings.Join(payload.Merged, ","), "alpha,beta,gamma"; got != want {
		t.Fatalf("merged = %q, want %q", got, want)
	}
	if got, want := strings.Join(payload.Overlap, ","), "beta,gamma"; got != want {
		t.Fatalf("overlap = %q, want %q", got, want)
	}
	if payload.RemainderSize != 2 {
		t.Fatalf("remainder_size = %d, want 2", payload.RemainderSize)
	}
	if payload.Rendered != "Owner: Ada | First: beta | Count: 2" {
		t.Fatalf("unexpected rendered template: %q", payload.Rendered)
	}
}

func TestEngine_TemplateRenderMissingValue(t *testing.T) {
	engine := NewEngine()
	runner, err := engine.CompileToolScript("template_error_tool", `
def run(args):
    return template.render("Hello {{missing}}", {"present": "world"})
`)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	_, err = runner.Run(context.Background(), json.RawMessage(`{}`))
	if err == nil || !strings.Contains(err.Error(), `missing value for "missing"`) {
		t.Fatalf("expected missing template value error, got %v", err)
	}
}

func TestEngine_ValidateBuiltins(t *testing.T) {
	engine := NewEngine()
	runner, err := engine.CompileToolScript("validate_tool", `
def run(args):
    return json.encode({
        "valid_email": validate.email(" alice@example.com "),
        "invalid_email": validate.email("Alice <alice@example.com>"),
        "valid_url": validate.url("https://example.com/path?q=1"),
        "invalid_url": validate.url("example.com/no-scheme"),
        "valid_json": validate.json('{"ok":true}'),
        "invalid_json": validate.json('{oops'),
    })
`)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	result, err := runner.Run(context.Background(), json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("run: %v", err)
	}

	var payload map[string]bool
	if err := json.Unmarshal([]byte(result), &payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !payload["valid_email"] || payload["invalid_email"] {
		t.Fatalf("unexpected email validation payload: %+v", payload)
	}
	if !payload["valid_url"] || payload["invalid_url"] {
		t.Fatalf("unexpected URL validation payload: %+v", payload)
	}
	if !payload["valid_json"] || payload["invalid_json"] {
		t.Fatalf("unexpected JSON validation payload: %+v", payload)
	}
}

func TestEngine_FsCopyMoveAndDiffBuiltins(t *testing.T) {
	engine := NewEngine()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	tmp := t.TempDir()
	if err := os.Chdir(tmp); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	defer func() { _ = os.Chdir(cwd) }()

	if err := os.MkdirAll(filepath.Join(tmp, "nested", "sub"), 0o755); err != nil {
		t.Fatalf("mkdir nested: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tmp, "nested", "sub", "data.txt"), []byte("nested-data\n"), 0o644); err != nil {
		t.Fatalf("write nested file: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tmp, "original.txt"), []byte("line1\nline2\n"), 0o644); err != nil {
		t.Fatalf("write original: %v", err)
	}

	runner, err := engine.CompileToolScript("fs_ext_tool", `
def run(args):
    fs.copy("nested", "mirror")
    fs.copy("original.txt", "scratch/copy.txt")
    original = fs.read("original.txt")
    candidate = original + "line3\n"
    preview = fs.diff(original, candidate, "original.txt", "candidate.txt")
    fs.write("scratch/copy.txt", candidate)
    fs.move("scratch/copy.txt", "archive/final.txt")
    return json.encode({
        "mirror_text": fs.read("mirror/sub/data.txt"),
        "moved_exists": fs.exists("archive/final.txt"),
        "old_exists": fs.exists("scratch/copy.txt"),
        "diff": preview,
        "final": fs.read("archive/final.txt"),
    })
`)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	result, err := runner.Run(context.Background(), json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("run: %v", err)
	}

	var payload struct {
		MirrorText  string `json:"mirror_text"`
		MovedExists bool   `json:"moved_exists"`
		OldExists   bool   `json:"old_exists"`
		Diff        string `json:"diff"`
		Final       string `json:"final"`
	}
	if err := json.Unmarshal([]byte(result), &payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if payload.MirrorText != "nested-data\n" {
		t.Fatalf("unexpected mirror text: %q", payload.MirrorText)
	}
	if !payload.MovedExists || payload.OldExists {
		t.Fatalf("unexpected move state: %+v", payload)
	}
	if !strings.Contains(payload.Diff, "--- original.txt") || !strings.Contains(payload.Diff, "+++ candidate.txt") || !strings.Contains(payload.Diff, "+line3") {
		t.Fatalf("unexpected diff output: %q", payload.Diff)
	}
	if payload.Final != "line1\nline2\nline3\n" {
		t.Fatalf("unexpected final file contents: %q", payload.Final)
	}
}
