package management

import (
	"context"
	"testing"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/watcher/synthesizer"
	coreauth "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/auth"
)

func TestDisabledOpenAICompatibilityReturnsRuntimeIdentity(t *testing.T) {
	cfg := &config.Config{OpenAICompatibility: []config.OpenAICompatibility{
		{
			Name: "draft-provider", BaseURL: "https://draft.example.com/v1", Disabled: true,
			APIKeyEntries: []config.OpenAICompatibilityAPIKey{
				{APIKey: "test-key", AllowedModels: []string{"test-model"}},
			},
		},
	}}
	auths, err := synthesizer.NewConfigSynthesizer().Synthesize(&synthesizer.SynthesisContext{
		Config: cfg, Now: time.Now(), IDGenerator: synthesizer.NewStableIDGenerator(),
	})
	if err != nil || len(auths) != 1 {
		t.Fatalf("合成停用认证：auths=%d err=%v", len(auths), err)
	}
	manager := coreauth.NewManager(nil, nil, nil)
	if _, err = manager.Register(context.Background(), auths[0]); err != nil {
		t.Fatalf("注册停用认证：%v", err)
	}

	result := (&Handler{cfg: cfg, authManager: manager}).openAICompatibilityWithAuthIndex()
	if len(result) != 1 || len(result[0].APIKeyEntries) != 1 {
		t.Fatalf("管理接口账号 = %#v", result)
	}
	entry := result[0].APIKeyEntries[0]
	if entry.AuthIndex == "" || entry.ModelRuleVersion == "" {
		t.Fatalf("停用认证运行时身份不完整：auth-index=%q rule-version=%q", entry.AuthIndex, entry.ModelRuleVersion)
	}
}
