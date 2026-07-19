package officialclient

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

func TestProfilesAndCurrentProfile(t *testing.T) {
	got := Profiles()
	if len(got) != 2 {
		t.Fatalf("Profiles() length = %d, want 2", len(got))
	}
	if got[0].ID != "claude-desktop-2.1.215-v1" || got[1].ID != "codex-desktop-0.145.0-alpha.18-v1" {
		t.Fatalf("Profiles() IDs = %q, %q", got[0].ID, got[1].ID)
	}
	profile, ok := CurrentProfile(" CODEX ")
	if !ok || profile.ID != "codex-desktop-0.145.0-alpha.18-v1" {
		t.Fatalf("CurrentProfile(codex) = %#v, %v", profile, ok)
	}
	if _, ok := CurrentProfile("xai"); ok {
		t.Fatal("CurrentProfile(xai) unexpectedly succeeded")
	}
}

func TestClaudeProfileMatchesGoldenFixture(t *testing.T) {
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller() failed")
	}
	data, err := os.ReadFile(filepath.Join(filepath.Dir(filename), "..", "..", "..", "..", "docs", "official-client-compatibility", "fixtures", "claude-desktop-2.1.215-v1.golden.json"))
	if err != nil {
		t.Fatalf("read golden fixture: %v", err)
	}
	var fixture struct {
		ProfileID string `json:"profile_id"`
		Client    struct {
			ClientVersion   string `json:"client_version"`
			AgentSDKVersion string `json:"agent_sdk_version"`
		} `json:"client"`
		Headers struct {
			Fixed        map[string]string `json:"fixed"`
			MessagesOnly map[string]string `json:"messages_only"`
			Dynamic      struct {
				RetryCount struct {
					Initial string `json:"initial"`
				} `json:"x-stainless-retry-count"`
			} `json:"dynamic"`
		} `json:"headers"`
		Endpoints struct {
			MessagesStream struct {
				AnthropicBeta struct {
					BaseOrdered       []string `json:"base_ordered"`
					JSONSchemaReplace string   `json:"json_schema_replace_last"`
				} `json:"anthropic_beta"`
			} `json:"messages_stream"`
			MessagesNonStream struct {
				AnthropicBeta struct {
					Ordered           []string `json:"ordered"`
					JSONSchemaReplace string   `json:"json_schema_replace_last"`
				} `json:"anthropic_beta"`
			} `json:"messages_non_stream"`
			CountTokens struct {
				AnthropicBeta struct {
					Ordered []string `json:"ordered"`
				} `json:"anthropic_beta"`
			} `json:"count_tokens"`
		} `json:"endpoints"`
		ToolNormalization struct {
			KnownCaseNormalization map[string]string `json:"known_case_normalization"`
		} `json:"tool_normalization"`
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatalf("decode golden fixture: %v", err)
	}

	profile, ok := LookupProfile(fixture.ProfileID)
	if !ok || profile.Claude == nil {
		t.Fatalf("LookupProfile(%q) = %#v, %v", fixture.ProfileID, profile, ok)
	}
	if profile.ClientVersion != fixture.Client.ClientVersion || profile.Claude.AgentSDKVersion != fixture.Client.AgentSDKVersion {
		t.Fatalf("client versions = %q/%q, want %q/%q", profile.ClientVersion, profile.Claude.AgentSDKVersion, fixture.Client.ClientVersion, fixture.Client.AgentSDKVersion)
	}
	wantFixedHeaders := make(map[string]string, len(fixture.Headers.Fixed)+1)
	for key, value := range fixture.Headers.Fixed {
		wantFixedHeaders[strings.ToLower(key)] = value
	}
	wantFixedHeaders["x-stainless-retry-count"] = fixture.Headers.Dynamic.RetryCount.Initial
	gotFixedHeaders := make(map[string]string, len(profile.Claude.FixedHeaders))
	for key, value := range profile.Claude.FixedHeaders {
		gotFixedHeaders[strings.ToLower(key)] = value
	}
	if !reflect.DeepEqual(gotFixedHeaders, wantFixedHeaders) {
		t.Fatalf("fixed headers = %#v, want %#v", gotFixedHeaders, wantFixedHeaders)
	}
	wantMessagesHeaders := make(map[string]string, len(fixture.Headers.MessagesOnly))
	for key, value := range fixture.Headers.MessagesOnly {
		wantMessagesHeaders[http.CanonicalHeaderKey(key)] = value
	}
	if !reflect.DeepEqual(profile.Claude.MessagesHeaders, wantMessagesHeaders) {
		t.Fatalf("messages headers = %#v, want %#v", profile.Claude.MessagesHeaders, wantMessagesHeaders)
	}
	if !reflect.DeepEqual(profile.Claude.MessagesStreamBetas, fixture.Endpoints.MessagesStream.AnthropicBeta.BaseOrdered) ||
		!reflect.DeepEqual(profile.Claude.MessagesNonStreamBetas, fixture.Endpoints.MessagesNonStream.AnthropicBeta.Ordered) ||
		!reflect.DeepEqual(profile.Claude.CountTokensBetas, fixture.Endpoints.CountTokens.AnthropicBeta.Ordered) {
		t.Fatal("Anthropic beta policy differs from golden fixture")
	}
	if profile.Claude.StructuredOutputsBeta != fixture.Endpoints.MessagesStream.AnthropicBeta.JSONSchemaReplace {
		t.Fatalf("structured outputs beta = %q, want %q", profile.Claude.StructuredOutputsBeta, fixture.Endpoints.MessagesStream.AnthropicBeta.JSONSchemaReplace)
	}
	if profile.Claude.StructuredOutputsBeta != fixture.Endpoints.MessagesNonStream.AnthropicBeta.JSONSchemaReplace {
		t.Fatalf("non-stream structured outputs beta = %q, want %q", profile.Claude.StructuredOutputsBeta, fixture.Endpoints.MessagesNonStream.AnthropicBeta.JSONSchemaReplace)
	}
	if !reflect.DeepEqual(profile.Claude.ToolNameMap, fixture.ToolNormalization.KnownCaseNormalization) {
		t.Fatalf("tool name map = %#v, want %#v", profile.Claude.ToolNameMap, fixture.ToolNormalization.KnownCaseNormalization)
	}
	if !strings.Contains(profile.Claude.FixedHeaders["User-Agent"], profile.Claude.Entrypoint) {
		t.Fatalf("User-Agent does not contain entrypoint %q", profile.Claude.Entrypoint)
	}
}

