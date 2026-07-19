package helps

import (
	"errors"
	"net/http"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/officialclient"
	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/auth"
)

func TestResolveOfficialClientCompatibility(t *testing.T) {
	encoded, err := officialclient.EncodeCompatibility(&officialclient.CompatibilityConfig{
		Enabled: true,
		Profile: "codex-desktop-0.145.0-alpha.18-v1",
	})
	if err != nil {
		t.Fatalf("EncodeCompatibility() error = %v", err)
	}
	auth := &cliproxyauth.Auth{
		Provider: "codex",
		Attributes: map[string]string{
			"auth_kind":                 "apikey",
			officialclient.AttributeKey: encoded,
		},
	}

	decision, err := ResolveOfficialClientCompatibility(auth, http.Header{"Originator": {"Codex Desktop"}}, false)
	if err != nil {
		t.Fatalf("ResolveOfficialClientCompatibility() error = %v", err)
	}
	if decision.State != officialclient.DecisionBypass {
		t.Fatalf("state = %q, want %q", decision.State, officialclient.DecisionBypass)
	}

	decision, err = ResolveOfficialClientCompatibility(auth, http.Header{"Originator": {"Codex Desktop"}}, true)
	if err != nil {
		t.Fatalf("ResolveOfficialClientCompatibility(connectivity) error = %v", err)
	}
	if decision.State != officialclient.DecisionApply {
		t.Fatalf("connectivity state = %q, want %q", decision.State, officialclient.DecisionApply)
	}
}

func TestResolveOfficialClientCompatibilityFailsClosed(t *testing.T) {
	tests := []struct {
		name    string
		auth    *cliproxyauth.Auth
		wantErr error
	}{
		{name: "nil auth", wantErr: officialclient.ErrInvalidAttributes},
		{
			name: "malformed JSON",
			auth: &cliproxyauth.Auth{Provider: "claude", Attributes: map[string]string{
				"auth_kind":                 "apikey",
				officialclient.AttributeKey: `{`,
			}},
			wantErr: officialclient.ErrInvalidAttributes,
		},
		{
			name: "oauth account",
			auth: &cliproxyauth.Auth{Provider: "claude", Attributes: map[string]string{
				"auth_kind":                 "oauth",
				officialclient.AttributeKey: `{"enabled":true,"profile":"claude-desktop-2.1.215-v1","tls-profile":""}`,
			}},
			wantErr: officialclient.ErrAPIKeyRequired,
		},
		{
			name: "xai provider",
			auth: &cliproxyauth.Auth{Provider: "xai", Attributes: map[string]string{
				"auth_kind":                 "apikey",
				officialclient.AttributeKey: `{"enabled":false,"profile":"","tls-profile":""}`,
			}},
			wantErr: officialclient.ErrUnsupportedProvider,
		},
		{
			name: "unknown profile",
			auth: &cliproxyauth.Auth{Provider: "codex", Attributes: map[string]string{
				"auth_kind":                 "apikey",
				officialclient.AttributeKey: `{"enabled":true,"profile":"unknown","tls-profile":""}`,
			}},
			wantErr: officialclient.ErrUnknownProfile,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ResolveOfficialClientCompatibility(tt.auth, nil, false)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("error = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

func TestFinalizeOfficialClientHeaders(t *testing.T) {
	headers := http.Header{
		"user-agent": {"account-value"},
		"User-Agent": {"client-value"},
		"X-Custom":   {"keep"},
	}
	FinalizeOfficialClientHeaders(headers, "claude", map[string]string{
		"User-Agent": "claude-cli/2.1.215",
		"X-Custom":   "ignored",
	})
	if got := headers.Values("User-Agent"); len(got) != 1 || got[0] != "claude-cli/2.1.215" {
		t.Fatalf("User-Agent values = %#v", got)
	}
	if got := headers.Get("X-Custom"); got != "keep" {
		t.Fatalf("X-Custom = %q, want keep", got)
	}
}
