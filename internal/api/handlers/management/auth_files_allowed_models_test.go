package management

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
	coreauth "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/auth"
)

func TestPatchAuthFileFieldsUpdatesAndEchoesModelPolicy(t *testing.T) {
	authDir := t.TempDir()
	path := filepath.Join(authDir, "test.json")
	if errWrite := os.WriteFile(path, []byte(`{"type":"claude"}`), 0o600); errWrite != nil {
		t.Fatalf("write auth file: %v", errWrite)
	}
	store := &memoryAuthStore{}
	manager := coreauth.NewManager(store, nil, nil)
	record := &coreauth.Auth{
		ID:       "test.json",
		FileName: "test.json",
		Provider: "claude",
		Status:   coreauth.StatusActive,
		Attributes: map[string]string{
			coreauth.AttributePath: path,
		},
		Metadata: map[string]any{"type": "claude"},
	}
	if _, errRegister := manager.Register(context.Background(), record); errRegister != nil {
		t.Fatalf("register auth: %v", errRegister)
	}
	h := NewHandlerWithoutConfigFilePath(&config.Config{AuthDir: authDir}, manager)

	body := `{
		"name":"test.json",
		"allowed_models":["claude-sonnet-*"],
		"model_aliases":[{"name":"claude-sonnet-4","alias":"sonnet"}]
	}`
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	req := httptest.NewRequest(http.MethodPatch, "/v0/management/auth-files/fields", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	ctx.Request = req
	h.PatchAuthFileFields(ctx)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", recorder.Code, recorder.Body.String())
	}

	updated, ok := manager.GetByID("test.json")
	if !ok || updated == nil {
		t.Fatal("updated auth is missing")
	}
	policy := coreauth.AllowedModelPolicyForAuth(updated)
	if !reflect.DeepEqual(policy.Patterns, []string{"claude-sonnet-*"}) {
		t.Fatalf("patterns = %#v", policy.Patterns)
	}
	if !reflect.DeepEqual(policy.Aliases, []string{"sonnet"}) {
		t.Fatalf("aliases = %#v", policy.Aliases)
	}
	coreauth.MarkCredentialDraft(updated)

	entry := h.buildAuthFileEntry(updated)
	if entry == nil {
		t.Fatal("management entry is nil")
	}
	if got, okVersion := entry["model_rule_version"].(string); !okVersion || got == "" {
		t.Fatalf("model_rule_version = %#v", entry["model_rule_version"])
	}
	if got, okEffective := entry["effective_allowed_models"].([]string); !okEffective || !reflect.DeepEqual(got, []string{"claude-sonnet-*", "sonnet"}) {
		t.Fatalf("effective_allowed_models = %#v", entry["effective_allowed_models"])
	}
	if got, okDraft := entry["credential_draft"].(bool); !okDraft || !got {
		t.Fatalf("credential_draft = %#v", entry["credential_draft"])
	}
}
