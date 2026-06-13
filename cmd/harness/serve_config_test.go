package main

import (
	"strings"
	"testing"

	"github.com/htekdev/ai-harness/config"
)

// TestResolveSources_CLIBeatsServeConfig verifies precedence:
// when --source flags are present, the serve: config block is ignored.
func TestResolveSources_CLIBeatsServeConfig(t *testing.T) {
	serveCfg := &config.ServeConfig{
		Sources: []config.ServeSourceConfig{
			{Type: "telegram", TokenEnv: "TELEGRAM_BOT_TOKEN", ChatAllowlist: []int64{1}, PollTimeoutSeconds: 25},
		},
	}

	srcs, labels, err := resolveSources([]string{"stdin"}, nil, 25, meshwireCLI{}, serveCfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(srcs) != 1 || labels[0] != "stdin" {
		t.Fatalf("expected stdin from CLI flag, got labels=%v len=%d", labels, len(srcs))
	}
	for _, s := range srcs {
		_ = s.Close()
	}
}

// TestResolveSources_ServeConfigUsedWhenNoFlags verifies that when no --source
// CLI flag is given, the serve: block from harness.md is used.
func TestResolveSources_ServeConfigUsedWhenNoFlags(t *testing.T) {
	serveCfg := &config.ServeConfig{
		Sources: []config.ServeSourceConfig{
			{Type: "stdin"},
		},
	}

	srcs, labels, err := resolveSources(nil, nil, 25, meshwireCLI{}, serveCfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(srcs) != 1 || labels[0] != "stdin" {
		t.Fatalf("expected stdin from serve block, got labels=%v", labels)
	}
	for _, s := range srcs {
		_ = s.Close()
	}
}

// TestResolveSources_DefaultsToStdin verifies the fallback path when neither
// CLI flags nor a serve: config block are present.
func TestResolveSources_DefaultsToStdin(t *testing.T) {
	srcs, labels, err := resolveSources(nil, nil, 25, meshwireCLI{}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(srcs) != 1 || labels[0] != "stdin" {
		t.Fatalf("expected default stdin, got labels=%v", labels)
	}
	for _, s := range srcs {
		_ = s.Close()
	}
}

// TestResolveSources_DefaultsToStdinWhenServeBlockEmpty verifies the fallback
// path when a serve: block is present but has zero sources (shouldn't happen
// after Validate, but guard anyway).
func TestResolveSources_DefaultsToStdinWhenServeBlockEmpty(t *testing.T) {
	srcs, labels, err := resolveSources(nil, nil, 25, meshwireCLI{}, &config.ServeConfig{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(srcs) != 1 || labels[0] != "stdin" {
		t.Fatalf("expected default stdin, got labels=%v", labels)
	}
	for _, s := range srcs {
		_ = s.Close()
	}
}

// TestBuildSourcesFromConfig_TelegramMissingToken surfaces a clear error when
// the env var named by token_env is empty.
func TestBuildSourcesFromConfig_TelegramMissingToken(t *testing.T) {
	t.Setenv("MISSING_TG_TOKEN_ENV", "")
	srcs := []config.ServeSourceConfig{
		{Type: "telegram", TokenEnv: "MISSING_TG_TOKEN_ENV", ChatAllowlist: []int64{1}, PollTimeoutSeconds: 25},
	}
	_, _, err := buildSourcesFromConfig(srcs)
	if err == nil {
		t.Fatal("expected error for empty env var, got nil")
	}
	if !strings.Contains(err.Error(), "MISSING_TG_TOKEN_ENV") {
		t.Errorf("error should reference env var name: %v", err)
	}
}

// TestBuildSourcesFromConfig_MeshWireMissingToken confirms that a meshwire
// entry in harness.md surfaces a clear error when the env var named by
// token_env is empty (now that PR #75 is merged and the source is wired).
func TestBuildSourcesFromConfig_MeshWireMissingToken(t *testing.T) {
	t.Setenv("MISSING_MW_TOKEN_ENV", "")
	srcs := []config.ServeSourceConfig{
		{Type: "meshwire", TokenEnv: "MISSING_MW_TOKEN_ENV", MeshID: "m", AgentID: "a", SenderAllowlist: []string{"peer"}},
	}
	_, _, err := buildSourcesFromConfig(srcs)
	if err == nil {
		t.Fatal("expected meshwire missing-token error, got nil")
	}
	if !strings.Contains(err.Error(), "MISSING_MW_TOKEN_ENV") {
		t.Errorf("error should reference env var name, got: %v", err)
	}
}

// TestBuildSourcesFromConfig_MeshWireWired exercises the meshwire happy path
// now that PR #75 is in main: env-resolved token + required fields produce a
// constructed source with the right label.
func TestBuildSourcesFromConfig_MeshWireWired(t *testing.T) {
	t.Setenv("FAKE_MW_TOKEN", "fake-token")
	srcs := []config.ServeSourceConfig{
		{
			Type:               "meshwire",
			TokenEnv:           "FAKE_MW_TOKEN",
			MeshID:             "family-mesh",
			AgentID:            "harness-bot",
			SenderAllowlist:    []string{"peer-reviewer"},
			PollTimeoutSeconds: 30,
		},
	}
	out, labels, err := buildSourcesFromConfig(srcs)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(out) != 1 || labels[0] != "meshwire" {
		t.Fatalf("expected single meshwire source, got %d sources, labels=%v", len(out), labels)
	}
	_ = out[0].Close()
}

// TestBuildSourcesFromConfig_StdinPlusTelegram exercises the multi-source
// happy path where stdin and telegram are both present with telegram driven
// by env-resolved token + allowlist + offset path.
func TestBuildSourcesFromConfig_StdinPlusTelegram(t *testing.T) {
	t.Setenv("FAKE_TG_TOKEN", "fake-token")
	srcs := []config.ServeSourceConfig{
		{Type: "stdin"},
		{
			Type:               "telegram",
			TokenEnv:           "FAKE_TG_TOKEN",
			ChatAllowlist:      []int64{42},
			PollTimeoutSeconds: 5,
			OffsetPath:         t.TempDir() + "/offset.json",
		},
	}
	out, labels, err := buildSourcesFromConfig(srcs)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(out) != 2 {
		t.Fatalf("expected 2 sources, got %d", len(out))
	}
	if labels[0] != "stdin" || labels[1] != "telegram" {
		t.Errorf("unexpected labels: %v", labels)
	}
	for _, s := range out {
		_ = s.Close()
	}
}
