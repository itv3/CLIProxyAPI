package officialclient

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
)

type Provider string

const (
	ProviderClaude Provider = "claude"
	ProviderCodex  Provider = "codex"

	AttributeKey       = "official_client_compatibility"
	SupportHeader      = "X-CPA-SUPPORT-OFFICIAL-CLIENT-COMPATIBILITY"
	SupportHeaderValue = "true"
	DecisionDisabled   = "disabled"
	DecisionBypass     = "bypass_official_client"
	DecisionApply      = "apply_profile"
)

var (
	ErrUnsupportedProvider = errors.New("official client compatibility provider is unsupported")
	ErrProfileRequired     = errors.New("official client compatibility profile is required when enabled")
	ErrUnknownProfile      = errors.New("official client compatibility profile is unknown or inactive")
	ErrProviderMismatch    = errors.New("official client compatibility profile provider mismatch")
	ErrTLSProfile          = errors.New("official client compatibility TLS profile is unsupported")
	ErrInvalidAttributes   = errors.New("official client compatibility attributes are invalid")
	ErrAPIKeyRequired      = errors.New("official client compatibility requires an API key account")
)

type CompatibilityConfig struct {
	Enabled    bool   `yaml:"enabled" json:"enabled"`
	Profile    string `yaml:"profile" json:"profile"`
	TLSProfile string `yaml:"tls-profile" json:"tls-profile"`
}

type Profile struct {
	ID            string
	Provider      Provider
	ClientVersion string
	Active        bool
	Claude        *ClaudeProfile
	Codex         *CodexProfile
}

type ClaudeProfile struct {
	AgentSDKVersion        string
	Entrypoint             string
	IdentitySystemText     string
	FixedHeaders           map[string]string
	MessagesHeaders        map[string]string
	MessagesStreamBetas    []string
	MessagesNonStreamBetas []string
	CountTokensBetas       []string
	StructuredOutputsBeta  string
	ToolNameMap            map[string]string
}

type CodexProfile struct {
	DesktopBuild            string
	Platform                string
	ResponsesHeaders        map[string]string
	CompactHeaders          map[string]string
	ReasoningContext        string
	TextVerbosity           string
	RequiredIncludes        []string
	CompactBodyAllowlist    []string
	TurnMetadataFixedValues map[string]string
}

