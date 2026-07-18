package gemini

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

func TestGeminiModelHandlerUsesGeminiProtocolGroup(t *testing.T) {
	registryRef := registry.GetGlobalRegistry()
	clients := map[string]struct {
		group string
		model string
	}{
		"protocol-gemini-handler-gemini":  {group: registry.ProtocolGroupGemini, model: "protocol-gemini-handler-gemini-model"},
		"protocol-gemini-handler-openai":  {group: registry.ProtocolGroupOpenAI, model: "protocol-gemini-handler-openai-model"},
		"protocol-gemini-handler-unknown": {model: "protocol-gemini-handler-unknown-model"},
	}
	for clientID, client := range clients {
		registryRef.RegisterClientWithProtocolGroup(clientID, "test", client.group, []*registry.ModelInfo{{ID: client.model, Name: client.model}})
		clientID := clientID
		t.Cleanup(func() { registryRef.UnregisterClient(clientID) })
	}

	handler := NewGeminiAPIHandler(handlers.NewBaseAPIHandlers(&sdkconfig.SDKConfig{ProtocolModelListEnabled: true}, nil))
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	handler.GeminiModels(ctx)
	var response struct {
		Models []map[string]any `json:"models"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode list response: %v", err)
	}
	models := response.Models
	assertGeminiModelPresence(t, models, "protocol-gemini-handler-gemini-model", true)
	assertGeminiModelPresence(t, models, "protocol-gemini-handler-unknown-model", true)
	assertGeminiModelPresence(t, models, "protocol-gemini-handler-openai-model", false)

	detailRecorder := httptest.NewRecorder()
	detailContext, _ := gin.CreateTestContext(detailRecorder)
	detailContext.Params = gin.Params{{Key: "action", Value: "protocol-gemini-handler-openai-model"}}
	handler.GeminiGetHandler(detailContext)
	if detailRecorder.Code != http.StatusOK {
		t.Fatalf("cross-protocol model detail status = %d, want %d", detailRecorder.Code, http.StatusOK)
	}
}

func assertGeminiModelPresence(t *testing.T, models []map[string]any, modelID string, want bool) {
	t.Helper()
	for _, model := range models {
		if model["name"] == modelID || model["name"] == "models/"+modelID {
			if !want {
				t.Fatalf("catalog unexpectedly contains %q", modelID)
			}
			return
		}
	}
	if want {
		t.Fatalf("catalog does not contain %q", modelID)
	}
}