func TestCodexProfileMatchesGoldenFixture(t *testing.T) {
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller() failed")
	}
	data, err := os.ReadFile(filepath.Join(filepath.Dir(filename), "..", "..", "..", "..", "docs", "official-client-compatibility", "fixtures", "codex-desktop-0.145.0-alpha.18-v1.golden.json"))
	if err != nil {
		t.Fatalf("read golden fixture: %v", err)
	}
	var fixture struct {
		ProfileID string `json:"profile_id"`
		Client    struct {
			ClientVersion string `json:"client_version"`
			DesktopBuild  string `json:"desktop_build"`
			Platform      string `json:"platform"`
		} `json:"client"`
		Headers struct {
			Responses map[string]string `json:"fixed_http_sse"`
			Compact   map[string]string `json:"fixed_compact"`
			Protected []string          `json:"protected_case_insensitive"`
		} `json:"headers"`
		Endpoints struct {
			Responses struct {
				Body struct {
					RequiredIncludes []string `json:"ensure_include_contains"`
					EnsureIfMissing  struct {
						ReasoningContext string `json:"reasoning.context"`
						TextVerbosity    string `json:"text.verbosity"`
					} `json:"ensure_if_missing"`
					TurnMetadata struct {
						FixedValues map[string]string `json:"fixed_public_values"`
					} `json:"turn_metadata"`
				} `json:"body"`
			} `json:"responses_http_sse"`
			Compact struct {
				BodyAllowlist []string `json:"body_allowlist"`
			} `json:"responses_compact"`
		} `json:"endpoints"`
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatalf("decode golden fixture: %v", err)
	}

	profile, ok := LookupProfile(fixture.ProfileID)
	if !ok || profile.Codex == nil {
		t.Fatalf("LookupProfile(%q) = %#v, %v", fixture.ProfileID, profile, ok)
	}
	if profile.ClientVersion != fixture.Client.ClientVersion || profile.Codex.DesktopBuild != fixture.Client.DesktopBuild || profile.Codex.Platform != fixture.Client.Platform {
		t.Fatal("Codex client identity differs from golden fixture")
	}
	if !reflect.DeepEqual(lowercaseStringMap(profile.Codex.ResponsesHeaders), fixture.Headers.Responses) ||
		!reflect.DeepEqual(lowercaseStringMap(profile.Codex.CompactHeaders), fixture.Headers.Compact) {
		t.Fatal("Codex fixed headers differ from golden fixture")
	}
	if !reflect.DeepEqual(ProtectedHeaderNames("codex"), fixture.Headers.Protected) {
		t.Fatal("Codex protected headers differ from golden fixture")
	}
	if profile.Codex.ReasoningContext != fixture.Endpoints.Responses.Body.EnsureIfMissing.ReasoningContext ||
		profile.Codex.TextVerbosity != fixture.Endpoints.Responses.Body.EnsureIfMissing.TextVerbosity ||
		!reflect.DeepEqual(profile.Codex.RequiredIncludes, fixture.Endpoints.Responses.Body.RequiredIncludes) ||
		!reflect.DeepEqual(profile.Codex.CompactBodyAllowlist, fixture.Endpoints.Compact.BodyAllowlist) ||
		!reflect.DeepEqual(profile.Codex.TurnMetadataFixedValues, fixture.Endpoints.Responses.Body.TurnMetadata.FixedValues) {
		t.Fatal("Codex body policy differs from golden fixture")
	}
}

