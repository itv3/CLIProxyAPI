package helps

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/officialclient"
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/executor"
)

type CodexOfficialClientEndpoint string

const (
	CodexOfficialClientResponses CodexOfficialClientEndpoint = "responses"
	CodexOfficialClientCompact   CodexOfficialClientEndpoint = "compact"
)

type CodexOfficialClientRequestState struct {
	InstallationID      string
	SessionID           string
	ThreadID            string
	TurnID              string
	WindowID            string
	TurnStartedAtUnixMS int64
	TurnMetadata        string
}

func ApplyCodexOfficialClientResponsesProfile(profile officialclient.Profile, authID string, payload []byte, headers http.Header, metadata map[string]any) ([]byte, *CodexOfficialClientRequestState, error) {
	codex, body, err := decodeCodexOfficialClientBody(profile, authID, payload)
	if err != nil {
		return nil, nil, NewOfficialClientRequestError(err)
	}
	state, err := newCodexOfficialClientRequestState(profile, authID, body, headers, metadata)
	if err != nil {
		return nil, nil, NewOfficialClientRequestError(err)
	}

	body["store"] = false
	body["stream"] = true
	ensureCodexOfficialClientIncludes(body, codex.RequiredIncludes)
	ensureCodexOfficialClientNestedDefault(body, "reasoning", "context", codex.ReasoningContext)
	ensureCodexOfficialClientNestedDefault(body, "text", "verbosity", codex.TextVerbosity)
	normalizeCodexOfficialClientInput(body)
	body["prompt_cache_key"] = state.SessionID
	body["client_metadata"] = map[string]any{
		"session_id":              state.SessionID,
		"thread_id":               state.ThreadID,
		"turn_id":                 state.TurnID,
		"x-codex-installation-id": state.InstallationID,
		"x-codex-turn-metadata":   state.TurnMetadata,
		"x-codex-window-id":       state.WindowID,
	}

	encoded, err := json.Marshal(body)
	if err != nil {
		return nil, nil, NewOfficialClientRequestError(fmt.Errorf("encode Codex official client body: %w", err))
	}
	return encoded, state, nil
}

func ApplyCodexOfficialClientCompactProfile(profile officialclient.Profile, authID string, payload []byte, headers http.Header, metadata map[string]any) ([]byte, *CodexOfficialClientRequestState, error) {
	codex, body, err := decodeCodexOfficialClientBody(profile, authID, payload)
	if err != nil {
		return nil, nil, NewOfficialClientRequestError(err)
	}
	state, err := newCodexOfficialClientRequestState(profile, authID, body, headers, metadata)
	if err != nil {
		return nil, nil, NewOfficialClientRequestError(err)
	}

	allowed := make(map[string]struct{}, len(codex.CompactBodyAllowlist))
	for _, name := range codex.CompactBodyAllowlist {
		allowed[name] = struct{}{}
	}
	for name := range body {
		if _, ok := allowed[name]; !ok {
			delete(body, name)
		}
	}
	encoded, err := json.Marshal(body)
	if err != nil {
		return nil, nil, NewOfficialClientRequestError(fmt.Errorf("encode Codex compact body: %w", err))
	}
	return encoded, state, nil
}

func CodexOfficialClientHeaders(profile officialclient.Profile, endpoint CodexOfficialClientEndpoint, apiKey string, state *CodexOfficialClientRequestState) (map[string]string, error) {
	if strings.TrimSpace(apiKey) == "" {
		return nil, NewOfficialClientRequestError(errors.New("Codex official client profile requires an API key"))
	}
	if state == nil || strings.TrimSpace(state.SessionID) == "" || strings.TrimSpace(state.TurnMetadata) == "" {
		return nil, NewOfficialClientRequestError(errors.New("Codex official client profile requires request state"))
	}
	codex, err := validateCodexOfficialClientProfile(profile, "header", []byte(`{}`))
	if err != nil {
		return nil, NewOfficialClientRequestError(err)
	}

	var fixed map[string]string
	switch endpoint {
	case CodexOfficialClientResponses:
		fixed = codex.ResponsesHeaders
	case CodexOfficialClientCompact:
		fixed = codex.CompactHeaders
	default:
		return nil, NewOfficialClientRequestError(fmt.Errorf("unsupported Codex official client endpoint %q", endpoint))
	}
	desired := make(map[string]string, len(fixed)+6)
	for name, value := range fixed {
		desired[name] = value
	}
	desired["Authorization"] = "Bearer " + apiKey
	desired["X-Client-Request-Id"] = state.SessionID
	desired["Session-Id"] = state.SessionID
	desired["Thread-Id"] = state.ThreadID
	desired["X-Codex-Window-Id"] = state.WindowID
	desired["X-Codex-Turn-Metadata"] = state.TurnMetadata
	return desired, nil
}

func decodeCodexOfficialClientBody(profile officialclient.Profile, authID string, payload []byte) (*officialclient.CodexProfile, map[string]any, error) {
	codex, err := validateCodexOfficialClientProfile(profile, authID, payload)
	if err != nil {
		return nil, nil, err
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.UseNumber()
	var body map[string]any
	if err := decoder.Decode(&body); err != nil {
		return nil, nil, fmt.Errorf("decode Codex official client body: %w", err)
	}
	if err := ensureCodexJSONEOF(decoder); err != nil {
		return nil, nil, err
	}
	return codex, body, nil
}

func validateCodexOfficialClientProfile(profile officialclient.Profile, authID string, payload []byte) (*officialclient.CodexProfile, error) {
	if profile.Provider != officialclient.ProviderCodex || profile.Codex == nil || !profile.Active {
		return nil, errors.New("Codex official client profile is invalid or inactive")
	}
	if strings.TrimSpace(authID) == "" {
		return nil, errors.New("Codex official client profile requires an auth ID")
	}
	trimmed := bytes.TrimSpace(payload)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return nil, errors.New("Codex official client profile requires a JSON object body")
	}
	return profile.Codex, nil
}