var profiles = map[string]Profile{
	"claude-desktop-2.1.215-v1": {
		ID:            "claude-desktop-2.1.215-v1",
		Provider:      ProviderClaude,
		ClientVersion: "2.1.215",
		Active:        true,
		Claude: &ClaudeProfile{
			AgentSDKVersion:    "0.3.215",
			Entrypoint:         "claude-desktop-3p",
			IdentitySystemText: "You are a Claude agent, built on Anthropic's Claude Agent SDK.",
			FixedHeaders: map[string]string{
				"Accept": "application/json",
				"Anthropic-Dangerous-Direct-Browser-Access": "true",
				"Anthropic-Version":                         "2023-06-01",
				"Content-Type":                              "application/json",
				"User-Agent":                                "claude-cli/2.1.215 (external, claude-desktop-3p, agent-sdk/0.3.215)",
				"X-App":                                     "cli",
				"X-Stainless-Arch":                          "arm64",
				"X-Stainless-Lang":                          "js",
				"X-Stainless-OS":                            "MacOS",
				"X-Stainless-Package-Version":               "0.94.0",
				"X-Stainless-Retry-Count":                   "0",
				"X-Stainless-Runtime":                       "node",
				"X-Stainless-Runtime-Version":               "v26.3.0",
			},
			MessagesHeaders: map[string]string{
				"X-Stainless-Timeout": "900",
			},
			MessagesStreamBetas: []string{
				"claude-code-20250219",
				"context-1m-2025-08-07",
				"interleaved-thinking-2025-05-14",
				"mid-conversation-system-2026-04-07",
				"effort-2025-11-24",
				"fallback-credit-2026-06-01",
			},
			MessagesNonStreamBetas: []string{
				"claude-code-20250219",
				"context-1m-2025-08-07",
				"interleaved-thinking-2025-05-14",
				"mid-conversation-system-2026-04-07",
				"effort-2025-11-24",
				"fallback-credit-2026-06-01",
			},
			CountTokensBetas: []string{
				"claude-code-20250219",
				"context-1m-2025-08-07",
				"interleaved-thinking-2025-05-14",
				"mid-conversation-system-2026-04-07",
				"effort-2025-11-24",
				"fallback-credit-2026-06-01",
				"token-counting-2024-11-01",
			},
			StructuredOutputsBeta: "structured-outputs-2025-12-15",
			ToolNameMap: map[string]string{
				"bash":         "Bash",
				"edit":         "Edit",
				"notebookedit": "NotebookEdit",
				"read":         "Read",
				"skill":        "Skill",
				"webfetch":     "WebFetch",
				"websearch":    "WebSearch",
				"write":        "Write",
			},
		},
	},
	"codex-desktop-0.145.0-alpha.18-v1": {
		ID:            "codex-desktop-0.145.0-alpha.18-v1",
		Provider:      ProviderCodex,
		ClientVersion: "0.145.0-alpha.18",
		Active:        true,
		Codex: &CodexProfile{
			DesktopBuild: "26.715.31925",
			Platform:     "Mac OS 26.5.2/arm64",
			ResponsesHeaders: map[string]string{
				"Accept":                "text/event-stream",
				"Content-Type":          "application/json",
				"Originator":            "Codex Desktop",
				"User-Agent":            "Codex Desktop/0.145.0-alpha.18 (Mac OS 26.5.2; arm64) unknown (Codex Desktop; 26.715.31925)",
				"X-Codex-Beta-Features": "remote_compaction_v2",
			},
			CompactHeaders: map[string]string{
				"Accept":                "application/json",
				"Content-Type":          "application/json",
				"Originator":            "Codex Desktop",
				"User-Agent":            "Codex Desktop/0.145.0-alpha.18 (Mac OS 26.5.2; arm64) unknown (Codex Desktop; 26.715.31925)",
				"X-Codex-Beta-Features": "remote_compaction_v2",
			},
			ReasoningContext: "all_turns",
			TextVerbosity:    "low",
			RequiredIncludes: []string{
				"reasoning.encrypted_content",
			},
			CompactBodyAllowlist: []string{
				"input",
				"instructions",
				"model",
				"parallel_tool_calls",
				"previous_response_id",
				"reasoning",
				"text",
				"tools",
			},
			TurnMetadataFixedValues: map[string]string{
				"request_kind":   "turn",
				"sandbox":        "none",
				"thread_source":  "user",
				"workspace_kind": "project",
			},
		},
	},
}

type DecisionInput struct {
	Provider       string
	APIKeyAccount  bool
	Compatibility  *CompatibilityConfig
	OfficialClient bool
	Connectivity   bool
}

type Decision struct {
	State    string
	Provider Provider
	Profile  Profile
}

func NormalizeProvider(value string) Provider {
	return Provider(strings.ToLower(strings.TrimSpace(value)))
}

func IsSupportedProvider(value string) bool {
	switch NormalizeProvider(value) {
	case ProviderClaude, ProviderCodex:
		return true
	default:
		return false
	}
}

func CurrentProfile(provider string) (Profile, bool) {
	var id string
	switch NormalizeProvider(provider) {
	case ProviderClaude:
		id = "claude-desktop-2.1.215-v1"
	case ProviderCodex:
		id = "codex-desktop-0.145.0-alpha.18-v1"
	default:
		return Profile{}, false
	}
	return LookupProfile(id)
}

func LookupProfile(id string) (Profile, bool) {
	profile, ok := profiles[strings.TrimSpace(id)]
	return cloneProfile(profile), ok
}

