package helps

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/officialclient"
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/executor"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

type ClaudeOfficialClientEndpoint string

const (
	ClaudeOfficialClientMessagesStream    ClaudeOfficialClientEndpoint = "messages_stream"
	ClaudeOfficialClientMessagesNonStream ClaudeOfficialClientEndpoint = "messages_non_stream"
	ClaudeOfficialClientCountTokens       ClaudeOfficialClientEndpoint = "count_tokens"
	claudeFingerprintSalt                                              = "59cf53e54c78"
)

type ClaudeOfficialClientRequestState struct {
	SessionID        string
	reverseToolNames map[string]string
}

type officialClientRequestError struct {
	err error
}

func (e *officialClientRequestError) Error() string {
	return e.err.Error()
}

func (e *officialClientRequestError) Unwrap() error {
	return e.err
}

func (e *officialClientRequestError) IsRequestScoped() bool {
	return true
}

func NewOfficialClientRequestError(err error) error {
	if err == nil {
		return nil
	}
	var requestScoped interface{ IsRequestScoped() bool }
	if errors.As(err, &requestScoped) && requestScoped.IsRequestScoped() {
		return err
	}
	return &officialClientRequestError{err: err}
}

func OfficialClientConnectivityTest(metadata map[string]any) bool {
	if len(metadata) == 0 {
		return false
	}
	raw, ok := metadata[cliproxyexecutor.ConnectivityTestMetadataKey]
	if !ok || raw == nil {
		return false
	}
	switch value := raw.(type) {
	case bool:
		return value
	case string:
		parsed, err := strconv.ParseBool(strings.TrimSpace(value))
		return err == nil && parsed
	case []byte:
		parsed, err := strconv.ParseBool(strings.TrimSpace(string(value)))
		return err == nil && parsed
	default:
		return false
	}
}

func ApplyClaudeOfficialClientMessagesProfile(profile officialclient.Profile, authID string, payload []byte, stream bool, headers http.Header, metadata map[string]any) ([]byte, *ClaudeOfficialClientRequestState, error) {
	claude, err := validateClaudeOfficialClientProfile(profile, authID, payload)
	if err != nil {
		return nil, nil, NewOfficialClientRequestError(err)
	}
	state := newClaudeOfficialClientRequestState(profile, authID, payload, headers, metadata)
	if stream {
		payload, err = sjson.SetBytes(payload, "stream", true)
	} else {
		payload, err = sjson.DeleteBytes(payload, "stream")
	}
	if err != nil {
		return nil, nil, NewOfficialClientRequestError(fmt.Errorf("apply Claude stream policy: %w", err))
	}
	if isPlainAutoToolChoice(payload) {
		payload, err = sjson.DeleteBytes(payload, "tool_choice")
		if err != nil {
			return nil, nil, NewOfficialClientRequestError(fmt.Errorf("remove Claude auto tool choice: %w", err))
		}
	}
	userID := claudeOfficialClientUserID(profile, authID, state.SessionID)
	payload, err = sjson.SetBytes(payload, "metadata.user_id", userID)
	if err != nil {
		return nil, nil, NewOfficialClientRequestError(fmt.Errorf("set Claude metadata identity: %w", err))
	}
	payload, err = applyClaudeOfficialClientSystem(payload, profile, claude)
	if err != nil {
		return nil, nil, NewOfficialClientRequestError(err)
	}
	payload, state.reverseToolNames = remapClaudeOfficialClientToolNames(payload, claude.ToolNameMap)
	return payload, state, nil
}

func ApplyClaudeOfficialClientCountTokensProfile(profile officialclient.Profile, authID string, payload []byte, headers http.Header, metadata map[string]any) ([]byte, *ClaudeOfficialClientRequestState, error) {
	claude, err := validateClaudeOfficialClientProfile(profile, authID, payload)
	if err != nil {
		return nil, nil, NewOfficialClientRequestError(err)
	}
	state := newClaudeOfficialClientRequestState(profile, authID, payload, headers, metadata)
	for _, path := range []string{"temperature", "top_p", "top_k", "stream", "stop_sequences", "stop"} {
		payload, err = sjson.DeleteBytes(payload, path)
		if err != nil {
			return nil, nil, NewOfficialClientRequestError(fmt.Errorf("remove Claude count_tokens field %q: %w", path, err))
		}
	}
	payload, state.reverseToolNames = remapClaudeOfficialClientToolNames(payload, claude.ToolNameMap)
	return payload, state, nil
}

