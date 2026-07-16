package management

import (
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
)

func TestPatchGeminiKeyUpdatesAllowedModels(t *testing.T) {
	h := &Handler{
		cfg: &config.Config{GeminiKey: []config.GeminiKey{{
			APIKey:  "test-key",
			BaseURL: "https://example.com",
		}}},
		configFilePath: writeTestConfigFile(t),
	}
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(
		http.MethodPatch,
		"/v0/management/gemini-api-key",
		strings.NewReader(`{"index":0,"value":{"allowed-models":["model-a","model-a","family-*"]}}`),
	)
	ctx.Request.Header.Set("Content-Type", "application/json")

	h.PatchGeminiKey(ctx)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", recorder.Code, recorder.Body.String())
	}
	if got := h.cfg.GeminiKey[0].AllowedModels; !reflect.DeepEqual(got, []string{"model-a", "family-*"}) {
		t.Fatalf("allowed models = %#v", got)
	}
}

func TestPutOpenAICompatibilityKeepsPerKeyAllowedModelsIndependent(t *testing.T) {
	h := &Handler{cfg: &config.Config{}, configFilePath: writeTestConfigFile(t)}
	body := `[
		{
			"name":"shared",
			"base-url":"https://example.com/v1",
			"api-key-entries":[
				{"api-key":"key-a","allowed-models":["model-a"]},
				{"api-key":"key-b","allowed-models":["model-b"]}
			]
		}
	]`
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPut, "/v0/management/openai-compatibility", strings.NewReader(body))
	ctx.Request.Header.Set("Content-Type", "application/json")

	h.PutOpenAICompat(ctx)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", recorder.Code, recorder.Body.String())
	}
	entries := h.cfg.OpenAICompatibility[0].APIKeyEntries
	if !reflect.DeepEqual(entries[0].AllowedModels, []string{"model-a"}) || !reflect.DeepEqual(entries[1].AllowedModels, []string{"model-b"}) {
		t.Fatalf("per-key allowed models = %#v", entries)
	}
}

func TestPatchOpenAICompatibilityUpdatesOneKeyAllowlistWithoutKeysInRequest(t *testing.T) {
	h := &Handler{
		cfg: &config.Config{OpenAICompatibility: []config.OpenAICompatibility{{
			Name:    "shared",
			BaseURL: "https://example.com/v1",
			APIKeyEntries: []config.OpenAICompatibilityAPIKey{
				{APIKey: "key-a", AllowedModels: []string{"old-a"}},
				{APIKey: "key-b", AllowedModels: []string{"old-b"}},
			},
		}}},
		configFilePath: writeTestConfigFile(t),
	}
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(
		http.MethodPatch,
		"/v0/management/openai-compatibility",
		strings.NewReader(`{"index":0,"key-index":1,"value":{"allowed-models":["new-b"]}}`),
	)
	ctx.Request.Header.Set("Content-Type", "application/json")

	h.PatchOpenAICompat(ctx)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", recorder.Code, recorder.Body.String())
	}
	entries := h.cfg.OpenAICompatibility[0].APIKeyEntries
	if !reflect.DeepEqual(entries[0].AllowedModels, []string{"old-a"}) || !reflect.DeepEqual(entries[1].AllowedModels, []string{"new-b"}) {
		t.Fatalf("per-key allowed models = %#v", entries)
	}
}
