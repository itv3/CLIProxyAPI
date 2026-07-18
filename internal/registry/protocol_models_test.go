package registry

import (
	"reflect"
	"sort"
	"testing"
	"time"
)

func TestGetAvailableModelsForProtocolFiltersByClientGroup(t *testing.T) {
	r := newTestModelRegistry()
	r.RegisterClientWithProtocolGroup("claude-client", "claude", ProtocolGroupClaude, []*ModelInfo{
		{ID: "claude-only"},
		{ID: "shared"},
	})
	r.RegisterClientWithProtocolGroup("openai-client", "codex", ProtocolGroupOpenAI, []*ModelInfo{
		{ID: "openai-only"},
		{ID: "shared"},
	})
	r.RegisterClientWithProtocolGroup("gemini-client", "gemini", ProtocolGroupGemini, []*ModelInfo{{ID: "gemini-only"}})
	r.RegisterClient("plugin-client", "plugin-provider", []*ModelInfo{{ID: "plugin-model"}})
	r.RegisterClientWithProtocolGroup("unknown-client", "future-provider", "future-protocol", []*ModelInfo{{ID: "unknown-model"}})

	tests := []struct {
		name     string
		group    string
		expected []string
	}{
		{
			name:     "Claude",
			group:    " CLAUDE ",
			expected: []string{"claude-only", "plugin-model", "shared", "unknown-model"},
		},
		{
			name:     "OpenAI",
			group:    ProtocolGroupOpenAI,
			expected: []string{"openai-only", "plugin-model", "shared", "unknown-model"},
		},
		{
			name:     "Gemini",
			group:    ProtocolGroupGemini,
			expected: []string{"gemini-only", "plugin-model", "unknown-model"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			models := r.GetAvailableModelsForProtocol("openai", tt.group)
			if got := protocolTestModelIDs(models); !reflect.DeepEqual(got, tt.expected) {
				t.Fatalf("协议组 %q 的模型 = %v，期望 %v", tt.group, got, tt.expected)
			}
		})
	}

	first := r.GetAvailableModelsForProtocol("openai", ProtocolGroupClaude)
	first[0]["id"] = "mutated"
	second := r.GetAvailableModelsForProtocol("openai", ProtocolGroupClaude)
	if got := protocolTestModelIDs(second); !reflect.DeepEqual(got, tests[0].expected) {
		t.Fatalf("修改返回快照后缓存内容 = %v，期望 %v", got, tests[0].expected)
	}
}

func TestGetAvailableModelsForProtocolInvalidatesCacheWhenGroupChanges(t *testing.T) {
	r := newTestModelRegistry()
	models := []*ModelInfo{{ID: "movable-model"}}
	r.RegisterClientWithProtocolGroup("client", "shared-provider", ProtocolGroupClaude, models)

	if got := protocolTestModelIDs(r.GetAvailableModelsForProtocol("openai", ProtocolGroupClaude)); !reflect.DeepEqual(got, []string{"movable-model"}) {
		t.Fatalf("变更前 Claude 模型 = %v", got)
	}
	if got := protocolTestModelIDs(r.GetAvailableModelsForProtocol("openai", ProtocolGroupOpenAI)); len(got) != 0 {
		t.Fatalf("变更前 OpenAI 模型 = %v，期望为空", got)
	}

	r.RegisterClientWithProtocolGroup("client", "shared-provider", ProtocolGroupOpenAI, models)
	if got := protocolTestModelIDs(r.GetAvailableModelsForProtocol("openai", ProtocolGroupClaude)); len(got) != 0 {
		t.Fatalf("变更后 Claude 模型 = %v，期望为空", got)
	}
	if got := protocolTestModelIDs(r.GetAvailableModelsForProtocol("openai", ProtocolGroupOpenAI)); !reflect.DeepEqual(got, []string{"movable-model"}) {
		t.Fatalf("变更后 OpenAI 模型 = %v", got)
	}

	r.UnregisterClient("client")
	if got := protocolTestModelIDs(r.GetAvailableModelsForProtocol("openai", ProtocolGroupOpenAI)); len(got) != 0 {
		t.Fatalf("注销后 OpenAI 模型 = %v，期望为空", got)
	}
}

