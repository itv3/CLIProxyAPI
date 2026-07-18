package claude

import (
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/registry"
	"github.com/router-for-me/CLIProxyAPI/v7/sdk/api/handlers"
	sdkconfig "github.com/router-for-me/CLIProxyAPI/v7/sdk/config"
)

func TestClaudeModelHandlerUsesClaudeProtocolGroup(t *testing.T) {
	registryRef := registry.GetGlobalRegistry()
	clients := map[string]struct {
		group string
		model string
	}{
		"protocol-claude-handler-claude":  {group: registry.ProtocolGroupClaude, model: "protocol-claude-handler-claude-model"},
		"protocol-claude-handler-openai":  {group: registry.ProtocolGroupOpenAI, model: "protocol-claude-handler-openai-model"},
		"protocol-claude-handler-unknown": {model: "protocol-claude-handler-unknown-model"},
	}
	for clientID, client := range clients {
		registryRef.RegisterClientWithProtocolGroup(clientID, "test", client.group, []*registry.ModelInfo{{ID: client.model}})
		clientID := clientID
		t.Cleanup(func() { registryRef.UnregisterClient(clientID) })
	}

	models := NewClaudeCodeAPIHandler(handlers.NewBaseAPIHandlers(&sdkconfig.SDKConfig{ProtocolModelListEnabled: true}, nil)).Models()
	assertClaudeModelPresence(t, models, "protocol-claude-handler-claude-model", true)
	assertClaudeModelPresence(t, models, "protocol-claude-handler-unknown-model", true)
	assertClaudeModelPresence(t, models, "protocol-claude-handler-openai-model", false)
}

func assertClaudeModelPresence(t *testing.T, models []map[string]any, modelID string, want bool) {
	t.Helper()
	for _, model := range models {
		if model["id"] == modelID {
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
