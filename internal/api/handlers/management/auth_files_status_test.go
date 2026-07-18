package management

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
	coreauth "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/auth"
)

func TestPatchAuthFileStatusFinalizesDraftWithoutEnablingAuth(t *testing.T) {
	store := &memoryAuthStore{}
	manager := coreauth.NewManager(store, nil, nil)
	auth := &coreauth.Auth{
		ID:       "codex-draft.json",
		FileName: "codex-draft.json",
		Provider: "codex",
		Metadata: map[string]any{
			"type":          "codex",
			"access_token":  "access-secret",
			"refresh_token": "refresh-secret",
			"draft":         true,
		},
	}
	coreauth.MarkCredentialDraft(auth)
	if _, errRegister := manager.Register(context.Background(), auth); errRegister != nil {
		t.Fatalf("register draft auth: %v", errRegister)
	}

	h := NewHandlerWithoutConfigFilePath(&config.Config{AuthDir: t.TempDir()}, manager)
	rec := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(rec)
	req := httptest.NewRequest(http.MethodPatch, "/v0/management/auth-files/status", strings.NewReader(
		`{"name":"codex-draft.json","disabled":true,"finalize_draft":true}`,
	))
	req.Header.Set("Content-Type", "application/json")
	ctx.Request = req

	h.PatchAuthFileStatus(ctx)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	var response map[string]any
	if errDecode := json.Unmarshal(rec.Body.Bytes(), &response); errDecode != nil {
		t.Fatalf("decode response: %v", errDecode)
	}
	if response["disabled"] != true || response["draft_finalized"] != true {
		t.Fatalf("unexpected response: %#v", response)
	}

	updated, ok := manager.GetByID(auth.ID)
	if !ok || updated == nil {
		t.Fatal("finalized auth is missing")
	}
	if !updated.Disabled || updated.Status != coreauth.StatusDisabled {
		t.Fatalf("disabled/status = %t/%q, want disabled", updated.Disabled, updated.Status)
	}
	if coreauth.IsCredentialDraft(updated) {
		t.Fatalf("draft markers were not cleared: %#v", updated.Metadata)
	}
	if disabled, _ := updated.Metadata["disabled"].(bool); !disabled {
		t.Fatalf("metadata.disabled = %#v, want true", updated.Metadata["disabled"])
	}
}

func TestPatchAuthFileStatusKeepsDraftMarkersByDefault(t *testing.T) {
	manager := coreauth.NewManager(&memoryAuthStore{}, nil, nil)
	auth := &coreauth.Auth{
		ID: "codex-draft.json", FileName: "codex-draft.json", Provider: "codex",
		Metadata: map[string]any{"type": "codex"},
	}
	coreauth.MarkCredentialDraft(auth)
	if _, errRegister := manager.Register(context.Background(), auth); errRegister != nil {
		t.Fatalf("register draft auth: %v", errRegister)
	}

	h := NewHandlerWithoutConfigFilePath(&config.Config{AuthDir: t.TempDir()}, manager)
	rec := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(rec)
	req := httptest.NewRequest(http.MethodPatch, "/v0/management/auth-files/status", strings.NewReader(
		`{"name":"codex-draft.json","disabled":true}`,
	))
	req.Header.Set("Content-Type", "application/json")
	ctx.Request = req
	h.PatchAuthFileStatus(ctx)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	updated, _ := manager.GetByID(auth.ID)
	if !coreauth.IsCredentialDraft(updated) {
		t.Fatalf("draft markers were cleared without finalize_draft: %#v", updated)
	}
}
