package auth

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/auth"
)

type testTokenStorage struct {
	meta map[string]any
}

func (s *testTokenStorage) SetMetadata(meta map[string]any) { s.meta = meta }

func (s *testTokenStorage) SaveTokenToFile(authFilePath string) error {
	raw, err := json.Marshal(s.meta)
	if err != nil {
		return err
	}
	return os.WriteFile(authFilePath, raw, 0o600)
}

func TestFileTokenStore_Save_DisabledPersistsFlagForTokenStorage(t *testing.T) {
	ctx := context.Background()
	baseDir := t.TempDir()
	path := filepath.Join(baseDir, "disabled.json")

	if err := os.WriteFile(path, []byte(`{"type":"test","disabled":true}`), 0o600); err != nil {
		t.Fatalf("seed auth file: %v", err)
	}

	store := NewFileTokenStore()
	store.SetBaseDir(baseDir)
	storage := &testTokenStorage{}

	auth := &cliproxyauth.Auth{
		ID:       "disabled.json",
		Provider: "test",
		FileName: "disabled.json",
		Disabled: true,
		Storage:  storage,
		Metadata: map[string]any{"type": "test"},
	}

	if _, err := store.Save(ctx, auth); err != nil {
		t.Fatalf("Save() error: %v", err)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read auth file: %v", err)
	}
	var meta map[string]any
	if err := json.Unmarshal(raw, &meta); err != nil {
		t.Fatalf("unmarshal auth file: %v", err)
	}
	if disabled, _ := meta["disabled"].(bool); !disabled {
		t.Fatalf("disabled=%v, want true (raw=%s)", meta["disabled"], string(raw))
	}
}

func TestFileTokenStore_Save_DraftCreatesMissingDisabledAuth(t *testing.T) {
	ctx := context.Background()
	baseDir := t.TempDir()
	store := NewFileTokenStore()
	store.SetBaseDir(baseDir)

	draft := &cliproxyauth.Auth{
		ID:       "draft.json",
		Provider: "test",
		FileName: "draft.json",
		Metadata: map[string]any{"type": "test"},
	}
	cliproxyauth.MarkCredentialDraft(draft)

	path, errSave := store.Save(ctx, draft)
	if errSave != nil {
		t.Fatalf("Save() draft error: %v", errSave)
	}
	if path != filepath.Join(baseDir, "draft.json") {
		t.Fatalf("draft path = %q", path)
	}
	raw, errRead := os.ReadFile(path)
	if errRead != nil {
		t.Fatalf("read draft: %v", errRead)
	}
	var metadata map[string]any
	if errUnmarshal := json.Unmarshal(raw, &metadata); errUnmarshal != nil {
		t.Fatalf("unmarshal draft: %v", errUnmarshal)
	}
	if metadata["disabled"] != true || metadata["pro_draft"] != true || metadata[cliproxyauth.MetadataCredentialDraft] != true {
		t.Fatalf("draft metadata = %#v", metadata)
	}
}

func TestFileTokenStore_Save_OrdinaryMissingDisabledAuthIsSkipped(t *testing.T) {
	baseDir := t.TempDir()
	store := NewFileTokenStore()
	store.SetBaseDir(baseDir)
	auth := &cliproxyauth.Auth{
		ID:       "ordinary.json",
		Provider: "test",
		FileName: "ordinary.json",
		Disabled: true,
		Metadata: map[string]any{"type": "test", "disabled": true},
	}

	path, errSave := store.Save(context.Background(), auth)
	if errSave != nil {
		t.Fatalf("Save() ordinary disabled error: %v", errSave)
	}
	if path != "" {
		t.Fatalf("ordinary disabled path = %q, want empty", path)
	}
	if _, errStat := os.Stat(filepath.Join(baseDir, "ordinary.json")); !os.IsNotExist(errStat) {
		t.Fatalf("ordinary disabled auth was recreated, stat error = %v", errStat)
	}
}
