package synthesizer

import (
	"context"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
	coreauth "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/auth"
	"github.com/router-for-me/CLIProxyAPI/v7/sdk/pluginapi"
)

func TestConfigSynthesizerKeepsOpenAICompatibilityAllowlistPerKey(t *testing.T) {
	ctx := &SynthesisContext{
		Config: &config.Config{OpenAICompatibility: []config.OpenAICompatibility{{
			Name:    "shared",
			BaseURL: "https://example.com/v1",
			Models: []config.OpenAICompatibilityModel{
				{Name: "upstream-a", Alias: "alias-a"},
			},
			APIKeyEntries: []config.OpenAICompatibilityAPIKey{
				{APIKey: "key-a", AllowedModels: []string{"model-a"}},
				{APIKey: "key-b", AllowedModels: []string{"model-b"}},
			},
		}}},
		Now:         time.Now(),
		IDGenerator: NewStableIDGenerator(),
	}

	auths, errSynthesize := NewConfigSynthesizer().Synthesize(ctx)
	if errSynthesize != nil {
		t.Fatalf("Synthesize() error: %v", errSynthesize)
	}
	if len(auths) != 2 {
		t.Fatalf("auth count = %d, want 2", len(auths))
	}

	first := coreauth.AllowedModelPolicyForAuth(auths[0])
	second := coreauth.AllowedModelPolicyForAuth(auths[1])
	if !reflect.DeepEqual(first.Patterns, []string{"model-a"}) {
		t.Fatalf("first patterns = %#v", first.Patterns)
	}
	if !reflect.DeepEqual(second.Patterns, []string{"model-b"}) {
		t.Fatalf("second patterns = %#v", second.Patterns)
	}
	if !reflect.DeepEqual(first.Aliases, []string{"alias-a"}) || !reflect.DeepEqual(second.Aliases, []string{"alias-a"}) {
		t.Fatalf("mapping aliases first/second = %#v/%#v", first.Aliases, second.Aliases)
	}
}

func TestFileSynthesizerAppliesAllowlistToPluginVirtualAuths(t *testing.T) {
	authDir := t.TempDir()
	path := filepath.Join(authDir, "plugin.json")
	data := []byte(`{
		"type":"gemini-cli",
		"allowed_models":["gemini-2.5-pro"],
		"model_aliases":[{"name":"gemini-2.5-pro","alias":"gemini-pro"}]
	}`)
	ctx := &SynthesisContext{
		Config:           &config.Config{},
		AuthDir:          authDir,
		Now:              time.Now(),
		IDGenerator:      NewStableIDGenerator(),
		PluginAuthParser: allowedModelsPluginParser{},
	}

	auths := SynthesizeAuthFile(ctx, path, data)
	if len(auths) != 2 {
		t.Fatalf("auth count = %d, want 2", len(auths))
	}
	for i, auth := range auths {
		policy := coreauth.AllowedModelPolicyForAuth(auth)
		if !reflect.DeepEqual(policy.Patterns, []string{"gemini-2.5-pro"}) {
			t.Fatalf("auth[%d] patterns = %#v", i, policy.Patterns)
		}
		if !reflect.DeepEqual(policy.Aliases, []string{"gemini-pro"}) {
			t.Fatalf("auth[%d] aliases = %#v", i, policy.Aliases)
		}
		if !coreauth.IsPluginVirtualAuth(auth) {
			t.Fatalf("auth[%d] was not marked virtual", i)
		}
	}
}

type allowedModelsPluginParser struct{}

func (allowedModelsPluginParser) ParseAuth(context.Context, pluginapi.AuthParseRequest) (*coreauth.Auth, bool, error) {
	return nil, false, nil
}

func (allowedModelsPluginParser) ParseAuths(context.Context, pluginapi.AuthParseRequest) ([]*coreauth.Auth, bool, error) {
	return []*coreauth.Auth{
		{ID: "plugin-a", Provider: "gemini-cli"},
		{ID: "plugin-b", Provider: "gemini-cli"},
	}, true, nil
}
