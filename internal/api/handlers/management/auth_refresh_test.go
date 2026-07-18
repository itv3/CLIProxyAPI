package management

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
	coreauth "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/auth"
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/executor"
)

type authRefreshHandlerStore struct{}

func (authRefreshHandlerStore) List(context.Context) ([]*coreauth.Auth, error) { return nil, nil }
func (authRefreshHandlerStore) Save(context.Context, *coreauth.Auth) (string, error) {
	return "/tmp/codex.json", nil
}
func (authRefreshHandlerStore) Delete(context.Context, string) error { return nil }

type authRefreshHandlerExecutor struct {
	refreshErr error
}

func (authRefreshHandlerExecutor) Identifier() string { return "codex" }
func (authRefreshHandlerExecutor) Execute(context.Context, *coreauth.Auth, cliproxyexecutor.Request, cliproxyexecutor.Options) (cliproxyexecutor.Response, error) {
	return cliproxyexecutor.Response{}, nil
}
func (authRefreshHandlerExecutor) ExecuteStream(context.Context, *coreauth.Auth, cliproxyexecutor.Request, cliproxyexecutor.Options) (*cliproxyexecutor.StreamResult, error) {
	return nil, nil
}
func (e authRefreshHandlerExecutor) Refresh(_ context.Context, auth *coreauth.Auth) (*coreauth.Auth, error) {
	if e.refreshErr != nil {
		return nil, e.refreshErr
	}
	auth.Metadata["access_token"] = "new-secret-token"
	return auth, nil
}

func TestRefreshAuthFileCredentialMapsInvalidGrantToSafeReauthorizationCode(t *testing.T) {
	gin.SetMode(gin.TestMode)
	manager := coreauth.NewManager(authRefreshHandlerStore{}, nil, nil)
	manager.RegisterExecutor(authRefreshHandlerExecutor{refreshErr: errors.New(`status 400: {"error":"invalid_grant"}`)})
	registered, err := manager.Register(coreauth.WithSkipPersist(context.Background()), &coreauth.Auth{
		ID: "codex-account", Provider: "codex", FileName: "codex.json", Status: coreauth.StatusActive,
		Metadata: map[string]any{"access_token": "old-secret-token", "refresh_token": "refresh-secret-token"},
	})
	if err != nil {
		t.Fatalf("register auth: %v", err)
	}
	handler := NewHandlerWithoutConfigFilePath(&config.Config{}, manager)
	body := `{"auth_index":"` + registered.EnsureIndex() + `"}`
	request := httptest.NewRequest(http.MethodPost, "/v0/management/auth-files/refresh", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = request

	handler.RefreshAuthFileCredential(ctx)

	if recorder.Code != http.StatusUnprocessableEntity || !strings.Contains(recorder.Body.String(), `"code":"reauthorization_required"`) {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if strings.Contains(recorder.Body.String(), "invalid_grant") || strings.Contains(recorder.Body.String(), "secret-token") {
		t.Fatalf("response exposed upstream or credential details: %s", recorder.Body.String())
	}
}
func (authRefreshHandlerExecutor) CountTokens(context.Context, *coreauth.Auth, cliproxyexecutor.Request, cliproxyexecutor.Options) (cliproxyexecutor.Response, error) {
	return cliproxyexecutor.Response{}, nil
}
func (authRefreshHandlerExecutor) HttpRequest(context.Context, *coreauth.Auth, *http.Request) (*http.Response, error) {
	return nil, nil
}

func TestRefreshAuthFileCredentialReturnsOnlySafeMetadata(t *testing.T) {
	gin.SetMode(gin.TestMode)
	manager := coreauth.NewManager(authRefreshHandlerStore{}, nil, nil)
	manager.RegisterExecutor(authRefreshHandlerExecutor{})
	registered, err := manager.Register(coreauth.WithSkipPersist(context.Background()), &coreauth.Auth{
		ID: "codex-account", Provider: "codex", FileName: "codex.json", Status: coreauth.StatusActive,
		Metadata: map[string]any{"access_token": "old-secret-token", "refresh_token": "refresh-secret-token"},
	})
	if err != nil {
		t.Fatalf("register auth: %v", err)
	}
	handler := NewHandlerWithoutConfigFilePath(&config.Config{}, manager)
	body := `{"auth_index":"` + registered.EnsureIndex() + `"}`
	request := httptest.NewRequest(http.MethodPost, "/v0/management/auth-files/refresh", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = request

	handler.RefreshAuthFileCredential(ctx)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if strings.Contains(recorder.Body.String(), "secret-token") {
		t.Fatalf("response exposed credential: %s", recorder.Body.String())
	}
	var response map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response["status"] != "ok" || response["id"] != "codex-account" || response["auth_index"] != registered.EnsureIndex() {
		t.Fatalf("unexpected response: %#v", response)
	}
}
