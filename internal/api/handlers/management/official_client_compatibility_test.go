package management

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/officialclient"
)

func TestPutClaudeKeysAssignsCurrentOfficialClientProfile(t *testing.T) {
	h := &Handler{cfg: &config.Config{}, configFilePath: writeTestConfigFile(t)}
	rec, ctx := managementJSONContext(http.MethodPut, `/v0/management/claude-api-key`, `[
  {
    "api-key": "claude-secret",
    "official-client-compatibility": {"enabled": true}
  }
]`)

	h.PutClaudeKeys(ctx)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	compatibility := h.cfg.ClaudeKey[0].OfficialClientCompatibility
	if compatibility == nil || compatibility.Profile != "claude-desktop-2.1.215-v1" || !compatibility.Enabled {
		t.Fatalf("compatibility = %#v", compatibility)
	}
}

func TestGetClaudeKeysReturnsOfficialClientCompatibility(t *testing.T) {
	h := &Handler{cfg: &config.Config{
		ClaudeKey: []config.ClaudeKey{{
			APIKey: "claude-secret",
			OfficialClientCompatibility: &officialclient.CompatibilityConfig{
				Enabled: true,
				Profile: "claude-desktop-2.1.215-v1",
			},
		}},
	}}
	rec, ctx := managementJSONContext(http.MethodGet, `/v0/management/claude-api-key`, ``)

	h.GetClaudeKeys(ctx)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"official-client-compatibility":{"enabled":true,"profile":"claude-desktop-2.1.215-v1","tls-profile":""}`) {
		t.Fatalf("response is missing compatibility: %s", rec.Body.String())
	}
}

func TestPutCodexKeysRejectsInvalidOfficialClientProfileWithoutMutation(t *testing.T) {
	h := &Handler{
		cfg:            &config.Config{CodexKey: []config.CodexKey{{APIKey: "original", BaseURL: "https://api.openai.com/v1"}}},
		configFilePath: writeTestConfigFile(t),
	}
	rec, ctx := managementJSONContext(http.MethodPut, `/v0/management/codex-api-key`, `[
  {
    "api-key": "replacement-secret",
    "base-url": "https://api.openai.com/v1",
    "official-client-compatibility": {"enabled": true, "profile": "unknown"}
  }
]`)

	h.PutCodexKeys(ctx)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", rec.Code, rec.Body.String())
	}
	if got := h.cfg.CodexKey[0].APIKey; got != "original" {
		t.Fatalf("API key mutated to %q", got)
	}
	if strings.Contains(rec.Body.String(), "replacement-secret") {
		t.Fatalf("response exposes API key: %s", rec.Body.String())
	}
}

func TestPatchCodexKeyAssignsCurrentOfficialClientProfile(t *testing.T) {
	h := &Handler{
		cfg:            &config.Config{CodexKey: []config.CodexKey{{APIKey: "codex-secret", BaseURL: "https://api.openai.com/v1"}}},
		configFilePath: writeTestConfigFile(t),
	}
	rec, ctx := managementJSONContext(http.MethodPatch, `/v0/management/codex-api-key`, `{
  "index": 0,
  "value": {"official-client-compatibility": {"enabled": true}}
}`)

	h.PatchCodexKey(ctx)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	compatibility := h.cfg.CodexKey[0].OfficialClientCompatibility
	if compatibility == nil || compatibility.Profile != "codex-desktop-0.145.0-alpha.18-v1" || !compatibility.Enabled {
		t.Fatalf("compatibility = %#v", compatibility)
	}
}

func TestPatchClaudeKeyRejectsNullOfficialClientCompatibilityWithoutMutation(t *testing.T) {
	want := &officialclient.CompatibilityConfig{Enabled: true, Profile: "claude-desktop-2.1.215-v1"}
	h := &Handler{
		cfg: &config.Config{ClaudeKey: []config.ClaudeKey{{
			APIKey:                      "claude-secret",
			OfficialClientCompatibility: want,
		}}},
		configFilePath: writeTestConfigFile(t),
	}
	rec, ctx := managementJSONContext(http.MethodPatch, `/v0/management/claude-api-key`, `{
  "index": 0,
  "value": {"official-client-compatibility": null}
}`)

	h.PatchClaudeKey(ctx)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", rec.Code, rec.Body.String())
	}
	if h.cfg.ClaudeKey[0].OfficialClientCompatibility != want {
		t.Fatal("compatibility was mutated")
	}
}

func TestXAIManagementRejectsOfficialClientCompatibility(t *testing.T) {
	t.Run("put", func(t *testing.T) {
		h := &Handler{
			cfg:            &config.Config{XAIKey: []config.XAIKey{{APIKey: "original", BaseURL: "https://api.x.ai/v1"}}},
			configFilePath: writeTestConfigFile(t),
		}
		rec, ctx := managementJSONContext(http.MethodPut, `/v0/management/xai-api-key`, `[
  {
    "api-key": "replacement",
    "base-url": "https://api.x.ai/v1",
    "official-client-compatibility": null
  }
]`)
		h.PutXAIKeys(ctx)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400; body=%s", rec.Code, rec.Body.String())
		}
		if got := h.cfg.XAIKey[0].APIKey; got != "original" {
			t.Fatalf("API key mutated to %q", got)
		}
	})

	t.Run("patch", func(t *testing.T) {
		h := &Handler{
			cfg:            &config.Config{XAIKey: []config.XAIKey{{APIKey: "original", BaseURL: "https://api.x.ai/v1"}}},
			configFilePath: writeTestConfigFile(t),
		}
		rec, ctx := managementJSONContext(http.MethodPatch, `/v0/management/xai-api-key`, `{
  "index": 0,
  "value": {"official-client-compatibility": {"enabled": false}}
}`)
		h.PatchXAIKey(ctx)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400; body=%s", rec.Code, rec.Body.String())
		}
	})
}

func managementJSONContext(method, target, body string) (*httptest.ResponseRecorder, *gin.Context) {
	rec := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(rec)
	ctx.Request = httptest.NewRequest(method, target, strings.NewReader(body))
	ctx.Request.Header.Set("Content-Type", "application/json")
	return rec, ctx
}
