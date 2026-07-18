package cliproxy

import (
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/registry"
	coreauth "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/auth"
)

func TestProtocolGroupForAuth(t *testing.T) {
	pluginAuth := &coreauth.Auth{ID: "plugin", Provider: "claude"}
	coreauth.MarkPluginVirtualAuth(pluginAuth, "plugin.yaml", 0)

	tests := []struct {
		name     string
		auth     *coreauth.Auth
		expected string
	}{
		{name: "nil", auth: nil, expected: ""},
		{name: "Claude", auth: &coreauth.Auth{Provider: "claude"}, expected: registry.ProtocolGroupClaude},
		{name: "OpenAI", auth: &coreauth.Auth{Provider: "openai"}, expected: registry.ProtocolGroupOpenAI},
		{name: "Codex", auth: &coreauth.Auth{Provider: "codex"}, expected: registry.ProtocolGroupOpenAI},
		{name: "Kimi", auth: &coreauth.Auth{Provider: "kimi"}, expected: registry.ProtocolGroupOpenAI},
		{name: "XAI", auth: &coreauth.Auth{Provider: "xai"}, expected: registry.ProtocolGroupOpenAI},
		{name: "Gemini", auth: &coreauth.Auth{Provider: "gemini"}, expected: registry.ProtocolGroupGemini},
		{name: "GeminiInteractions", auth: &coreauth.Auth{Provider: "gemini-interactions"}, expected: registry.ProtocolGroupGemini},
		{name: "Vertex", auth: &coreauth.Auth{Provider: "vertex"}, expected: registry.ProtocolGroupGemini},
		{name: "AIStudio", auth: &coreauth.Auth{Provider: "aistudio"}, expected: registry.ProtocolGroupGemini},
		{name: "Antigravity", auth: &coreauth.Auth{Provider: "antigravity"}, expected: registry.ProtocolGroupGemini},
		{
			name: "OpenAICompatibility",
			auth: &coreauth.Auth{
				Provider:   "custom-provider",
				Attributes: map[string]string{"compat_name": "Custom Provider", "provider_key": "custom-provider"},
			},
			expected: registry.ProtocolGroupOpenAI,
		},
		{name: "Unknown", auth: &coreauth.Auth{Provider: "future-provider"}, expected: ""},
		{name: "PluginVirtual", auth: pluginAuth, expected: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := protocolGroupForAuth(tt.auth); got != tt.expected {
				t.Fatalf("protocolGroupForAuth() = %q，期望 %q", got, tt.expected)
			}
		})
	}
}

func TestRegisterResolvedModelsForAuthAppliesAllowedModelsBeforeProtocolView(t *testing.T) {
	const (
		clientID     = "protocol-view-allowed-models-client"
		allowedModel = "protocol-view-allowed-model"
		deniedModel  = "protocol-view-denied-model"
	)
	modelRegistry := registry.GetGlobalRegistry()
	modelRegistry.UnregisterClient(clientID)
	t.Cleanup(func() {
		modelRegistry.UnregisterClient(clientID)
	})

	auth := &coreauth.Auth{ID: clientID, Provider: "claude"}
	coreauth.SetAllowedModelsAttribute(auth, []string{allowedModel})
	service := &Service{}
	service.registerResolvedModelsForAuth(auth, "claude", []*ModelInfo{
		{ID: allowedModel},
		{ID: deniedModel},
	})

	if !modelRegistry.ClientSupportsModel(clientID, allowedModel) {
		t.Fatalf("允许模型 %q 未注册", allowedModel)
	}
	if modelRegistry.ClientSupportsModel(clientID, deniedModel) {
		t.Fatalf("未允许模型 %q 不应注册", deniedModel)
	}
	claudeModels := modelRegistry.GetAvailableModelsForProtocol("openai", registry.ProtocolGroupClaude)
	if !serviceProtocolModelsContains(claudeModels, allowedModel) {
		t.Fatalf("Claude 协议组中缺少允许模型 %q", allowedModel)
	}
	openAIModels := modelRegistry.GetAvailableModelsForProtocol("openai", registry.ProtocolGroupOpenAI)
	if serviceProtocolModelsContains(openAIModels, allowedModel) {
		t.Fatalf("Claude 账号模型 %q 不应出现在 OpenAI 协议组", allowedModel)
	}
}

