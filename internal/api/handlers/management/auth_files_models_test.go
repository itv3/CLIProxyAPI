package management

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/registry"
	coreauth "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/auth"
)

func TestGetAuthFileModelsResolvesRuntimeAuthIndex(t *testing.T) {
	authDir := t.TempDir()
	manager := coreauth.NewManager(&memoryAuthStore{}, nil, nil)
	record := &coreauth.Auth{
		ID:       "registry-auth-id",
		FileName: "account.json",
		Provider: "codex",
		Status:   coreauth.StatusActive,
		Attributes: map[string]string{
			coreauth.AttributePath: filepath.Join(authDir, "account.json"),
		},
	}
	if _, err := manager.Register(context.Background(), record); err != nil {
		t.Fatalf("register auth: %v", err)
	}
	stored, ok := manager.GetByID(record.ID)
	if !ok || stored == nil {
		t.Fatal("registered auth is missing")
	}
	authIndex := stored.EnsureIndex()
	if authIndex == "" || authIndex == record.ID || authIndex == record.FileName {
		t.Fatalf("unexpected auth index %q", authIndex)
	}

	modelRegistry := registry.GetGlobalRegistry()
	modelRegistry.RegisterClient(record.ID, "codex", []*registry.ModelInfo{{ID: "gpt-test"}})
	t.Cleanup(func() { modelRegistry.UnregisterClient(record.ID) })

	handler := NewHandlerWithoutConfigFilePath(&config.Config{AuthDir: authDir}, manager)
	for _, query := range []string{
		"auth_index=" + url.QueryEscape(authIndex),
		"name=" + url.QueryEscape(authIndex),
	} {
		recorder := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(recorder)
		ctx.Request = httptest.NewRequest(http.MethodGet, "/v0/management/auth-files/models?"+query, nil)
		handler.GetAuthFileModels(ctx)
		if recorder.Code != http.StatusOK {
			t.Fatalf("query %q returned status %d: %s", query, recorder.Code, recorder.Body.String())
		}
		var payload struct {
			Models []struct {
				ID string `json:"id"`
			} `json:"models"`
		}
		if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
			t.Fatalf("decode response for %q: %v", query, err)
		}
		if len(payload.Models) != 1 || payload.Models[0].ID != "gpt-test" {
			t.Fatalf("query %q returned models %#v", query, payload.Models)
		}
	}
}