func TestGetAvailableModelsForProtocolSupportsModelLevelUnscopedVisibility(t *testing.T) {
	r := newTestModelRegistry()
	models := []*ModelInfo{{ID: "native-model"}, {ID: "plugin-model"}}
	r.RegisterClientWithProtocolGroupAndUnscopedModels(
		"real-client",
		"claude",
		ProtocolGroupClaude,
		[]string{"plugin-model"},
		models,
	)

	claudeModels := protocolTestModelIDs(r.GetAvailableModelsForProtocol("openai", ProtocolGroupClaude))
	if !reflect.DeepEqual(claudeModels, []string{"native-model", "plugin-model"}) {
		t.Fatalf("Claude 协议组模型 = %v", claudeModels)
	}
	for _, group := range []string{ProtocolGroupOpenAI, ProtocolGroupGemini} {
		got := protocolTestModelIDs(r.GetAvailableModelsForProtocol("openai", group))
		if !reflect.DeepEqual(got, []string{"plugin-model"}) {
			t.Fatalf("协议组 %q 的模型 = %v，期望仅插件模型", group, got)
		}
	}

	r.SuspendClientModel("real-client", "plugin-model", "manual")
	for _, group := range []string{ProtocolGroupClaude, ProtocolGroupOpenAI, ProtocolGroupGemini} {
		got := protocolTestModelIDs(r.GetAvailableModelsForProtocol("openai", group))
		if containsProtocolTestModel(got, "plugin-model") {
			t.Fatalf("真实客户端挂起后协议组 %q 仍显示插件模型：%v", group, got)
		}
	}
	if got := protocolTestModelIDs(r.GetAvailableModelsForProtocol("openai", ProtocolGroupClaude)); !containsProtocolTestModel(got, "native-model") {
		t.Fatalf("插件模型挂起不应隐藏同客户端的原生模型：%v", got)
	}

	r.ResumeClientModel("real-client", "plugin-model")
	r.SetModelQuotaExceeded("real-client", "plugin-model")
	for _, group := range []string{ProtocolGroupClaude, ProtocolGroupOpenAI, ProtocolGroupGemini} {
		got := protocolTestModelIDs(r.GetAvailableModelsForProtocol("openai", group))
		if !containsProtocolTestModel(got, "plugin-model") {
			t.Fatalf("协议组 %q 未沿用配额冷却可见语义：%v", group, got)
		}
	}

	r.RegisterClientWithProtocolGroup("real-client", "claude", ProtocolGroupClaude, models)
	for _, group := range []string{ProtocolGroupOpenAI, ProtocolGroupGemini} {
		got := protocolTestModelIDs(r.GetAvailableModelsForProtocol("openai", group))
		if containsProtocolTestModel(got, "plugin-model") {
			t.Fatalf("清除模型级覆盖后协议组 %q 仍显示插件模型：%v", group, got)
		}
	}
}

func TestGetAvailableModelsForProtocolKeepsAvailabilitySemanticsPerGroup(t *testing.T) {
	r := newTestModelRegistry()
	r.RegisterClientWithProtocolGroup("claude-a", "claude", ProtocolGroupClaude, []*ModelInfo{{ID: "shared-model"}})
	r.RegisterClientWithProtocolGroup("claude-b", "claude", ProtocolGroupClaude, []*ModelInfo{{ID: "shared-model"}})
	r.RegisterClientWithProtocolGroup("openai-a", "codex", ProtocolGroupOpenAI, []*ModelInfo{{ID: "shared-model"}})
	r.RegisterClientWithProtocolGroup("quota-client", "claude", ProtocolGroupClaude, []*ModelInfo{{ID: "quota-model"}})
	r.RegisterClientWithProtocolGroup("cooldown-client", "claude", ProtocolGroupClaude, []*ModelInfo{{ID: "cooldown-model"}})

	r.SuspendClientModel("claude-a", "shared-model", "manual")
	if got := protocolTestModelIDs(r.GetAvailableModelsForProtocol("openai", ProtocolGroupClaude)); !containsProtocolTestModel(got, "shared-model") {
		t.Fatalf("仍有一个可用 Claude 客户端时 shared-model 被隐藏：%v", got)
	}
	r.SuspendClientModel("claude-b", "shared-model", "manual")
	if got := protocolTestModelIDs(r.GetAvailableModelsForProtocol("openai", ProtocolGroupClaude)); containsProtocolTestModel(got, "shared-model") {
		t.Fatalf("所有 Claude 客户端手工挂起后 shared-model 仍可见：%v", got)
	}
	if got := protocolTestModelIDs(r.GetAvailableModelsForProtocol("openai", ProtocolGroupOpenAI)); !containsProtocolTestModel(got, "shared-model") {
		t.Fatalf("Claude 挂起不应影响 OpenAI 组：%v", got)
	}

	r.SetModelQuotaExceeded("quota-client", "quota-model")
	r.SuspendClientModel("cooldown-client", "cooldown-model", "quota")
	got := protocolTestModelIDs(r.GetAvailableModelsForProtocol("openai", ProtocolGroupClaude))
	if !containsProtocolTestModel(got, "quota-model") || !containsProtocolTestModel(got, "cooldown-model") {
		t.Fatalf("配额冷却模型应沿用现有列表可见语义：%v", got)
	}
}