func Profiles() []Profile {
	out := make([]Profile, 0, len(profiles))
	for _, profile := range profiles {
		out = append(out, cloneProfile(profile))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func cloneProfile(profile Profile) Profile {
	if profile.Claude != nil {
		claude := *profile.Claude
		claude.FixedHeaders = cloneStringMap(claude.FixedHeaders)
		claude.MessagesHeaders = cloneStringMap(claude.MessagesHeaders)
		claude.MessagesStreamBetas = append([]string(nil), claude.MessagesStreamBetas...)
		claude.MessagesNonStreamBetas = append([]string(nil), claude.MessagesNonStreamBetas...)
		claude.CountTokensBetas = append([]string(nil), claude.CountTokensBetas...)
		claude.ToolNameMap = cloneStringMap(claude.ToolNameMap)
		profile.Claude = &claude
	}
	if profile.Codex != nil {
		codex := *profile.Codex
		codex.ResponsesHeaders = cloneStringMap(codex.ResponsesHeaders)
		codex.CompactHeaders = cloneStringMap(codex.CompactHeaders)
		codex.RequiredIncludes = append([]string(nil), codex.RequiredIncludes...)
		codex.CompactBodyAllowlist = append([]string(nil), codex.CompactBodyAllowlist...)
		codex.TurnMetadataFixedValues = cloneStringMap(codex.TurnMetadataFixedValues)
		profile.Codex = &codex
	}
	return profile
}

func cloneStringMap(input map[string]string) map[string]string {
	if input == nil {
		return nil
	}
	output := make(map[string]string, len(input))
	for key, value := range input {
		output[key] = value
	}
	return output
}

func NormalizeCompatibility(provider string, cfg *CompatibilityConfig, assignCurrent bool) error {
	if cfg == nil {
		return nil
	}
	cfg.Profile = strings.TrimSpace(cfg.Profile)
	cfg.TLSProfile = strings.TrimSpace(cfg.TLSProfile)
	if cfg.Enabled && cfg.Profile == "" && assignCurrent {
		profile, ok := CurrentProfile(provider)
		if !ok {
			return fmt.Errorf("%w: %q", ErrUnsupportedProvider, provider)
		}
		cfg.Profile = profile.ID
	}
	return ValidateCompatibility(provider, cfg)
}

func ValidateCompatibility(provider string, cfg *CompatibilityConfig) error {
	if cfg == nil {
		return nil
	}
	normalizedProvider := NormalizeProvider(provider)
	if !IsSupportedProvider(string(normalizedProvider)) {
		return fmt.Errorf("%w: %q", ErrUnsupportedProvider, provider)
	}
	if cfg.TLSProfile != "" {
		return fmt.Errorf("%w: %q", ErrTLSProfile, cfg.TLSProfile)
	}
	if cfg.Profile == "" {
		if cfg.Enabled {
			return ErrProfileRequired
		}
		return nil
	}
	profile, ok := LookupProfile(cfg.Profile)
	if !ok || !profile.Active {
		return fmt.Errorf("%w: %q", ErrUnknownProfile, cfg.Profile)
	}
	if profile.Provider != normalizedProvider {
		return fmt.Errorf("%w: profile=%q provider=%q", ErrProviderMismatch, cfg.Profile, provider)
	}
	return nil
}

func EncodeCompatibility(cfg *CompatibilityConfig) (string, error) {
	if cfg == nil {
		return "", ErrInvalidAttributes
	}
	data, err := json.Marshal(cfg)
	if err != nil {
		return "", fmt.Errorf("encode official client compatibility attributes: %w", err)
	}
	return string(data), nil
}

func DecodeCompatibility(raw string) (*CompatibilityConfig, error) {
	return decodeCompatibility(raw, false)
}

func decodeCompatibility(raw string, requireFull bool) (*CompatibilityConfig, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, ErrInvalidAttributes
	}
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.DisallowUnknownFields()
	var decoded *struct {
		Enabled    *bool   `json:"enabled"`
		Profile    *string `json:"profile"`
		TLSProfile *string `json:"tls-profile"`
	}
	if err := decoder.Decode(&decoded); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidAttributes, err)
	}
	if decoded == nil {
		return nil, fmt.Errorf("%w: null is not allowed", ErrInvalidAttributes)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("%w: trailing JSON value", ErrInvalidAttributes)
	}
	if requireFull && (decoded.Enabled == nil || decoded.Profile == nil || decoded.TLSProfile == nil) {
		return nil, fmt.Errorf("%w: complete enabled, profile, and tls-profile fields are required", ErrInvalidAttributes)
	}
	cfg := &CompatibilityConfig{}
	if decoded.Enabled != nil {
		cfg.Enabled = *decoded.Enabled
	}
	if decoded.Profile != nil {
		cfg.Profile = *decoded.Profile
	}
	if decoded.TLSProfile != nil {
		cfg.TLSProfile = *decoded.TLSProfile
	}
	cfg.Profile = strings.TrimSpace(cfg.Profile)
	cfg.TLSProfile = strings.TrimSpace(cfg.TLSProfile)
	return cfg, nil
}

func CompatibilityFromAttributes(attrs map[string]string) (*CompatibilityConfig, error) {
	if len(attrs) == 0 {
		return nil, nil
	}
	raw, ok := attrs[AttributeKey]
	if !ok {
		return nil, nil
	}
	return decodeCompatibility(raw, true)
}

