package main

import (
	"os"
	"testing"

	"github.com/htekdev/ai-harness/config"
	"github.com/htekdev/ai-harness/harness/errs"
)

// Phase 5.3 PR-C: cmd/harness/serve must classify config / source build
// failures as KindConfig so retry policies and operator dashboards can
// distinguish "operator misconfigured serve" from a runtime / network
// failure.

func TestServeBuildSources_UnknownSourceIsKindConfig(t *testing.T) {
	t.Parallel()
	_, err := buildSources([]string{"not-a-real-source"}, nil, 25,
		"", "", nil, 30, "")
	if err == nil {
		t.Fatal("expected error for unknown source")
	}
	if errs.KindOf(err) != errs.KindConfig {
		t.Fatalf("expected KindConfig, got %s (err=%v)", errs.KindOf(err), err)
	}
}

func TestServeBuildSources_TelegramMissingTokenIsKindConfig(t *testing.T) {
	t.Setenv("TELEGRAM_BOT_TOKEN", "")
	_, err := buildSources([]string{"telegram"}, []string{"123"}, 25,
		"", "", nil, 30, "")
	if err == nil {
		t.Fatal("expected error for missing token env")
	}
	if errs.KindOf(err) != errs.KindConfig {
		t.Fatalf("expected KindConfig, got %s (err=%v)", errs.KindOf(err), err)
	}
}

func TestServeBuildSources_MeshwireMissingMeshIsKindConfig(t *testing.T) {
	t.Setenv("MESHWIRE_TOKEN", "mw_test")
	defer os.Unsetenv("MESHWIRE_TOKEN")
	_, err := buildSources([]string{"meshwire"}, nil, 25,
		"", "agent-1", []string{"peer-1"}, 30, "")
	if err == nil {
		t.Fatal("expected error for missing mesh id")
	}
	if errs.KindOf(err) != errs.KindConfig {
		t.Fatalf("expected KindConfig, got %s (err=%v)", errs.KindOf(err), err)
	}
}

func TestServeBuildSourcesFromConfig_UnknownTypeIsKindConfig(t *testing.T) {
	t.Parallel()
	_, _, err := buildSourcesFromConfig([]config.ServeSourceConfig{
		{Type: "not-real"},
	})
	if err == nil {
		t.Fatal("expected error for unknown type")
	}
	if errs.KindOf(err) != errs.KindConfig {
		t.Fatalf("expected KindConfig, got %s (err=%v)", errs.KindOf(err), err)
	}
}