func TestRegisterResolvedModelsForAuthLeavesPluginAndUnknownUnscoped(t *testing.T) {
	const (
		pluginClientID = "protocol-view-plugin-client"
		pluginModel    = "protocol-view-plugin-model"
		unknownClient  = "protocol-view-unknown-client"
		unknownModel   = "protocol-view-unknown-model"
	)
	modelRegistry := registry.GetGlobalRegistry()
	for _, clientID := range []string{pluginClientID, unknownClient} {
		modelRegistry.UnregisterClient(clientID)
	}
	t.Cleanup(func() {
		for _, clientID := range []string{pluginClientID, unknownClient} {
			modelRegistry.UnregisterClient(clientID)
		}
	})

	pluginAuth := &coreauth.Auth{ID: pluginClientID, Provider: "claude"}
	coreauth.MarkPluginVirtualAuth(pluginAuth, "plugin.yaml", 0)
	service := &Service{}
	service.registerResolvedModelsForAuth(pluginAuth, "claude", []*ModelInfo{{ID: pluginModel}})
	service.registerResolvedModelsForAuth(
		&coreauth.Auth{ID: unknownClient, Provider: "future-provider"},
		"future-provider",
		[]*ModelInfo{{ID: unknownModel}},
	)

	for _, group := range []string{registry.ProtocolGroupClaude, registry.ProtocolGroupOpenAI, registry.ProtocolGroupGemini} {
		models := modelRegistry.GetAvailableModelsForProtocol("openai", group)
		if !serviceProtocolModelsContains(models, pluginModel) {
			t.Fatalf("协议组 %q 缺少插件模型 %q", group, pluginModel)
		}
		if !serviceProtocolModelsContains(models, unknownModel) {
			t.Fatalf("协议组 %q 缺少未知来源模型 %q", group, unknownModel)
		}
	}
}

