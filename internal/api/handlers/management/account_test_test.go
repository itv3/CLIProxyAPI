package management

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/interfaces"
	sdkhandlers "github.com/router-for-me/CLIProxyAPI/v7/sdk/api/handlers"
	coreauth "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/auth"
	"github.com/tidwall/gjson"
)

type accountTestExecutorStub struct {
	request      sdkhandlers.ProtocolExecutionRequest
	responseBody []byte
}

func (s *accountTestExecutorStub) ExecuteProtocolWithAuthManager(_ context.Context, request sdkhandlers.ProtocolExecutionRequest) (sdkhandlers.ModelExecutionResponse, *interfaces.ErrorMessage) {
	s.request = request
	responseBody := s.responseBody
	if responseBody == nil {
		responseBody = []byte(`{"model":"gpt-5","output_text":"OK"}`)
	}
	return sdkhandlers.ModelExecutionResponse{StatusCode: http.StatusOK, Body: responseBody}, nil
}

func (s *accountTestExecutorStub) ExecuteProtocolStreamWithAuthManager(_ context.Context, request sdkhandlers.ProtocolExecutionRequest) (sdkhandlers.ModelExecutionStream, *interfaces.ErrorMessage) {
	s.request = request
	chunks := make(chan sdkhandlers.ModelExecutionChunk, 1)
	chunks <- sdkhandlers.ModelExecutionChunk{Payload: []byte("event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"delta\":{\"type\":\"text_delta\",\"text\":\"Hi\"}}\n\n")}
	close(chunks)
	return sdkhandlers.ModelExecutionStream{StatusCode: http.StatusOK, Chunks: chunks}, nil
}

func TestAccountTestBuildsPinnedCompactExecution(t *testing.T) {
	gin.SetMode(gin.TestMode)
	manager := coreauth.NewManager(nil, nil, nil)
	registered, err := manager.Register(context.Background(), &coreauth.Auth{
		ID: "disabled-auth", Provider: "openai-compatibility", Label: "test-compat", Disabled: true,
		Attributes: map[string]string{"compat_name": "test-compat", "provider_key": "test-compat"},
	})
	if err != nil {
		t.Fatalf("注册测试认证失败：%v", err)
	}
	executor := &accountTestExecutorStub{responseBody: []byte(`{"model":"gpt-5"}`)}
	handler := &Handler{authManager: manager, modelExecutor: executor}

	router := gin.New()
	router.POST("/v0/management/account-test", handler.AccountTest)
	requestBody := `{"auth_index":"` + registered.EnsureIndex() + `","model":"gpt-5","protocol":"responses","mode":"compact"}`
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/v0/management/account-test", strings.NewReader(requestBody))
	request.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("状态码 = %d，响应 = %s", recorder.Code, recorder.Body.String())
	}
	if executor.request.ForcedProvider == "" || executor.request.Model != "gpt-5" || executor.request.EntryProtocol != "openai-response" || executor.request.Alt != "responses/compact" {
		t.Fatalf("执行请求错误：%#v", executor.request)
	}
	if !strings.Contains(recorder.Body.String(), `"success":true`) || !strings.Contains(recorder.Body.String(), `"response_preview":"Compact probe succeeded"`) {
		t.Fatalf("结构化响应错误：%s", recorder.Body.String())
	}
}