func TestGetAvailableModelsForProtocolUsesMetadataFromSelectedGroup(t *testing.T) {
	r := newTestModelRegistry()
	r.RegisterClientWithProtocolGroup("claude-client", "claude", ProtocolGroupClaude, []*ModelInfo{{
		ID:                  "shared-model",
		DisplayName:         "Claude Metadata",
		ContextLength:       111,
		MaxCompletionTokens: 11,
	}})
	r.RegisterClientWithProtocolGroup("openai-client", "codex", ProtocolGroupOpenAI, []*ModelInfo{{
		ID:                  "shared-model",
		DisplayName:         "OpenAI Metadata",
		ContextLength:       222,
		MaxCompletionTokens: 22,
	}})

	claudeModels := r.GetAvailableModelsForProtocol("claude", ProtocolGroupClaude)
	if len(claudeModels) != 1 {
		t.Fatalf("Claude 协议组模型数量 = %d，期望 1", len(claudeModels))
	}
	if got := claudeModels[0]["display_name"]; got != "Claude Metadata" {
		t.Fatalf("Claude 协议组 display_name = %v", got)
	}
	if got := claudeModels[0]["max_input_tokens"]; got != 111 {
		t.Fatalf("Claude 协议组 max_input_tokens = %v", got)
	}

	openAIModels := r.GetAvailableModelsForProtocol("claude", ProtocolGroupOpenAI)
	if len(openAIModels) != 1 {
		t.Fatalf("OpenAI 协议组模型数量 = %d，期望 1", len(openAIModels))
	}
	if got := openAIModels[0]["display_name"]; got != "OpenAI Metadata" {
		t.Fatalf("OpenAI 协议组 display_name = %v", got)
	}
	if got := openAIModels[0]["max_input_tokens"]; got != 222 {
		t.Fatalf("OpenAI 协议组 max_input_tokens = %v", got)
	}

	r.UnregisterClient("openai-client")
	claudeModels = r.GetAvailableModelsForProtocol("claude", ProtocolGroupClaude)
	if len(claudeModels) != 1 || claudeModels[0]["display_name"] != "Claude Metadata" {
		t.Fatalf("其他组注销后的 Claude 元数据 = %v", claudeModels)
	}
}

func TestGetAvailableModelsForProtocolRebuildsAfterQuotaRecovery(t *testing.T) {
	r := newTestModelRegistry()
	r.RegisterClientWithProtocolGroup("quota-client", "claude", ProtocolGroupClaude, []*ModelInfo{{ID: "quota-model"}})
	r.SetModelQuotaExceeded("quota-client", "quota-model")

	recoveryDelay := 100 * time.Millisecond
	quotaTime := time.Now().Add(-modelQuotaExceededWindow).Add(recoveryDelay)
	r.mutex.Lock()
	r.models["quota-model"].QuotaExceededClients["quota-client"] = &quotaTime
	r.invalidateAvailableModelsCacheLocked()
	r.mutex.Unlock()

	r.GetAvailableModelsForProtocol("openai", ProtocolGroupClaude)
	cacheKey := protocolModelsCacheKey("openai", ProtocolGroupClaude)
	r.mutex.RLock()
	beforeRecovery := r.availableModelsCache[cacheKey]
	r.mutex.RUnlock()
	if beforeRecovery.expiresAt.IsZero() {
		t.Fatal("配额恢复前的协议模型缓存缺少过期时间")
	}

	time.Sleep(2 * recoveryDelay)
	r.GetAvailableModelsForProtocol("openai", ProtocolGroupClaude)
	r.mutex.RLock()
	afterRecovery := r.availableModelsCache[cacheKey]
	r.mutex.RUnlock()
	if !afterRecovery.expiresAt.IsZero() {
		t.Fatalf("配额恢复后的协议模型缓存过期时间 = %v，期望清空", afterRecovery.expiresAt)
	}
}

func protocolTestModelIDs(models []map[string]any) []string {
	ids := make([]string, 0, len(models))
	for _, model := range models {
		if id, ok := model["id"].(string); ok {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	return ids
}

func containsProtocolTestModel(models []string, expected string) bool {
	for _, model := range models {
		if model == expected {
			return true
		}
	}
	return false
}
