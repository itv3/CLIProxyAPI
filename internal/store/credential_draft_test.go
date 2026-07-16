package store

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	git "github.com/go-git/go-git/v6"
	"github.com/go-git/go-git/v6/plumbing"
	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/auth"
)

func TestObjectTokenStoreCredentialDraftCompatibility(t *testing.T) {
	var uploadCount atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		uploadCount.Add(1)
		w.Header().Set("ETag", `"draft-test"`)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	endpoint, errParse := url.Parse(server.URL)
	if errParse != nil {
		t.Fatalf("parse object endpoint: %v", errParse)
	}
	root := t.TempDir()
	store, errNew := NewObjectTokenStore(ObjectStoreConfig{
		Endpoint:  endpoint.Host,
		Bucket:    "draft-test",
		AccessKey: "access",
		SecretKey: "secret",
		Region:    "us-east-1",
		LocalRoot: root,
		PathStyle: true,
	})
	if errNew != nil {
		t.Fatalf("NewObjectTokenStore() error: %v", errNew)
	}

	assertOrdinaryDisabledAuthSkipped(t, store, filepath.Join(store.AuthDir(), "ordinary.json"))
	if got := uploadCount.Load(); got != 0 {
		t.Fatalf("ordinary disabled auth triggered %d object requests", got)
	}

	draft := newCredentialDraft("draft.json")
	path, errSave := store.Save(context.Background(), draft)
	if errSave != nil {
		t.Fatalf("Save() object draft error: %v", errSave)
	}
	assertCredentialDraftFile(t, path)
	if got := uploadCount.Load(); got != 1 {
		t.Fatalf("draft triggered %d object requests, want 1", got)
	}
}

func TestPostgresStoreCredentialDraftCompatibility(t *testing.T) {
	connector := &draftDBConnector{}
	db := sql.OpenDB(connector)
	t.Cleanup(func() { _ = db.Close() })
	authDir := filepath.Join(t.TempDir(), "auths")
	if errMkdir := os.MkdirAll(authDir, 0o700); errMkdir != nil {
		t.Fatalf("create auth dir: %v", errMkdir)
	}
	store := &PostgresStore{
		db:      db,
		cfg:     PostgresStoreConfig{AuthTable: defaultAuthTable},
		authDir: authDir,
	}

	assertOrdinaryDisabledAuthSkipped(t, store, filepath.Join(authDir, "ordinary.json"))
	if got := connector.execCount.Load(); got != 0 {
		t.Fatalf("ordinary disabled auth triggered %d database writes", got)
	}

	draft := newCredentialDraft("draft.json")
	path, errSave := store.Save(context.Background(), draft)
	if errSave != nil {
		t.Fatalf("Save() postgres draft error: %v", errSave)
	}
	assertCredentialDraftFile(t, path)
	if got := connector.execCount.Load(); got != 1 {
		t.Fatalf("draft triggered %d database writes, want 1", got)
	}
}

func TestGitTokenStoreCredentialDraftCompatibility(t *testing.T) {
	root := t.TempDir()
	remoteDir := setupGitRemoteRepository(t, root, "master", testBranchSpec{name: "master", contents: "base\n"})
	baseDir := filepath.Join(root, "workspace", "auths")
	store := NewGitTokenStore(remoteDir, "", "", "master")
	store.SetBaseDir(baseDir)

	assertOrdinaryDisabledAuthSkipped(t, store, filepath.Join(baseDir, "ordinary.json"))

	draft := newCredentialDraft("draft.json")
	path, errSave := store.Save(context.Background(), draft)
	if errSave != nil {
		t.Fatalf("Save() git draft error: %v", errSave)
	}
	assertCredentialDraftFile(t, path)
	assertRemoteAuthFileExists(t, remoteDir, "master", "auths/draft.json")
}

type draftStore interface {
	Save(context.Context, *cliproxyauth.Auth) (string, error)
}

func assertOrdinaryDisabledAuthSkipped(t *testing.T, store draftStore, expectedPath string) {
	t.Helper()
	auth := &cliproxyauth.Auth{
		ID:       "ordinary.json",
		FileName: "ordinary.json",
		Provider: "test",
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
	if _, errStat := os.Stat(expectedPath); !os.IsNotExist(errStat) {
		t.Fatalf("ordinary disabled auth was recreated, stat error = %v", errStat)
	}
}

func newCredentialDraft(fileName string) *cliproxyauth.Auth {
	auth := &cliproxyauth.Auth{
		ID:       fileName,
		FileName: fileName,
		Provider: "test",
		Metadata: map[string]any{"type": "test"},
	}
	cliproxyauth.MarkCredentialDraft(auth)
	return auth
}

func assertCredentialDraftFile(t *testing.T, path string) {
	t.Helper()
	raw, errRead := os.ReadFile(path)
	if errRead != nil {
		t.Fatalf("read credential draft: %v", errRead)
	}
	if !containsAll(raw, []string{`"disabled":true`, `"credential_draft":true`, `"pro_draft":true`}) {
		t.Fatalf("credential draft payload = %s", raw)
	}
}

func containsAll(data []byte, fragments []string) bool {
	text := string(data)
	for _, fragment := range fragments {
		if !strings.Contains(text, fragment) {
			return false
		}
	}
	return true
}

func assertRemoteAuthFileExists(t *testing.T, remoteDir, branch, path string) {
	t.Helper()
	repo, errOpen := git.PlainOpen(remoteDir)
	if errOpen != nil {
		t.Fatalf("open remote repository: %v", errOpen)
	}
	ref, errRef := repo.Reference(plumbing.NewBranchReferenceName(branch), false)
	if errRef != nil {
		t.Fatalf("read remote branch: %v", errRef)
	}
	commit, errCommit := repo.CommitObject(ref.Hash())
	if errCommit != nil {
		t.Fatalf("read remote commit: %v", errCommit)
	}
	tree, errTree := commit.Tree()
	if errTree != nil {
		t.Fatalf("read remote tree: %v", errTree)
	}
	if _, errFile := tree.File(path); errFile != nil {
		t.Fatalf("remote auth file %q is missing: %v", path, errFile)
	}
}

type draftDBConnector struct {
	execCount atomic.Int64
}

func (c *draftDBConnector) Connect(context.Context) (driver.Conn, error) {
	return &draftDBConn{execCount: &c.execCount}, nil
}

func (c *draftDBConnector) Driver() driver.Driver { return draftDBDriver{} }

type draftDBDriver struct{}

func (draftDBDriver) Open(string) (driver.Conn, error) {
	return nil, errors.New("use connector")
}

type draftDBConn struct {
	execCount *atomic.Int64
}

func (c *draftDBConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("prepare is not supported")
}

func (c *draftDBConn) Close() error { return nil }

func (c *draftDBConn) Begin() (driver.Tx, error) {
	return nil, errors.New("transactions are not supported")
}

func (c *draftDBConn) ExecContext(context.Context, string, []driver.NamedValue) (driver.Result, error) {
	c.execCount.Add(1)
	return driver.RowsAffected(1), nil
}