func SetCompatibilityAttribute(attrs map[string]string, provider string, cfg *CompatibilityConfig) error {
	if attrs == nil {
		return ErrInvalidAttributes
	}
	if cfg == nil {
		delete(attrs, AttributeKey)
		return nil
	}
	if err := ValidateCompatibility(provider, cfg); err != nil {
		return err
	}
	raw, err := EncodeCompatibility(cfg)
	if err != nil {
		return err
	}
	attrs[AttributeKey] = raw
	return nil
}

func Decide(input DecisionInput) (Decision, error) {
	provider := NormalizeProvider(input.Provider)
	if input.Compatibility == nil {
		return Decision{State: DecisionDisabled, Provider: provider}, nil
	}
	if err := ValidateCompatibility(string(provider), input.Compatibility); err != nil {
		return Decision{}, err
	}
	if !input.Compatibility.Enabled {
		return Decision{State: DecisionDisabled, Provider: provider}, nil
	}
	if !input.APIKeyAccount {
		return Decision{}, ErrAPIKeyRequired
	}
	profile, ok := LookupProfile(input.Compatibility.Profile)
	if !ok {
		return Decision{}, fmt.Errorf("%w: %q", ErrUnknownProfile, input.Compatibility.Profile)
	}
	if input.OfficialClient && !input.Connectivity {
		return Decision{State: DecisionBypass, Provider: provider, Profile: profile}, nil
	}
	return Decision{State: DecisionApply, Provider: provider, Profile: profile}, nil
}

func IsOfficialClient(provider string, headers http.Header) bool {
	if headers == nil {
		return false
	}
	userAgent := strings.ToLower(strings.TrimSpace(headerValue(headers, "User-Agent")))
	originator := strings.TrimSpace(headerValue(headers, "originator"))
	switch NormalizeProvider(provider) {
	case ProviderClaude:
		return strings.HasPrefix(userAgent, "claude-cli/") ||
			strings.HasPrefix(userAgent, "claude desktop/") ||
			strings.HasPrefix(userAgent, "claude-desktop/") ||
			strings.HasPrefix(userAgent, "claude_desktop/") ||
			strings.HasPrefix(userAgent, "claude_app/")
	case ProviderCodex:
		return strings.HasPrefix(userAgent, "codex desktop/") ||
			strings.EqualFold(originator, "Codex Desktop") ||
			strings.HasPrefix(userAgent, "codex_cli_rs/")
	default:
		return false
	}
}

func headerValue(headers http.Header, name string) string {
	for key, values := range headers {
		if !strings.EqualFold(key, name) || len(values) == 0 {
			continue
		}
		return values[0]
	}
	return ""
}

func ProtectedHeaderNames(provider string) []string {
	switch NormalizeProvider(provider) {
	case ProviderClaude:
		return []string{
			"accept", "anthropic-beta", "anthropic-dangerous-direct-browser-access", "anthropic-version",
			"authorization", "content-type", "user-agent", "x-api-key", "x-app", "x-claude-code-session-id",
			"x-client-request-id", "x-stainless-arch", "x-stainless-helper-method", "x-stainless-lang",
			"x-stainless-os", "x-stainless-package-version", "x-stainless-retry-count", "x-stainless-runtime",
			"x-stainless-runtime-version", "x-stainless-timeout",
		}
	case ProviderCodex:
		return []string{
			"authorization", "conversation_id", "openai-beta", "originator", "session-id", "session_id",
			"thread-id", "user-agent", "version", "x-api-key", "x-client-request-id", "x-codex-beta-features",
			"x-codex-turn-metadata", "x-codex-turn-state", "x-codex-window-id", "x-openai-internal-codex-responses-lite",
		}
	default:
		return nil
	}
}

func FinalizeProtectedHeaders(headers http.Header, provider string, desired map[string]string) {
	if headers == nil {
		return
	}
	protected := ProtectedHeaderNames(provider)
	for _, name := range protected {
		DeleteHeaderAllForms(headers, name)
	}
	for name, value := range desired {
		if !containsHeaderName(protected, name) {
			continue
		}
		headers.Set(name, value)
	}
}

func containsHeaderName(names []string, candidate string) bool {
	for _, name := range names {
		if strings.EqualFold(name, candidate) {
			return true
		}
	}
	return false
}

func DeleteHeaderAllForms(headers http.Header, name string) {
	for key := range headers {
		if strings.EqualFold(key, name) {
			delete(headers, key)
		}
	}
}
