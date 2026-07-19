package cliproxy

import (
	"testing"

	internalregistry "github.com/router-for-me/CLIProxyAPI/v7/internal/registry"
	coreauth "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/auth"
)

func TestRegisterResolvedModelsForAuthFiltersClientVisibleModels(t *testing.T) {
	auth := &coreauth.Auth{ID: "allowed-model-auth", Prefix: "team"}
	coreauth.SetAllowedModelsAttribute(auth, []string{"model-a", "family-*"})
	coreauth.SetAllowedModelAliasesAttribute(auth, []string{"mapped"})

	registry := GlobalModelRegistry()
	registry.UnregisterClient(auth.ID)
	t.Cleanup(func() { registry.UnregisterClient(auth.ID) })

	service := &Service{}
	service.registerResolvedModelsForAuth(auth, "test-provider", []*ModelInfo{
		{ID: "model-a"},
		{ID: "family-one"},
		{ID: "mapped"},
		{ID: "team/mapped"},
		{ID: "blocked"},
	})

	models := internalregistry.GetGlobalRegistry().GetModelsForClient(auth.ID)
	got := make(map[string]bool, len(models))
	for _, model := range models {
		got[model.ID] = true
	}
	for _, modelID := range []string{"model-a", "family-one", "mapped", "team/mapped"} {
		if !got[modelID] {
			t.Fatalf("expected %q in filtered registry: %#v", modelID, got)
		}
	}
	if got["blocked"] {
		t.Fatalf("blocked model remained registered: %#v", got)
	}
	if registry.ClientSupportsModel(auth.ID, "blocked") {
		t.Fatal("blocked model remained eligible for scheduling")
	}
}

func TestRegisterResolvedModelsForAuthEmptyAllowlistKeepsLegacyBehavior(t *testing.T) {
	auth := &coreauth.Auth{ID: "empty-allowed-model-auth"}
	registry := GlobalModelRegistry()
	registry.UnregisterClient(auth.ID)
	t.Cleanup(func() { registry.UnregisterClient(auth.ID) })

	(&Service{}).registerResolvedModelsForAuth(auth, "test-provider", []*ModelInfo{{ID: "model-a"}, {ID: "model-b"}})
	if models := internalregistry.GetGlobalRegistry().GetModelsForClient(auth.ID); len(models) != 2 {
		t.Fatalf("empty allowlist registered %d models, want 2", len(models))
	}
}

func TestRegisterResolvedModelsForAuthMalformedAllowlistFailsClosed(t *testing.T) {
	auth := &coreauth.Auth{ID: "invalid-allowed-model-auth"}
	coreauth.SetAllowedModelsAttribute(auth, []string{"model-*invalid"})
	registry := GlobalModelRegistry()
	registry.UnregisterClient(auth.ID)
	t.Cleanup(func() { registry.UnregisterClient(auth.ID) })

	(&Service{}).registerResolvedModelsForAuth(auth, "test-provider", []*ModelInfo{{ID: "model-a"}})
	if models := internalregistry.GetGlobalRegistry().GetModelsForClient(auth.ID); len(models) != 0 {
		t.Fatalf("malformed allowlist registered models: %#v", models)
	}
}