func TestBuildAccountTestExecutionUsesHiPrompt(t *testing.T) {
	tests := []struct {
		name     string
		protocol string
	}{
		{name: "Chat Completions", protocol: "chat_completions"},
		{name: "Responses", protocol: "responses"},
		{name: "Messages", protocol: "messages"},
		{name: "Generate Content", protocol: "generate_content"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request, _, err := buildAccountTestExecution(accountTestRequest{
				Model: "test-model", Protocol: test.protocol, Mode: accountTestModeDefault,
			}, "test-provider")
			if err != nil {
				t.Fatalf("构造测试请求失败：%v", err)
			}
			body := string(request.Body)
			if !strings.Contains(body, `"hi"`) || strings.Contains(body, "Reply with OK") {
				t.Fatalf("测试提示词错误：%s", body)
			}
			if request.Stream != (test.protocol == "messages") {
				t.Fatalf("流式执行标记错误：protocol=%s stream=%v", test.protocol, request.Stream)
			}
			if test.protocol == "responses" && strings.Contains(body, "max_output_tokens") {
				t.Fatalf("Responses 测试不应设置过小的输出上限：%s", body)
			}
			if test.protocol == "messages" {
				if gjson.GetBytes(request.Body, "messages.0.content.0.text").String() != "hi" || gjson.GetBytes(request.Body, "max_tokens").Int() != 512 || gjson.GetBytes(request.Body, "tools.#").Int() != 27 || !gjson.GetBytes(request.Body, "stream").Bool() {
					t.Fatalf("Claude Code 测试请求形态错误：%s", body)
				}
			}
		})
	}
}

func TestAccountTestResponsePreviewCollectsClaudeStream(t *testing.T) {
	raw := []byte("event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"model\":\"claude-opus-4-8\"}}\n\nevent: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"delta\":{\"type\":\"text_delta\",\"text\":\"Hi! \"}}\n\nevent: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"delta\":{\"type\":\"text_delta\",\"text\":\"How can I help?\"}}\n\nevent: message_stop\ndata: {\"type\":\"message_stop\"}\n\n")
	if preview := accountTestResponsePreview(raw); preview != "Hi! How can I help?" {
		t.Fatalf("Claude 流式回复提取结果 = %q", preview)
	}
	if model := accountTestUpstreamModel(raw); model != "claude-opus-4-8" {
		t.Fatalf("Claude 流式模型提取结果 = %q", model)
	}
	if !accountTestStreamCompleted(raw) {
		t.Fatal("Claude message_stop 未被识别为流式终止事件")
	}
}

func TestAccountTestResponsePreviewFindsResponsesMessageAfterReasoning(t *testing.T) {
	raw := []byte(`{"output":[{"type":"reasoning","content":[]},{"type":"message","content":[{"type":"output_text","text":"Hi! What can I help you with?"}]}]}`)
	preview := accountTestResponsePreview(raw)
	if preview != "Hi! What can I help you with?" {
		t.Fatalf("Responses 回复提取结果 = %q", preview)
	}
}

func TestAccountTestRejectsCompactForUnsupportedProtocolInput(t *testing.T) {
	request, protocol, err := buildAccountTestExecution(accountTestRequest{
		Model: "gpt-5", Protocol: "chat_completions", Mode: accountTestModeCompact,
	}, "openai-compatibility")
	if err != nil {
		t.Fatalf("Compact 应自动使用 Responses：%v", err)
	}
	if protocol != "responses_compact" || request.Alt != "responses/compact" {
		t.Fatalf("Compact 请求错误：protocol=%q request=%#v", protocol, request)
	}
}

func TestClassifyAccountTestFailureDistinguishesModelAndProtocol(t *testing.T) {
	if code, _ := classifyAccountTestFailure(http.StatusNotFound, `{"error":{"message":"model not found"}}`, "responses"); code != "model_unavailable" {
		t.Fatalf("模型 404 分类 = %q", code)
	}
	if code, _ := classifyAccountTestFailure(http.StatusNotFound, "route not found", "responses"); code != "protocol_not_supported" {
		t.Fatalf("端点 404 分类 = %q", code)
	}
	if code, _ := classifyAccountTestFailure(http.StatusNotFound, "not found", "responses_compact"); code != "protocol_not_supported" {
		t.Fatalf("Compact 404 分类 = %q", code)
	}
}

func TestSafeAccountTestTextRedactsCredential(t *testing.T) {
	redacted := safeAccountTestText("upstream echoed Bearer sk-example-secret-value", 512)
	if strings.Contains(redacted, "sk-example") || !strings.Contains(redacted, "[REDACTED]") {
		t.Fatalf("凭证脱敏失败：%q", redacted)
	}
}