func lowercaseStringMap(input map[string]string) map[string]string {
	output := make(map[string]string, len(input))
	for key, value := range input {
		output[strings.ToLower(key)] = value
	}
	return output
}

func TestLookupProfileReturnsClone(t *testing.T) {
	profile, ok := LookupProfile("claude-desktop-2.1.215-v1")
	if !ok || profile.Claude == nil {
		t.Fatal("Claude profile not found")
	}
	profile.Claude.FixedHeaders["User-Agent"] = "changed"
	profile.Claude.MessagesStreamBetas[0] = "changed"
	profile.Claude.ToolNameMap["bash"] = "changed"

	got, _ := LookupProfile("claude-desktop-2.1.215-v1")
	if got.Claude.FixedHeaders["User-Agent"] == "changed" || got.Claude.MessagesStreamBetas[0] == "changed" || got.Claude.ToolNameMap["bash"] == "changed" {
		t.Fatal("LookupProfile() exposed mutable registry state")
	}

	codex, ok := LookupProfile("codex-desktop-0.145.0-alpha.18-v1")
	if !ok || codex.Codex == nil {
		t.Fatal("Codex profile not found")
	}
	codex.Codex.ResponsesHeaders["User-Agent"] = "changed"
	codex.Codex.RequiredIncludes[0] = "changed"
	codex.Codex.CompactBodyAllowlist[0] = "changed"
	codex.Codex.TurnMetadataFixedValues["request_kind"] = "changed"
	gotCodex, _ := LookupProfile("codex-desktop-0.145.0-alpha.18-v1")
	if gotCodex.Codex.ResponsesHeaders["User-Agent"] == "changed" || gotCodex.Codex.RequiredIncludes[0] == "changed" || gotCodex.Codex.CompactBodyAllowlist[0] == "changed" || gotCodex.Codex.TurnMetadataFixedValues["request_kind"] == "changed" {
		t.Fatal("LookupProfile() exposed mutable Codex registry state")
	}
}

func TestNormalizeCompatibility(t *testing.T) {
	tests := []struct {
		name          string
		provider      string
		cfg           *CompatibilityConfig
		assignCurrent bool
		wantProfile   string
		wantErr       error
	}{
		{name: "disabled empty", provider: "claude", cfg: &CompatibilityConfig{}},
		{name: "assign current", provider: "codex", cfg: &CompatibilityConfig{Enabled: true}, assignCurrent: true, wantProfile: "codex-desktop-0.145.0-alpha.18-v1"},
		{name: "profile required", provider: "codex", cfg: &CompatibilityConfig{Enabled: true}, wantErr: ErrProfileRequired},
		{name: "unknown profile", provider: "codex", cfg: &CompatibilityConfig{Profile: "missing"}, wantErr: ErrUnknownProfile},
		{name: "provider mismatch", provider: "claude", cfg: &CompatibilityConfig{Profile: "codex-desktop-0.145.0-alpha.18-v1"}, wantErr: ErrProviderMismatch},
		{name: "tls profile", provider: "claude", cfg: &CompatibilityConfig{TLSProfile: "chrome"}, wantErr: ErrTLSProfile},
		{name: "unsupported provider", provider: "xai", cfg: &CompatibilityConfig{}, wantErr: ErrUnsupportedProvider},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := NormalizeCompatibility(tt.provider, tt.cfg, tt.assignCurrent)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("NormalizeCompatibility() error = %v, want %v", err, tt.wantErr)
			}
			if tt.cfg.Profile != tt.wantProfile && tt.wantProfile != "" {
				t.Fatalf("profile = %q, want %q", tt.cfg.Profile, tt.wantProfile)
			}
		})
	}
}

