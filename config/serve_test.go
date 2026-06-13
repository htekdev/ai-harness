package config

import (
	"strings"
	"testing"
)

func TestServeConfig_ParseAndValidate(t *testing.T) {
	tests := []struct {
		name    string
		yaml    string
		wantErr string // substring; "" means must succeed
		check   func(t *testing.T, cfg *Config)
	}{
		{
			name: "no serve block is fine",
			yaml: `
model:
  name: gpt-4o
`,
			check: func(t *testing.T, cfg *Config) {
				if cfg.Serve != nil {
					t.Errorf("expected nil Serve, got %+v", cfg.Serve)
				}
			},
		},
		{
			name: "stdin only",
			yaml: `
model:
  name: gpt-4o
serve:
  sources:
    - type: stdin
`,
			check: func(t *testing.T, cfg *Config) {
				if cfg.Serve == nil || len(cfg.Serve.Sources) != 1 {
					t.Fatalf("expected 1 source, got %+v", cfg.Serve)
				}
				if cfg.Serve.Sources[0].NormalizedType() != "stdin" {
					t.Errorf("got type %q", cfg.Serve.Sources[0].Type)
				}
			},
		},
		{
			name: "telegram fully populated",
			yaml: `
model:
  name: gpt-4o
serve:
  sources:
    - type: stdin
    - type: telegram
      token_env: TELEGRAM_BOT_TOKEN
      poll_timeout_seconds: 25
      offset_path: /var/lib/harness/telegram.offset.json
      chat_allowlist:
        - 7729308746
        - 1001
`,
			check: func(t *testing.T, cfg *Config) {
				if got := len(cfg.Serve.Sources); got != 2 {
					t.Fatalf("want 2 sources, got %d", got)
				}
				tg := cfg.Serve.Sources[1]
				if tg.TokenEnv != "TELEGRAM_BOT_TOKEN" {
					t.Errorf("token_env = %q", tg.TokenEnv)
				}
				if tg.PollTimeoutSeconds != 25 {
					t.Errorf("poll_timeout_seconds = %d", tg.PollTimeoutSeconds)
				}
				if tg.OffsetPath != "/var/lib/harness/telegram.offset.json" {
					t.Errorf("offset_path = %q", tg.OffsetPath)
				}
				if len(tg.ChatAllowlist) != 2 || tg.ChatAllowlist[0] != 7729308746 {
					t.Errorf("chat_allowlist = %v", tg.ChatAllowlist)
				}
			},
		},
		{
			name: "meshwire fully populated",
			yaml: `
model:
  name: gpt-4o
serve:
  sources:
    - type: meshwire
      token_env: MESHWIRE_TOKEN
      mesh_id: family-mesh
      agent_id: harness-bot
      sender_allowlist: [peer-reviewer, ops-bot]
      poll_timeout_seconds: 30
      base_url: https://meshwire.io
`,
			check: func(t *testing.T, cfg *Config) {
				mw := cfg.Serve.Sources[0]
				if mw.MeshID != "family-mesh" || mw.AgentID != "harness-bot" {
					t.Errorf("mesh_id/agent_id mismatch: %+v", mw)
				}
				if len(mw.SenderAllowlist) != 2 {
					t.Errorf("sender_allowlist = %v", mw.SenderAllowlist)
				}
				if mw.BaseURL != "https://meshwire.io" {
					t.Errorf("base_url = %q", mw.BaseURL)
				}
			},
		},
		{
			name: "empty sources list rejected",
			yaml: `
model:
  name: gpt-4o
serve:
  sources: []
`,
			wantErr: "serve.sources must contain at least one entry",
		},
		{
			name: "unknown type rejected",
			yaml: `
model:
  name: gpt-4o
serve:
  sources:
    - type: discord
`,
			wantErr: "is not recognized",
		},
		{
			name: "telegram missing token_env",
			yaml: `
model:
  name: gpt-4o
serve:
  sources:
    - type: telegram
      chat_allowlist: [1]
`,
			wantErr: "token_env is required",
		},
		{
			name: "telegram missing chat_allowlist",
			yaml: `
model:
  name: gpt-4o
serve:
  sources:
    - type: telegram
      token_env: TELEGRAM_BOT_TOKEN
`,
			wantErr: "chat_allowlist must be non-empty",
		},
		{
			name: "telegram poll out of range",
			yaml: `
model:
  name: gpt-4o
serve:
  sources:
    - type: telegram
      token_env: TELEGRAM_BOT_TOKEN
      chat_allowlist: [1]
      poll_timeout_seconds: 90
`,
			wantErr: "poll_timeout_seconds must be between 0 and 50",
		},
		{
			name: "meshwire missing required fields",
			yaml: `
model:
  name: gpt-4o
serve:
  sources:
    - type: meshwire
      token_env: MESHWIRE_TOKEN
`,
			wantErr: "mesh_id is required",
		},
		{
			name: "duplicate types rejected",
			yaml: `
model:
  name: gpt-4o
serve:
  sources:
    - type: stdin
    - type: stdin
`,
			wantErr: `duplicates are not supported`,
		},
		{
			name: "type case-insensitive normalization",
			yaml: `
model:
  name: gpt-4o
serve:
  sources:
    - type: STDIN
`,
			check: func(t *testing.T, cfg *Config) {
				if cfg.Serve.Sources[0].NormalizedType() != "stdin" {
					t.Errorf("expected normalized stdin, got %q", cfg.Serve.Sources[0].NormalizedType())
				}
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg, err := Parse([]byte(tc.yaml))
			if tc.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tc.wantErr)
				}
				if !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("expected error containing %q, got %v", tc.wantErr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tc.check != nil {
				tc.check(t, cfg)
			}
		})
	}
}

func TestServeConfig_NilValidateOK(t *testing.T) {
	var s *ServeConfig
	if err := s.Validate(); err != nil {
		t.Errorf("nil ServeConfig.Validate() should be nil, got %v", err)
	}
}