func ensureCodexJSONEOF(decoder *json.Decoder) error {
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return errors.New("Codex official client body contains trailing JSON")
		}
		return fmt.Errorf("decode Codex official client trailing data: %w", err)
	}
	return nil
}

func newCodexOfficialClientRequestState(profile officialclient.Profile, authID string, body map[string]any, headers http.Header, metadata map[string]any) (*CodexOfficialClientRequestState, error) {
	discriminator := metadataStringValue(metadata, cliproxyexecutor.ExecutionSessionMetadataKey)
	if discriminator == "" && headers != nil {
		for _, name := range []string{"Session-Id", "Session_id", "X-Client-Request-Id"} {
			if discriminator = strings.TrimSpace(headers.Get(name)); discriminator != "" {
				break
			}
		}
	}
	if discriminator == "" {
		discriminator = firstCodexOfficialClientUserText(body)
	}
	if discriminator == "" {
		discriminator = "default"
	}

	authID = strings.TrimSpace(authID)
	installationID := deterministicOfficialClientUUID("cliproxyapi", profile.ID, authID, string(profile.Provider), "installation")
	sessionID := deterministicOfficialClientUUID("cliproxyapi", profile.ID, authID, string(profile.Provider), "session", discriminator)
	turnID := uuid.NewString()
	windowID := sessionID + ":0"
	startedAt := time.Now().UnixMilli()
	turnMetadata := map[string]any{
		"installation_id":         installationID,
		"session_id":              sessionID,
		"thread_id":               sessionID,
		"turn_id":                 turnID,
		"turn_started_at_unix_ms": startedAt,
		"window_id":               windowID,
	}
	for name, value := range profile.Codex.TurnMetadataFixedValues {
		turnMetadata[name] = value
	}
	encodedMetadata, err := json.Marshal(turnMetadata)
	if err != nil {
		return nil, fmt.Errorf("encode Codex turn metadata: %w", err)
	}
	return &CodexOfficialClientRequestState{
		InstallationID:      installationID,
		SessionID:           sessionID,
		ThreadID:            sessionID,
		TurnID:              turnID,
		WindowID:            windowID,
		TurnStartedAtUnixMS: startedAt,
		TurnMetadata:        string(encodedMetadata),
	}, nil
}

func deterministicOfficialClientUUID(parts ...string) string {
	return uuid.NewSHA1(uuid.NameSpaceOID, []byte(strings.Join(parts, "\x00"))).String()
}

func ensureCodexOfficialClientIncludes(body map[string]any, required []string) {
	existing, ok := body["include"].([]any)
	if !ok {
		existing = nil
	}
	seen := make(map[string]struct{}, len(existing)+len(required))
	for _, value := range existing {
		if name, ok := value.(string); ok {
			seen[name] = struct{}{}
		}
	}
	for _, name := range required {
		if _, ok := seen[name]; ok {
			continue
		}
		existing = append(existing, name)
		seen[name] = struct{}{}
	}
	body["include"] = existing
}

func ensureCodexOfficialClientNestedDefault(body map[string]any, objectName string, fieldName string, value string) {
	if strings.TrimSpace(value) == "" {
		return
	}
	nested, ok := body[objectName].(map[string]any)
	if !ok {
		if body[objectName] != nil {
			return
		}
		nested = make(map[string]any)
		body[objectName] = nested
	}
	if existing, ok := nested[fieldName].(string); ok && strings.TrimSpace(existing) != "" {
		return
	}
	nested[fieldName] = value
}

func normalizeCodexOfficialClientInput(body map[string]any) {
	input, ok := body["input"].([]any)
	if !ok {
		return
	}
	for index, item := range input {
		message, ok := item.(map[string]any)
		if !ok {
			continue
		}
		role, _ := message["role"].(string)
		if strings.EqualFold(strings.TrimSpace(role), "system") {
			message["role"] = "developer"
		}
		if strings.TrimSpace(role) == "" {
			continue
		}
		if itemType, _ := message["type"].(string); strings.TrimSpace(itemType) != "" {
			continue
		}
		if _, ok := message["content"]; !ok {
			continue
		}
		cloned := make(map[string]any, len(message)+1)
		for name, value := range message {
			cloned[name] = value
		}
		cloned["type"] = "message"
		if text, ok := cloned["content"].(string); ok {
			cloned["content"] = []any{map[string]any{"type": "input_text", "text": text}}
		}
		input[index] = cloned
	}
	body["input"] = input
}

func firstCodexOfficialClientUserText(body map[string]any) string {
	if inputText, ok := body["input"].(string); ok {
		return strings.Join(strings.Fields(inputText), " ")
	}
	input, ok := body["input"].([]any)
	if !ok {
		return ""
	}
	for _, item := range input {
		message, ok := item.(map[string]any)
		if !ok || !strings.EqualFold(strings.TrimSpace(fmt.Sprint(message["role"])), "user") {
			continue
		}
		switch content := message["content"].(type) {
		case string:
			return strings.Join(strings.Fields(content), " ")
		case []any:
			for _, part := range content {
				block, ok := part.(map[string]any)
				if !ok {
					continue
				}
				text, _ := block["text"].(string)
				if strings.TrimSpace(text) != "" {
					return strings.Join(strings.Fields(text), " ")
				}
			}
		}
	}
	return ""
}