func TestCompatibilityAttributesRoundTrip(t *testing.T) {
	attrs := map[string]string{}
	want := &CompatibilityConfig{Enabled: false, Profile: "claude-desktop-2.1.215-v1"}
	if err := SetCompatibilityAttribute(attrs, "claude", want); err != nil {
		t.Fatalf("SetCompatibilityAttribute() error = %v", err)
	}
	got, err := CompatibilityFromAttributes(attrs)
	if err != nil {
		t.Fatalf("CompatibilityFromAttributes() error = %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("decoded config = %#v, want %#v", got, want)
	}
	if attrs[AttributeKey] != `{"enabled":false,"profile":"claude-desktop-2.1.215-v1","tls-profile":""}` {
		t.Fatalf("attribute = %q", attrs[AttributeKey])
	}

	invalid := []string{
		``,
		`null`,
		`{"enabled":true,"profile":"claude-desktop-2.1.215-v1","extra":true}`,
		`{"enabled":true}{"enabled":false}`,
	}
	for _, raw := range invalid {
		if _, err := DecodeCompatibility(raw); !errors.Is(err, ErrInvalidAttributes) {
			t.Fatalf("DecodeCompatibility(%q) error = %v", raw, err)
		}
	}
	for _, raw := range []string{`{}`, `{"enabled":false,"profile":""}`} {
		if _, err := CompatibilityFromAttributes(map[string]string{AttributeKey: raw}); !errors.Is(err, ErrInvalidAttributes) {
			t.Fatalf("CompatibilityFromAttributes(%q) error = %v", raw, err)
		}
	}
}

func TestDecide(t *testing.T) {
	claude := &CompatibilityConfig{Enabled: true, Profile: "claude-desktop-2.1.215-v1"}
	disabled := &CompatibilityConfig{Profile: "claude-desktop-2.1.215-v1"}
	tests := []struct {
		name      string
		input     DecisionInput
		wantState string
		wantErr   error
	}{
		{name: "missing config", input: DecisionInput{Provider: "xai"}, wantState: DecisionDisabled},
		{name: "disabled", input: DecisionInput{Provider: "claude", Compatibility: disabled}, wantState: DecisionDisabled},
		{name: "apply", input: DecisionInput{Provider: "claude", APIKeyAccount: true, Compatibility: claude}, wantState: DecisionApply},
		{name: "bypass official", input: DecisionInput{Provider: "claude", APIKeyAccount: true, Compatibility: claude, OfficialClient: true}, wantState: DecisionBypass},
		{name: "connectivity applies", input: DecisionInput{Provider: "claude", APIKeyAccount: true, Compatibility: claude, OfficialClient: true, Connectivity: true}, wantState: DecisionApply},
		{name: "requires api key", input: DecisionInput{Provider: "claude", Compatibility: claude}, wantErr: ErrAPIKeyRequired},
		{name: "xai attribute fails", input: DecisionInput{Provider: "xai", Compatibility: &CompatibilityConfig{}}, wantErr: ErrUnsupportedProvider},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Decide(tt.input)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("Decide() error = %v, want %v", err, tt.wantErr)
			}
			if got.State != tt.wantState {
				t.Fatalf("state = %q, want %q", got.State, tt.wantState)
			}
		})
	}
}

func TestIsOfficialClient(t *testing.T) {
	if !IsOfficialClient("claude", http.Header{"User-Agent": {"claude-cli/2.1.215"}}) {
		t.Fatal("Claude client was not recognized")
	}
	if !IsOfficialClient("codex", http.Header{"originator": {"codex desktop"}}) {
		t.Fatal("Codex client was not recognized")
	}
	if IsOfficialClient("codex", http.Header{"User-Agent": {"curl/8"}}) {
		t.Fatal("curl was incorrectly recognized")
	}
}

func TestFinalizeProtectedHeaders(t *testing.T) {
	headers := http.Header{
		"authorization": {"old-lower"},
		"Authorization": {"old-canonical"},
		"X-Custom":      {"keep"},
	}
	FinalizeProtectedHeaders(headers, "codex", map[string]string{
		"Authorization": "Bearer new",
		"X-Custom":      "replace",
	})
	if got := headers.Values("Authorization"); !reflect.DeepEqual(got, []string{"Bearer new"}) {
		t.Fatalf("Authorization values = %#v", got)
	}
	if got := headers.Get("X-Custom"); got != "keep" {
		t.Fatalf("X-Custom = %q, want keep", got)
	}
}
