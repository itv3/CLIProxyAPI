package openai

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/registry"
	"github.com/router-for-me/CLIProxyAPI/v7/sdk/api/handlers"
	sdkconfig "github.com/router-for-me/CLIProxyAPI/v7/sdk/config"
)

func TestOpenAIModelHandlersUseOpenAIProtocolGroup(t *testing.T) {
	registryRef := registry.GetGlobalRegistry()
	clients := map[string]struct {
		group string
		model string
	}{
		"protocol-openai-handler-claude":  {group: registry.ProtocolGroupClaude, model: "protocol-openai-handler-claude-model"},
		"protocol-openai-handler-openai":  {group: registry.ProtocolGroupOpenAI, model: "protocol-openai-handler-openai-model"},
		"protocol-openai-handler-gemini":  {group: registry.ProtocolGroupGemini, model: "protocol-openai-handler-gemini-model"},
		"protocol-openai-handler-unknown": {model: "protocol-openai-handler-unknown-model"},
	}
	for clientID, client := range clients {
		registryRef.RegisterClientWithProtocolGroup(clientID, "test", client.group, []*registry.ModelInfo{{ID: client.model}})
		clientID := clientID
		t.Cleanup(func() { registryRef.UnregisterClient(clientID) })
	}

	base := handlers.NewBaseAPIHandlers(&sdkconfig.SDKConfig{ProtocolModelListEnabled: true}, nil)
	chatHandler := NewOpenAIAPIHandler(base)
	chatModels := chatHandler.Models()
	responsesModels := NewOpenAIResponsesAPIHandler(base).Models()
	for name, models := range map[string][]map[string]any{"chat": chatModels, "responses": responsesModels} {
		assertOpenAIModelPresence(t, name, models, "protocol-openai-handler-openai-model", true)
		assertOpenAIModelPresence(t, name, models, "protocol-openai-handler-unknown-model", true)
		assertOpenAIModelPresence(t, name, models, "protocol-openai-handler-claude-model", false)
		assertOpenAIModelPresence(t, name, models, "protocol-openai-handler-gemini-model", false)
	}

	allModels := NewOpenAIAPIHandler(handlers.NewBaseAPIHandlers(&sdkconfig.SDKConfig{}, nil)).Models()
	assertOpenAIModelPresence(t, "disabled", allModels, "protocol-openai-handler-claude-model", true)
	assertOpenAIModelPresence(t, "disabled", allModels, "protocol-openai-handler-gemini-model", true)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/v1/models?client_version=1", nil)
	chatHandler.OpenAIModels(ctx)
	var codexCatalog struct {
		Models []map[string]any `json:"models"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &codexCatalog); err != nil {
		t.Fatalf("decode client_version response: %v", err)
	}
	assertCodexModelPresence(t, codexCatalog.Models, "protocol-openai-handler-openai-model", true)
	assertCodexModelPresence(t, codexCatalog.Models, "protocol-openai-handler-unknown-model", true)
	assertCodexModelPresence(t, codexCatalog.Models, "protocol-openai-handler-claude-model", false)
}

func assertOpenAIModelPresence(t *testing.T, catalog string, models []map[string]any, modelID string, want bool) {
	t.Helper()
	for _, model := range models {
		if model["id"] == modelID {
			if !want {
				t.Fatalf("%s catalog unexpectedly contains %q", catalog, modelID)
			}
			return
		}
	}
	if want {
		t.Fatalf("%s catalog does not contain %q", catalog, modelID)
	}
}

func assertCodexModelPresence(t *testing.T, models []map[string]any, modelID string, want bool) {
	t.Helper()
	for _, model := range models {
		if model["slug"] == modelID {
			if !want {
				t.Fatalf("client_version catalog unexpectedly contains %q", modelID)
			}
			return
		}
	}
	if want {
		t.Fatalf("client_version catalog does not contain %q", modelID)
	}
}
