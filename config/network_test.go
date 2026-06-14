package config

import (
	"testing"
)

func TestNetworkConfig_ParsesAllowedDomains(t *testing.T) {
	yamlSrc := []byte(`
model:
  name: gpt-4o
  api_key_env: TEST_KEY
  max_tokens: 1024
network:
  allowed_domains:
    - example.com
    - "*.api.internal"
`)
	t.Setenv("TEST_KEY", "x")

	cfg, err := Parse(yamlSrc)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if cfg.Network == nil {
		t.Fatalf("expected Network block to be parsed")
	}
	want := []string{"example.com", "*.api.internal"}
	if len(cfg.Network.AllowedDomains) != len(want) {
		t.Fatalf("got %v, want %v", cfg.Network.AllowedDomains, want)
	}
	for i := range want {
		if cfg.Network.AllowedDomains[i] != want[i] {
			t.Errorf("entry %d: got %q want %q", i, cfg.Network.AllowedDomains[i], want[i])
		}
	}
}

func TestNetworkConfig_OmittedIsNil(t *testing.T) {
	yamlSrc := []byte(`
model:
  name: gpt-4o
  api_key_env: TEST_KEY
  max_tokens: 1024
`)
	cfg, err := Parse(yamlSrc)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if cfg.Network != nil {
		t.Fatalf("expected Network to be nil when omitted, got %+v", cfg.Network)
	}
}