func ClaudeOfficialClientHeaders(profile officialclient.Profile, endpoint ClaudeOfficialClientEndpoint, apiKey string, body []byte, state *ClaudeOfficialClientRequestState) (map[string]string, error) {
	if strings.TrimSpace(apiKey) == "" {
		return nil, NewOfficialClientRequestError(errors.New("Claude official client profile requires an API key"))
	}
	if state == nil || strings.TrimSpace(state.SessionID) == "" {
		return nil, NewOfficialClientRequestError(errors.New("Claude official client profile requires request state"))
	}
	claude, err := validateClaudeOfficialClientProfile(profile, "header", body)
	if err != nil {
		return nil, NewOfficialClientRequestError(err)
	}
	desired := make(map[string]string, len(claude.FixedHeaders)+len(claude.MessagesHeaders)+4)
	for key, value := range claude.FixedHeaders {
		desired[key] = value
	}
	desired["x-api-key"] = apiKey
	desired["X-Claude-Code-Session-Id"] = state.SessionID

	var betas []string
	switch endpoint {
	case ClaudeOfficialClientMessagesStream:
		desired["Authorization"] = "Bearer " + apiKey
		betas = append(betas, claude.MessagesStreamBetas...)
		for key, value := range claude.MessagesHeaders {
			desired[key] = value
		}
	case ClaudeOfficialClientMessagesNonStream:
		desired["Authorization"] = "Bearer " + apiKey
		betas = append(betas, claude.MessagesNonStreamBetas...)
		for key, value := range claude.MessagesHeaders {
			desired[key] = value
		}
	case ClaudeOfficialClientCountTokens:
		betas = append(betas, claude.CountTokensBetas...)
	default:
		return nil, NewOfficialClientRequestError(fmt.Errorf("unsupported Claude official client endpoint %q", endpoint))
	}
	if endpoint != ClaudeOfficialClientCountTokens && gjson.GetBytes(body, "output_config.format.type").String() == "json_schema" {
		// 结构化输出沿用官方客户端的尾部替换规则，不能同时发送 fallback beta。
		if len(betas) == 0 {
			betas = append(betas, claude.StructuredOutputsBeta)
		} else {
			betas[len(betas)-1] = claude.StructuredOutputsBeta
		}
	}
	desired["Anthropic-Beta"] = strings.Join(betas, ",")
	return desired, nil
}

func (state *ClaudeOfficialClientRequestState) RestoreJSON(body []byte) []byte {
	if state == nil || len(state.reverseToolNames) == 0 || !gjson.ValidBytes(body) {
		return body
	}
	content := gjson.GetBytes(body, "content")
	if !content.IsArray() {
		return body
	}
	content.ForEach(func(index, part gjson.Result) bool {
		switch part.Get("type").String() {
		case "tool_use":
			if original, ok := state.reverseToolNames[part.Get("name").String()]; ok {
				body, _ = sjson.SetBytes(body, fmt.Sprintf("content.%d.name", index.Int()), original)
			}
		case "tool_reference":
			if original, ok := state.reverseToolNames[part.Get("tool_name").String()]; ok {
				body, _ = sjson.SetBytes(body, fmt.Sprintf("content.%d.tool_name", index.Int()), original)
			}
		}
		return true
	})
	return body
}

func (state *ClaudeOfficialClientRequestState) RestoreSSELine(line []byte) []byte {
	if state == nil || len(state.reverseToolNames) == 0 {
		return line
	}
	payload := JSONPayload(line)
	if len(payload) == 0 || !gjson.ValidBytes(payload) {
		return line
	}
	block := gjson.GetBytes(payload, "content_block")
	if !block.IsObject() {
		return line
	}
	var path string
	var current string
	switch block.Get("type").String() {
	case "tool_use":
		path = "content_block.name"
		current = block.Get("name").String()
	case "tool_reference":
		path = "content_block.tool_name"
		current = block.Get("tool_name").String()
	default:
		return line
	}
	original, ok := state.reverseToolNames[current]
	if !ok {
		return line
	}
	updated, err := sjson.SetBytes(payload, path, original)
	if err != nil {
		return line
	}
	if bytes.HasPrefix(bytes.TrimSpace(line), []byte("data:")) {
		return append([]byte("data: "), updated...)
	}
	return updated
}

func (state *ClaudeOfficialClientRequestState) RestoreSSE(payload []byte) []byte {
	lines := bytes.Split(payload, []byte("\n"))
	for index := range lines {
		lines[index] = state.RestoreSSELine(lines[index])
	}
	return bytes.Join(lines, []byte("\n"))
}

func validateClaudeOfficialClientProfile(profile officialclient.Profile, authID string, payload []byte) (*officialclient.ClaudeProfile, error) {
	if profile.Provider != officialclient.ProviderClaude || profile.Claude == nil || !profile.Active {
		return nil, errors.New("Claude official client profile is invalid or inactive")
	}
	if strings.TrimSpace(authID) == "" {
		return nil, errors.New("Claude official client profile requires an auth ID")
	}
	trimmed := bytes.TrimSpace(payload)
	if !gjson.ValidBytes(payload) || len(trimmed) == 0 || trimmed[0] != '{' {
		return nil, errors.New("Claude official client profile requires a JSON object body")
	}
	return profile.Claude, nil
}

