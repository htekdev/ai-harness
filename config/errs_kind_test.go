package config

import (
	"testing"

	"github.com/htekdev/ai-harness/harness/errs"
)

// TestValidate_KindConfig ensures Config.Validate emits a typed
// KindConfig error so hooks/dashboards can classify it without parsing
// message text.
func TestValidate_KindConfig(t *testing.T) {
	cfg := &Config{}
	applyDefaults(cfg)
	cfg.Model.Temperature = 5 // out of range
	cfg.Model.MaxTokens = 0   // invalid
	cfg.Model.Name = ""       // invalid

	err := cfg.Validate()
	if err == nil {
		t.Fatalf("expected validation error, got nil")
	}
	if got := errs.KindOf(err); got != errs.KindConfig {
		t.Fatalf("expected KindConfig (%v), got %v", errs.KindConfig, got)
	}
}

// TestServeValidate_KindConfig ensures the serve config validator also
// emits typed KindConfig errors.
func TestServeValidate_KindConfig(t *testing.T) {
	s := &ServeConfig{} // empty sources triggers error
	err := s.Validate()
	if err == nil {
		t.Fatalf("expected validation error, got nil")
	}
	if got := errs.KindOf(err); got != errs.KindConfig {
		t.Fatalf("expected KindConfig (%v), got %v", errs.KindConfig, got)
	}
}

// TestLoadAuto_UnsupportedExt_KindConfig ensures unsupported extensions
// surface as KindConfig instead of opaque strings.
func TestLoadAuto_UnsupportedExt_KindConfig(t *testing.T) {
	_, err := LoadAuto("does-not-matter.toml")
	if err == nil {
		t.Fatalf("expected error for unsupported extension, got nil")
	}
	if got := errs.KindOf(err); got != errs.KindConfig {
		t.Fatalf("expected KindConfig (%v), got %v", errs.KindConfig, got)
	}
}