func TestAppendedPluginModelsRemainUnscopedAfterPrefixAndAllowedModels(t *testing.T) {
	const (
		clientID           = "protocol-view-appended-plugin-client"
		nativeModel        = "team/native-model"
		allowedPluginModel = "team/plugin-model"
		deniedPluginModel  = "team/denied-plugin-model"
	)
	modelRegistry := registry.GetGlobalRegistry()
	modelRegistry.UnregisterClient(clientID)
	t.Cleanup(func() {
		modelRegistry.UnregisterClient(clientID)
	})

	models, unscopedModelIDs := mergePluginModels(
		[]*ModelInfo{{ID: "native-model"}},
		[]*ModelInfo{{ID: "plugin-model"}, {ID: "denied-plugin-model"}},
	)
	models, unscopedModelIDs = applyModelPrefixesWithUnscopedModels(models, unscopedModelIDs, "team", true)
	auth := &coreauth.Auth{ID: clientID, Provider: "claude"}
	coreauth.SetAllowedModelsAttribute(auth, []string{nativeModel, allowedPluginModel})
	service := &Service{}
	service.registerResolvedModelsForAuthWithProtocolVisibility(
		auth,
		"claude",
		registry.ProtocolGroupClaude,
		unscopedModelIDs,
		models,
	)

	claudeModels := modelRegistry.GetAvailableModelsForProtocol("openai", registry.ProtocolGroupClaude)
	if !serviceProtocolModelsContains(claudeModels, nativeModel) || !serviceProtocolModelsContains(claudeModels, allowedPluginModel) {
		t.Fatalf("Claude 协议组缺少前缀处理后的模型：%v", claudeModels)
	}
	for _, group := range []string{registry.ProtocolGroupOpenAI, registry.ProtocolGroupGemini} {
		visibleModels := modelRegistry.GetAvailableModelsForProtocol("openai", group)
		if serviceProtocolModelsContains(visibleModels, nativeModel) {
			t.Fatalf("原生模型 %q 泄漏到协议组 %q", nativeModel, group)
		}
		if !serviceProtocolModelsContains(visibleModels, allowedPluginModel) {
			t.Fatalf("协议组 %q 缺少插件模型 %q", group, allowedPluginModel)
		}
		if serviceProtocolModelsContains(visibleModels, deniedPluginModel) {
			t.Fatalf("协议组 %q 显示了被 allowed_models 排除的插件模型", group)
		}
	}
	if modelRegistry.ClientSupportsModel(clientID, deniedPluginModel) {
		t.Fatalf("被 allowed_models 排除的插件模型 %q 不应注册", deniedPluginModel)
	}

	modelRegistry.SuspendClientModel(clientID, allowedPluginModel, "manual")
	if models := modelRegistry.GetAvailableModelsForProtocol("openai", registry.ProtocolGroupOpenAI); serviceProtocolModelsContains(models, allowedPluginModel) {
		t.Fatalf("真实客户端挂起后插件模型仍可见：%v", models)
	}
	modelRegistry.ResumeClientModel(clientID, allowedPluginModel)
	modelRegistry.SetModelQuotaExceeded(clientID, allowedPluginModel)
	if models := modelRegistry.GetAvailableModelsForProtocol("openai", registry.ProtocolGroupOpenAI); !serviceProtocolModelsContains(models, allowedPluginModel) {
		t.Fatalf("插件模型未沿用配额冷却可见语义：%v", models)
	}
}

func TestAppendedPluginModelCollisionDoesNotUnscopeNativeModel(t *testing.T) {
	const (
		clientID       = "protocol-view-plugin-collision-client"
		collisionModel = "team/plugin-model"
	)
	modelRegistry := registry.GetGlobalRegistry()
	modelRegistry.UnregisterClient(clientID)
	t.Cleanup(func() {
		modelRegistry.UnregisterClient(clientID)
	})

	models, unscopedModelIDs := mergePluginModels(
		[]*ModelInfo{{ID: collisionModel}},
		[]*ModelInfo{{ID: "plugin-model"}, {ID: collisionModel}},
	)
	models, unscopedModelIDs = applyModelPrefixesWithUnscopedModels(models, unscopedModelIDs, "team", false)
	if _, unscoped := unscopedModelIDs[collisionModel]; unscoped {
		t.Fatalf("前缀碰撞后的原生模型 %q 不应标记为插件模型", collisionModel)
	}

	auth := &coreauth.Auth{ID: clientID, Provider: "claude"}
	coreauth.SetAllowedModelsAttribute(auth, []string{collisionModel})
	service := &Service{}
	service.registerResolvedModelsForAuthWithProtocolVisibility(
		auth,
		"claude",
		registry.ProtocolGroupClaude,
		unscopedModelIDs,
		models,
	)
	if models := modelRegistry.GetAvailableModelsForProtocol("openai", registry.ProtocolGroupClaude); !serviceProtocolModelsContains(models, collisionModel) {
		t.Fatalf("Claude 协议组缺少原生碰撞模型：%v", models)
	}
	for _, group := range []string{registry.ProtocolGroupOpenAI, registry.ProtocolGroupGemini} {
		if models := modelRegistry.GetAvailableModelsForProtocol("openai", group); serviceProtocolModelsContains(models, collisionModel) {
			t.Fatalf("原生碰撞模型泄漏到协议组 %q：%v", group, models)
		}
	}
}

func serviceProtocolModelsContains(models []map[string]any, expected string) bool {
	for _, model := range models {
		if id, _ := model["id"].(string); id == expected {
			return true
		}
	}
	return false
}
