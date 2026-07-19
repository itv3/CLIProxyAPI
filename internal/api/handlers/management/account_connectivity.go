package management

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/interfaces"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/util"
	sdkhandlers "github.com/router-for-me/CLIProxyAPI/v7/sdk/api/handlers"
	coreauth "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/auth"
	"github.com/tidwall/gjson"
)

const (
	accountTestModeDefault = "default"
	accountTestModeCompact = "compact"
	accountTestPrompt      = "hi"
)

var claudeOfficialClientTestToolNames = []string{
	"Agent", "Bash", "CronCreate", "CronDelete", "CronList", "DesignSync", "Edit",
	"EnterWorktree", "ExitWorktree", "Monitor", "NotebookEdit", "PushNotification",
	"Read", "ReportFindings", "ScheduleWakeup", "SendMessage", "Skill", "TaskCreate",
	"TaskGet", "TaskList", "TaskOutput", "TaskStop", "TaskUpdate", "WebFetch",
	"WebSearch", "Workflow", "Write",
}

func claudeOfficialClientTestTools() []map[string]any {
	tools := make([]map[string]any, 0, len(claudeOfficialClientTestToolNames))
	for _, name := range claudeOfficialClientTestToolNames {
		tools = append(tools, map[string]any{
			"name":        name,
			"description": "Use " + name + " during the account connectivity test.",
			"input_schema": map[string]any{
				"type":       "object",
				"properties": map[string]any{},
			},
		})
	}
	return tools
}

var accountTestSecretPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)(?:bearer\s+)?sk-[a-z0-9_-]{8,}`),
	regexp.MustCompile(`AIza[a-zA-Z0-9_-]{20,}`),
}

type accountTestModelExecutor interface {
	ExecuteProtocolWithAuthManager(context.Context, sdkhandlers.ProtocolExecutionRequest) (sdkhandlers.ModelExecutionResponse, *interfaces.ErrorMessage)
	ExecuteProtocolStreamWithAuthManager(context.Context, sdkhandlers.ProtocolExecutionRequest) (sdkhandlers.ModelExecutionStream, *interfaces.ErrorMessage)
}

type accountTestRequest struct {
	AuthIndex string `json:"auth_index"`
	Model     string `json:"model"`
	Protocol  string `json:"protocol"`
	Mode      string `json:"mode"`
}

type accountTestResponse struct {
	Success         bool   `json:"success"`
	StatusCode      int    `json:"status_code,omitempty"`
	Protocol        string `json:"protocol"`
	Mode            string `json:"mode"`
	Model           string `json:"model"`
	UpstreamModel   string `json:"upstream_model,omitempty"`
	DurationMS      int64  `json:"duration_ms"`
	ResponsePreview string `json:"response_preview,omitempty"`
	ErrorCode       string `json:"error_code,omitempty"`
	ErrorMessage    string `json:"error_message,omitempty"`
	Retryable       bool   `json:"retryable"`
}

func (h *Handler) SetModelExecutor(executor accountTestModelExecutor) {
	if h == nil {
		return
	}
	h.modelExecutor = executor
}

// AccountTest 固定指定认证，并通过生产执行器发送一个最小模型请求。
func (h *Handler) AccountTest(c *gin.Context) {
	if h == nil || h.modelExecutor == nil || h.authManager == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "account test executor is unavailable"})
		return
	}

	var body accountTestRequest
	if errBind := c.ShouldBindJSON(&body); errBind != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid body"})
		return
	}
	body.AuthIndex = strings.TrimSpace(body.AuthIndex)
	body.Model = strings.TrimSpace(body.Model)
	body.Protocol = strings.ToLower(strings.TrimSpace(body.Protocol))
	body.Mode = strings.ToLower(strings.TrimSpace(body.Mode))
	if body.Mode == "" {
		body.Mode = accountTestModeDefault
	}
	if body.AuthIndex == "" || body.Model == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "auth_index and model are required"})
		return
	}
	if body.Mode != accountTestModeDefault && body.Mode != accountTestModeCompact {
		c.JSON(http.StatusBadRequest, gin.H{"error": "unsupported test mode"})
		return
	}

	auth := h.authByIndex(body.AuthIndex)
	if auth == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "auth not found"})
		return
	}
	request, protocol, errRequest := buildAccountTestExecution(body, accountTestProviderKey(auth))
	if errRequest != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": errRequest.Error()})
		return
	}

	startedAt := time.Now()
	testCtx := sdkhandlers.WithPinnedAuthID(c.Request.Context(), auth.ID)
	testCtx = sdkhandlers.WithAccountConnectivityTest(testCtx)
	result, errMessage := executeAccountTest(testCtx, h.modelExecutor, request)
	durationMS := time.Since(startedAt).Milliseconds()
	if errMessage != nil {
		statusCode := errMessage.StatusCode
		if statusCode <= 0 {
			statusCode = http.StatusBadGateway
		}
		message := "account connectivity test failed"
		if errMessage.Error != nil {
			message = safeAccountTestText(errMessage.Error.Error(), 512)
		}
		code, retryable := classifyAccountTestFailure(statusCode, message, protocol)
		c.JSON(http.StatusOK, accountTestResponse{
			Success: false, StatusCode: statusCode, Protocol: protocol, Mode: body.Mode,
			Model: body.Model, DurationMS: durationMS, ErrorCode: code,
			ErrorMessage: message, Retryable: retryable,
		})
		return
	}

	responsePreview := accountTestResponsePreview(result.Body)
	if responsePreview == "" && body.Mode == accountTestModeCompact {
		responsePreview = "Compact probe succeeded"
	}
	c.JSON(http.StatusOK, accountTestResponse{
		Success: true, StatusCode: result.StatusCode, Protocol: protocol, Mode: body.Mode,
		Model: body.Model, UpstreamModel: accountTestUpstreamModel(result.Body),
		DurationMS: durationMS, ResponsePreview: responsePreview,
	})
}

// executeAccountTest 根据请求形态选择执行路径，并把流式事件汇总为可解析的响应体。
func executeAccountTest(ctx context.Context, executor accountTestModelExecutor, request sdkhandlers.ProtocolExecutionRequest) (sdkhandlers.ModelExecutionResponse, *interfaces.ErrorMessage) {
	if !request.Stream {
		return executor.ExecuteProtocolWithAuthManager(ctx, request)
	}
	streamCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	stream, errMessage := executor.ExecuteProtocolStreamWithAuthManager(streamCtx, request)
	if errMessage != nil {
		return sdkhandlers.ModelExecutionResponse{}, errMessage
	}
	var body bytes.Buffer
	for chunk := range stream.Chunks {
		if chunk.Err != nil {
			return sdkhandlers.ModelExecutionResponse{}, &interfaces.ErrorMessage{
				StatusCode: chunk.Err.StatusCode,
				Error:      errors.New(chunk.Err.Error()),
			}
		}
		body.Write(chunk.Payload)
		if accountTestStreamCompleted(body.Bytes()) {
			break
		}
	}
	return sdkhandlers.ModelExecutionResponse{
		StatusCode: stream.StatusCode,
		Headers:    stream.Headers,
		Body:       body.Bytes(),
	}, nil
}

func accountTestStreamCompleted(raw []byte) bool {
	for _, event := range accountTestSSEEvents(raw) {
		switch gjson.GetBytes(event, "type").String() {
		case "message_stop", "response.completed":
			return true
		}
	}
	return false
}

func buildAccountTestExecution(input accountTestRequest, provider string) (sdkhandlers.ProtocolExecutionRequest, string, error) {
	if provider == "" {
		return sdkhandlers.ProtocolExecutionRequest{}, "", &coreauth.Error{Code: "executor_not_found", Message: "account provider is unavailable"}
	}
	protocol := input.Protocol
	if input.Mode == accountTestModeCompact {
		protocol = "responses"
	}

	var payload any
	entryProtocol := ""
	alt := ""
	switch protocol {
	case "chat_completions":
		entryProtocol = "openai"
		payload = map[string]any{
			"model": input.Model, "messages": []map[string]string{{"role": "user", "content": accountTestPrompt}},
			"stream": false,
		}
	case "responses":
		entryProtocol = "openai-response"
		if input.Mode == accountTestModeCompact {
			alt = "responses/compact"
			payload = map[string]any{
				"model": input.Model, "instructions": "You are a helpful coding assistant.",
				"input": []any{map[string]any{"type": "message", "role": "user", "content": accountTestPrompt}},
			}
		} else {
			payload = map[string]any{
				"model": input.Model, "input": accountTestPrompt, "stream": false,
			}
		}
	case "messages":
		entryProtocol = "claude"
		payload = map[string]any{
			"model": input.Model,
			"messages": []map[string]any{{
				"role":    "user",
				"content": []map[string]any{{"type": "text", "text": accountTestPrompt}},
			}},
			"system":     "Account connectivity test",
			"max_tokens": 512,
			"tools":      claudeOfficialClientTestTools(),
			"stream":     true,
		}
	case "generate_content":
		entryProtocol = "gemini"
		payload = map[string]any{
			"contents": []map[string]any{{"role": "user", "parts": []map[string]string{{"text": accountTestPrompt}}}},
		}
	default:
		return sdkhandlers.ProtocolExecutionRequest{}, "", &coreauth.Error{Code: "protocol_not_supported", Message: "unsupported account test protocol"}
	}

	raw, errMarshal := json.Marshal(payload)
	if errMarshal != nil {
		return sdkhandlers.ProtocolExecutionRequest{}, "", errMarshal
	}
	displayProtocol := protocol
	if alt == "responses/compact" {
		displayProtocol = "responses_compact"
	}
	return sdkhandlers.ProtocolExecutionRequest{
		EntryProtocol: entryProtocol, ExitProtocol: entryProtocol, ForcedProvider: provider,
		AuthSelectionModel: input.Model, Model: input.Model, Body: raw, Alt: alt,
		Stream: protocol == "messages",
	}, displayProtocol, nil
}

func accountTestProviderKey(auth *coreauth.Auth) string {
	if auth == nil {
		return ""
	}
	if auth.Attributes != nil {
		compatName := strings.TrimSpace(auth.Attributes["compat_name"])
		if compatName != "" {
			providerKey := strings.TrimSpace(auth.Attributes["provider_key"])
			if providerKey == "" {
				providerKey = compatName
			}
			return util.OpenAICompatibleProviderKey(providerKey)
		}
	}
	if strings.EqualFold(strings.TrimSpace(auth.Provider), "openai-compatibility") {
		providerKey := strings.TrimSpace(auth.Label)
		if providerKey == "" {
			providerKey = "openai-compatibility"
		}
		return util.OpenAICompatibleProviderKey(providerKey)
	}
	return strings.ToLower(strings.TrimSpace(auth.Provider))
}

func classifyAccountTestFailure(statusCode int, message string, protocol string) (string, bool) {
	lowerMessage := strings.ToLower(message)
	switch {
	case statusCode == http.StatusUnauthorized || statusCode == http.StatusForbidden:
		return "authentication_failed", false
	case statusCode == http.StatusPaymentRequired:
		return "quota_exhausted", false
	case statusCode == http.StatusMethodNotAllowed || statusCode == http.StatusNotImplemented:
		return "protocol_not_supported", false
	case statusCode == http.StatusNotFound:
		if protocol == "responses_compact" || strings.Contains(lowerMessage, "endpoint") || strings.Contains(lowerMessage, "route") || strings.Contains(lowerMessage, "method") {
			return "protocol_not_supported", false
		}
		return "model_unavailable", false
	case statusCode == http.StatusTooManyRequests:
		return "rate_limited", true
	case statusCode >= http.StatusInternalServerError:
		return "upstream_unavailable", true
	}
	if strings.Contains(lowerMessage, "model") && (strings.Contains(lowerMessage, "not found") || strings.Contains(lowerMessage, "unsupported") || strings.Contains(lowerMessage, "unavailable")) {
		return "model_unavailable", false
	}
	if strings.Contains(lowerMessage, "auth_not_found") || strings.Contains(lowerMessage, "no auth available") {
		return "account_unavailable", false
	}
	if strings.Contains(lowerMessage, "executor_not_found") {
		return "executor_unavailable", false
	}
	return "connectivity_test_failed", false
}

func accountTestUpstreamModel(raw []byte) string {
	for _, path := range []string{"model", "response.model"} {
		if value := strings.TrimSpace(gjson.GetBytes(raw, path).String()); value != "" {
			return safeAccountTestText(value, 256)
		}
	}
	for _, event := range accountTestSSEEvents(raw) {
		for _, path := range []string{"message.model", "model"} {
			if value := strings.TrimSpace(gjson.GetBytes(event, path).String()); value != "" {
				return safeAccountTestText(value, 256)
			}
		}
	}
	return ""
}

func accountTestResponsePreview(raw []byte) string {
	for _, path := range []string{
		"output_text", "choices.0.message.content", "output.#(type==\"message\").content.0.text",
		"output.0.content.0.text", "content.0.text",
		"candidates.0.content.parts.0.text", "output.0.content",
	} {
		if value := strings.TrimSpace(gjson.GetBytes(raw, path).String()); value != "" {
			return safeAccountTestText(value, 512)
		}
	}
	var streamedText strings.Builder
	for _, event := range accountTestSSEEvents(raw) {
		for _, path := range []string{"delta.text", "content_block.text"} {
			if value := gjson.GetBytes(event, path).String(); value != "" {
				streamedText.WriteString(value)
			}
		}
	}
	if value := strings.TrimSpace(streamedText.String()); value != "" {
		return safeAccountTestText(value, 512)
	}
	return ""
}

func accountTestSSEEvents(raw []byte) [][]byte {
	events := make([][]byte, 0)
	for _, line := range bytes.Split(raw, []byte("\n")) {
		line = bytes.TrimSpace(line)
		if !bytes.HasPrefix(line, []byte("data:")) {
			continue
		}
		data := bytes.TrimSpace(bytes.TrimPrefix(line, []byte("data:")))
		if len(data) > 0 && !bytes.Equal(data, []byte("[DONE]")) && gjson.ValidBytes(data) {
			events = append(events, bytes.Clone(data))
		}
	}
	return events
}

func safeAccountTestText(value string, limit int) string {
	value = strings.Map(func(r rune) rune {
		if r == '\n' || r == '\t' || r >= 0x20 {
			return r
		}
		return -1
	}, strings.TrimSpace(value))
	for _, pattern := range accountTestSecretPatterns {
		value = pattern.ReplaceAllString(value, "[REDACTED]")
	}
	runes := []rune(value)
	if limit > 0 && len(runes) > limit {
		value = string(runes[:limit])
	}
	return value
}