func newClaudeOfficialClientRequestState(profile officialclient.Profile, authID string, payload []byte, headers http.Header, metadata map[string]any) *ClaudeOfficialClientRequestState {
	discriminator := metadataStringValue(metadata, cliproxyexecutor.ExecutionSessionMetadataKey)
	if discriminator == "" && headers != nil {
		discriminator = strings.TrimSpace(headers.Get("X-Claude-Code-Session-Id"))
	}
	if discriminator == "" {
		discriminator = firstClaudeUserMessageText(payload)
	}
	if discriminator == "" {
		discriminator = "default"
	}
	name := strings.Join([]string{"cliproxyapi", profile.ID, strings.TrimSpace(authID), "session", discriminator}, "\x00")
	return &ClaudeOfficialClientRequestState{SessionID: uuid.NewSHA1(uuid.NameSpaceOID, []byte(name)).String()}
}

func claudeOfficialClientUserID(profile officialclient.Profile, authID string, sessionID string) string {
	authID = strings.TrimSpace(authID)
	deviceSeed := strings.Join([]string{"cliproxyapi", profile.ID, authID, "device"}, "\x00")
	deviceHash := sha256.Sum256([]byte(deviceSeed))
	value, err := json.Marshal(map[string]string{
		"device_id":    hex.EncodeToString(deviceHash[:]),
		"account_uuid": "",
		"session_id":   sessionID,
	})
	if err != nil {
		return ""
	}
	return string(value)
}

func metadataStringValue(metadata map[string]any, key string) string {
	if len(metadata) == 0 {
		return ""
	}
	switch value := metadata[key].(type) {
	case string:
		return strings.TrimSpace(value)
	case []byte:
		return strings.TrimSpace(string(value))
	default:
		return ""
	}
}

func firstClaudeUserMessageText(payload []byte) string {
	messages := gjson.GetBytes(payload, "messages")
	if !messages.IsArray() {
		return ""
	}
	text := ""
	messages.ForEach(func(_, message gjson.Result) bool {
		if message.Get("role").String() != "user" {
			return true
		}
		content := message.Get("content")
		if content.Type == gjson.String {
			text = content.String()
			return false
		}
		if content.IsArray() {
			content.ForEach(func(_, part gjson.Result) bool {
				if part.Type == gjson.String {
					text = part.String()
					return false
				}
				if part.Get("type").String() == "text" {
					text = part.Get("text").String()
					return false
				}
				return true
			})
		}
		return text == ""
	})
	return strings.Join(strings.Fields(text), " ")
}

func isPlainAutoToolChoice(payload []byte) bool {
	choice := gjson.GetBytes(payload, "tool_choice")
	if !choice.IsObject() || choice.Get("type").String() != "auto" {
		return false
	}
	return len(choice.Map()) == 1
}

func applyClaudeOfficialClientSystem(payload []byte, profile officialclient.Profile, claude *officialclient.ClaudeProfile) ([]byte, error) {
	system := gjson.GetBytes(payload, "system")
	kept := make([]json.RawMessage, 0)
	fingerprintText := firstClaudeUserMessageText(payload)
	appendBlock := func(raw json.RawMessage) error {
		normalized, text, hasText, err := normalizeClaudeSystemBlock(raw)
		if err != nil {
			return err
		}
		if hasText {
			trimmed := strings.TrimSpace(text)
			if strings.HasPrefix(trimmed, "x-anthropic-billing-header:") || trimmed == strings.TrimSpace(claude.IdentitySystemText) {
				return nil
			}
		}
		kept = append(kept, normalized)
		return nil
	}

	if system.Exists() && system.Type != gjson.Null {
		switch {
		case system.Type == gjson.String:
			block, err := json.Marshal(map[string]any{"type": "text", "text": system.String()})
			if err != nil {
				return nil, fmt.Errorf("encode Claude system text: %w", err)
			}
			if err := appendBlock(block); err != nil {
				return nil, err
			}
		case system.IsArray():
			var blocks []json.RawMessage
			if err := json.Unmarshal([]byte(system.Raw), &blocks); err != nil {
				return nil, fmt.Errorf("decode Claude system blocks: %w", err)
			}
			for _, block := range blocks {
				if err := appendBlock(block); err != nil {
					return nil, err
				}
			}
		default:
			return nil, errors.New("Claude system must be a string or array")
		}
	}

	billingText := fmt.Sprintf(
		"x-anthropic-billing-header: cc_version=%s.%s; cc_entrypoint=%s;",
		profile.ClientVersion,
		claudeOfficialClientFingerprint(fingerprintText, profile.ClientVersion),
		claude.Entrypoint,
	)
	billingBlock, err := json.Marshal(map[string]any{"type": "text", "text": billingText})
	if err != nil {
		return nil, fmt.Errorf("encode Claude billing block: %w", err)
	}
	identityBlock, err := json.Marshal(map[string]any{"type": "text", "text": claude.IdentitySystemText})
	if err != nil {
		return nil, fmt.Errorf("encode Claude identity block: %w", err)
	}
	blocks := append([]json.RawMessage{billingBlock, identityBlock}, kept...)
	encoded, err := json.Marshal(blocks)
	if err != nil {
		return nil, fmt.Errorf("encode Claude system blocks: %w", err)
	}
	payload, err = sjson.SetRawBytes(payload, "system", encoded)
	if err != nil {
		return nil, fmt.Errorf("set Claude system blocks: %w", err)
	}
	return payload, nil
}

