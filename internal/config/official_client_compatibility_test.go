package config

import (
	"strings"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/officialclient"
)

func TestParseConfigBytesOfficialClientCompatibility(t *testing.T) {
	cfg, err := ParseConfigBytes([]byte(`
claude-api-key:
  - api-key: sk-secret
    official-client-compatibility:
      enabled: true
      profile: claude-desktop-2.1.215-v1
codex-api-key:
  - api-key: sk-codex
    base-url: https://api.openai.com/v1
    official-client-compatibility:
      enabled: false
      profile: codex-desktop-0.145.0-alpha.18-v1
`))
	if err != nil {
		t.Fatalf("ParseConfigBytes() error = %v", err)
	}
	if got := cfg.ClaudeKey[0].OfficialClientCompatibility.Profile; got != "claude-desktop-2.1.215-v1" {
		t.Fatalf("Claude profile = %q", got)
	}
	if got := cfg.CodexKey[0].OfficialClientCompatibility.Profile; got != "codex-desktop-0.145.0-alpha.18-v1" {
		t.Fatalf("Codex profile = %q", got)
	}
}

func TestParseConfigBytesRejectsInvalidOfficialClientCompatibility(t *testing.T) {
	tests := []struct {
		name    string
		yaml    string
		wantErr string
	}{
		{
			name: "profile required",
			yaml: `codex-api-key:
  - api-key: sk-secret
    base-url: https://api.openai.com/v1
    official-client-compatibility:
      enabled: true
`,
			wantErr: "profile is required",
		},
		{
			name: "provider mismatch",
			yaml: `claude-api-key:
  - api-key: sk-secret
    official-client-compatibility:
      enabled: true
      profile: codex-desktop-0.145.0-alpha.18-v1
`,
			wantErr: "provider mismatch",
		},
		{
			name: "tls unsupported",
			yaml: `claude-api-key:
  - api-key: sk-secret
    official-client-compatibility:
      tls-profile: chrome
`,
			wantErr: "TLS profile is unsupported",
		},
		{
			name: "xai isolated",
			yaml: `xai-api-key:
  - api-key: xai-secret
    base-url: https://api.x.ai/v1
    official-client-compatibility:
      enabled: false
`,
			wantErr: "provider is unsupported",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseConfigBytes([]byte(tt.yaml))
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("ParseConfigBytes() error = %v, want containing %q", err, tt.wantErr)
			}
			if strings.Contains(err.Error(), "sk-secret") || strings.Contains(err.Error(), "xai-secret") {
				t.Fatalf("error exposes API key: %v", err)
			}
		})
	}
}

func TestNormalizeOfficialClientCompatibilityAssignsCurrentProfile(t *testing.T) {
	cfg := &Config{CodexKey: []CodexKey{{
		OfficialClientCompatibility: &officialclientCompatibilityEnabled,
	}}}
	if err := cfg.NormalizeOfficialClientCompatibility(true); err != nil {
		t.Fatalf("NormalizeOfficialClientCompatibility() error = %v", err)
	}
	if got := cfg.CodexKey[0].OfficialClientCompatibility.Profile; got != "codex-desktop-0.145.0-alpha.18-v1" {
		t.Fatalf("profile = %q", got)
	}
}

var officialclientCompatibilityEnabled = officialclient.CompatibilityConfig{Enabled: true}
