package management

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
	coreauth "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/auth"
)

func TestImportVertexCredentialPersistsRequestedDraftDisabled(t *testing.T) {
	gin.SetMode(gin.TestMode)
	store := &memoryAuthStore{}
	manager := coreauth.NewManager(store, nil, nil)
	handler := NewHandlerWithoutConfigFilePath(&config.Config{AuthDir: t.TempDir()}, manager)
	handler.tokenStore = store
	payload := vertexServiceAccountPayload(t)

	request := vertexImportRequest(t, payload, true)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = request
	handler.ImportVertexCredential(ctx)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	items, errList := store.List(context.Background())
	if errList != nil || len(items) != 1 {
		t.Fatalf("saved items = %d, err = %v", len(items), errList)
	}
	item := items[0]
	if !item.Disabled || item.Status != coreauth.StatusDisabled || !coreauth.IsCredentialDraft(item) {
		t.Fatalf("draft auth = %#v", item)
	}
	if item.Metadata[coreauth.MetadataCredentialDraft] != true || item.Metadata["pro_draft"] != true {
		t.Fatalf("draft metadata = %#v", item.Metadata)
	}
}

func TestImportVertexCredentialWithoutDraftKeepsDefaultState(t *testing.T) {
	gin.SetMode(gin.TestMode)
	store := &memoryAuthStore{}
	manager := coreauth.NewManager(store, nil, nil)
	handler := NewHandlerWithoutConfigFilePath(&config.Config{AuthDir: t.TempDir()}, manager)
	handler.tokenStore = store

	request := vertexImportRequest(t, vertexServiceAccountPayload(t), false)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = request
	handler.ImportVertexCredential(ctx)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	items, errList := store.List(context.Background())
	if errList != nil || len(items) != 1 {
		t.Fatalf("saved items = %d, err = %v", len(items), errList)
	}
	if items[0].Disabled || coreauth.IsCredentialDraft(items[0]) {
		t.Fatalf("ordinary import unexpectedly became a draft: %#v", items[0])
	}
}

func vertexServiceAccountPayload(t *testing.T) []byte {
	t.Helper()
	key, errGenerate := rsa.GenerateKey(rand.Reader, 1024)
	if errGenerate != nil {
		t.Fatalf("generate key: %v", errGenerate)
	}
	privateKey := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
	payload, errMarshal := json.Marshal(map[string]any{
		"type":         "service_account",
		"project_id":   "project-one",
		"client_email": "vertex@example.test",
		"private_key":  string(privateKey),
	})
	if errMarshal != nil {
		t.Fatalf("marshal service account: %v", errMarshal)
	}
	return payload
}

func vertexImportRequest(t *testing.T, payload []byte, draft bool) *http.Request {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, errPart := writer.CreateFormFile("file", "service-account.json")
	if errPart != nil {
		t.Fatalf("create multipart file: %v", errPart)
	}
	if _, errWrite := part.Write(payload); errWrite != nil {
		t.Fatalf("write multipart file: %v", errWrite)
	}
	if errLocation := writer.WriteField("location", "us-central1"); errLocation != nil {
		t.Fatalf("write location: %v", errLocation)
	}
	if draft {
		if errDraft := writer.WriteField("credential_draft", "true"); errDraft != nil {
			t.Fatalf("write draft marker: %v", errDraft)
		}
	}
	if errClose := writer.Close(); errClose != nil {
		t.Fatalf("close multipart writer: %v", errClose)
	}
	request := httptest.NewRequest(http.MethodPost, "/v0/management/vertex/import", &body)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	return request
}
