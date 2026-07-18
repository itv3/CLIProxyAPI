package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/home"
	"github.com/router-for-me/CLIProxyAPI/v7/sdk/api/handlers/claude"
	"github.com/router-for-me/CLIProxyAPI/v7/sdk/api/handlers/gemini"
	"github.com/router-for-me/CLIProxyAPI/v7/sdk/api/handlers/openai"
)

func TestProtocolModelListEnabledKeepsHomeModelRoutesOnHomeCatalog(t *testing.T) {
	server := newTestServer(t)
	server.cfg.Home.Enabled = true
	server.cfg.ProtocolModelListEnabled = true
	server.handlers.UpdateClients(effectiveSDKConfig(server.cfg))
	home.ClearCurrent()
	t.Cleanup(home.ClearCurrent)

	openAIHandler := openai.NewOpenAIAPIHandler(server.handlers)
	claudeHandler := claude.NewClaudeCodeAPIHandler(server.handlers)
	unifiedHandler := server.unifiedModelsHandler(openAIHandler, claudeHandler)

	requests := []*http.Request{
		httptest.NewRequest(http.MethodGet, "/v1/models", nil),
		httptest.NewRequest(http.MethodGet, "/v1/models?client_version=1", nil),
		httptest.NewRequest(http.MethodGet, "/v1/models", nil),
	}
	requests[2].Header.Set("Anthropic-Version", "2023-06-01")
	for _, request := range requests {
		recorder := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(recorder)
		ctx.Request = request
		unifiedHandler(ctx)
		if recorder.Code != http.StatusServiceUnavailable {
			t.Fatalf("Home 模型目录请求 %q 状态 = %d，期望在缺少 Home 客户端时返回 %d", request.URL.String(), recorder.Code, http.StatusServiceUnavailable)
		}
	}

	geminiRecorder := httptest.NewRecorder()
	geminiContext, _ := gin.CreateTestContext(geminiRecorder)
	geminiContext.Request = httptest.NewRequest(http.MethodGet, "/v1beta/models", nil)
	server.geminiModelsHandler(gemini.NewGeminiAPIHandler(server.handlers))(geminiContext)
	if geminiRecorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("Home Gemini 模型目录状态 = %d，期望 %d", geminiRecorder.Code, http.StatusServiceUnavailable)
	}
}