func normalizeClaudeSystemBlock(raw json.RawMessage) (json.RawMessage, string, bool, error) {
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		block, marshalErr := json.Marshal(map[string]any{"type": "text", "text": text})
		if marshalErr != nil {
			return nil, "", false, marshalErr
		}
		return block, text, true, nil
	}
	var block map[string]json.RawMessage
	if err := json.Unmarshal(raw, &block); err != nil {
		return nil, "", false, fmt.Errorf("decode Claude system block: %w", err)
	}
	textRaw, ok := block["text"]
	if !ok {
		return raw, "", false, nil
	}
	if err := json.Unmarshal(textRaw, &text); err != nil {
		return nil, "", false, fmt.Errorf("decode Claude system block text: %w", err)
	}
	return raw, text, true, nil
}

func claudeOfficialClientFingerprint(messageText string, version string) string {
	indices := [3]int{4, 7, 20}
	runes := []rune(messageText)
	var selected strings.Builder
	for _, index := range indices {
		if index < len(runes) {
			selected.WriteRune(runes[index])
		} else {
			selected.WriteRune('0')
		}
	}
	sum := sha256.Sum256([]byte(claudeFingerprintSalt + selected.String() + version))
	return hex.EncodeToString(sum[:])[:3]
}

func remapClaudeOfficialClientToolNames(body []byte, mapping map[string]string) ([]byte, map[string]string) {
	reverse := make(map[string]string)
	record := func(original string) (string, bool) {
		renamed, ok := mapping[original]
		if !ok || renamed == original {
			return original, false
		}
		if _, exists := reverse[renamed]; !exists {
			reverse[renamed] = original
		}
		return renamed, true
	}

	tools := gjson.GetBytes(body, "tools")
	if tools.IsArray() {
		tools.ForEach(func(index, tool gjson.Result) bool {
			toolType := tool.Get("type").String()
			if toolType != "" && toolType != "function" && toolType != "custom" {
				return true
			}
			if renamed, ok := record(tool.Get("name").String()); ok {
				body, _ = sjson.SetBytes(body, fmt.Sprintf("tools.%d.name", index.Int()), renamed)
			}
			return true
		})
	}
	if gjson.GetBytes(body, "tool_choice.type").String() == "tool" {
		if renamed, ok := record(gjson.GetBytes(body, "tool_choice.name").String()); ok {
			body, _ = sjson.SetBytes(body, "tool_choice.name", renamed)
		}
	}
	messages := gjson.GetBytes(body, "messages")
	if messages.IsArray() {
		messages.ForEach(func(messageIndex, message gjson.Result) bool {
			content := message.Get("content")
			if !content.IsArray() {
				return true
			}
			content.ForEach(func(contentIndex, part gjson.Result) bool {
				switch part.Get("type").String() {
				case "tool_use":
					if renamed, ok := record(part.Get("name").String()); ok {
						body, _ = sjson.SetBytes(body, fmt.Sprintf("messages.%d.content.%d.name", messageIndex.Int(), contentIndex.Int()), renamed)
					}
				case "tool_reference":
					if renamed, ok := record(part.Get("tool_name").String()); ok {
						body, _ = sjson.SetBytes(body, fmt.Sprintf("messages.%d.content.%d.tool_name", messageIndex.Int(), contentIndex.Int()), renamed)
					}
				case "tool_result":
					nested := part.Get("content")
					if nested.IsArray() {
						nested.ForEach(func(nestedIndex, nestedPart gjson.Result) bool {
							if nestedPart.Get("type").String() == "tool_reference" {
								if renamed, ok := record(nestedPart.Get("tool_name").String()); ok {
									body, _ = sjson.SetBytes(body, fmt.Sprintf("messages.%d.content.%d.content.%d.tool_name", messageIndex.Int(), contentIndex.Int(), nestedIndex.Int()), renamed)
								}
							}
							return true
						})
					}
				}
				return true
			})
			return true
		})
	}
	return body, reverse
}
