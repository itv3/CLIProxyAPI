package auth

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync"
	"testing"

	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/executor"
)

type credentialRefreshStore struct {
	mu       sync.Mutex
	location string
	saveErr  error
	saved    *Auth
	saves    int
	deletes  int
}

func (s *credentialRefreshStore) List(context.Context) ([]*Auth, error) { return nil, nil }

func (s *credentialRefreshStore) Save(_ context.Context, auth *Auth) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.saveErr != nil {
		return "", s.saveErr
	}
	s.saves++
	s.saved = auth.Clone()
	return s.location, nil
}

func (s *credentialRefreshStore) Delete(context.Context, string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.deletes++
	s.saved = nil
	return nil
}

func (s *credentialRefreshStore) savedAuth() *Auth {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.saved.Clone()
}

func (s *credentialRefreshStore) operationCounts() (int, int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.saves, s.deletes
}

type credentialRefreshExecutor struct {
	provider string
	token    string
	err      error
}

type blockingCredentialRefreshExecutor struct {
	credentialRefreshExecutor
	started chan struct{}
	release chan struct{}
}

func (e blockingCredentialRefreshExecutor) Refresh(ctx context.Context, auth *Auth) (*Auth, error) {
	close(e.started)
	select {
	case <-e.release:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	return e.credentialRefreshExecutor.Refresh(ctx, auth)
}

func (e credentialRefreshExecutor) Identifier() string { return e.provider }

func (e credentialRefreshExecutor) Execute(context.Context, *Auth, cliproxyexecutor.Request, cliproxyexecutor.Options) (cliproxyexecutor.Response, error) {
	return cliproxyexecutor.Response{}, nil
}

func (e credentialRefreshExecutor) ExecuteStream(context.Context, *Auth, cliproxyexecutor.Request, cliproxyexecutor.Options) (*cliproxyexecutor.StreamResult, error) {
	return nil, nil
}

func (e credentialRefreshExecutor) Refresh(_ context.Context, auth *Auth) (*Auth, error) {
	if e.err != nil {
		return nil, e.err
	}
	auth.Metadata["access_token"] = e.token
	return auth, nil
}

func (e credentialRefreshExecutor) CountTokens(context.Context, *Auth, cliproxyexecutor.Request, cliproxyexecutor.Options) (cliproxyexecutor.Response, error) {
	return cliproxyexecutor.Response{}, nil
}

func (e credentialRefreshExecutor) HttpRequest(context.Context, *Auth, *http.Request) (*http.Response, error) {
	return nil, nil
}

func TestForceRefreshCredentialPersistsBeforeActivating(t *testing.T) {
	store := &credentialRefreshStore{location: "/tmp/codex.json"}
	manager := NewManager(store, nil, nil)
	manager.RegisterExecutor(credentialRefreshExecutor{provider: "codex", token: "fresh-access-token"})
	auth := &Auth{
		ID: "codex-account", Provider: "codex", FileName: "codex.json", Status: StatusActive,
		Metadata: map[string]any{"access_token": "stale-access-token", "refresh_token": "refresh-secret"},
	}
	registered, err := manager.Register(WithSkipPersist(context.Background()), auth)
	if err != nil {
		t.Fatalf("register auth: %v", err)
	}

	result, err := manager.ForceRefreshCredential(context.Background(), "", registered.EnsureIndex())
	if err != nil {
		t.Fatalf("ForceRefreshCredential returned error: %v", err)
	}
	if result.ID != auth.ID || result.AuthIndex != registered.EnsureIndex() || result.Provider != "codex" || result.RefreshedAt.IsZero() {
		t.Fatalf("unexpected safe result: %#v", result)
	}
	persisted := store.savedAuth()
	if persisted == nil || authAccessToken(persisted) != "fresh-access-token" {
		t.Fatalf("persisted auth = %#v", persisted)
	}
	current, ok := manager.GetByID(auth.ID)
	if !ok || authAccessToken(current) != "fresh-access-token" {
		t.Fatalf("runtime auth = %#v", current)
	}
}

func TestForceRefreshCredentialPersistenceFailureKeepsRuntimeCredential(t *testing.T) {
	store := &credentialRefreshStore{location: "/tmp/codex.json", saveErr: errors.New("disk unavailable")}
	manager := NewManager(store, nil, nil)
	manager.RegisterExecutor(credentialRefreshExecutor{provider: "codex", token: "fresh-access-token"})
	auth := &Auth{
		ID: "codex-account", Provider: "codex", FileName: "codex.json", Status: StatusActive,
		Metadata: map[string]any{"access_token": "stale-access-token", "refresh_token": "refresh-secret"},
	}
	if _, err := manager.Register(WithSkipPersist(context.Background()), auth); err != nil {
		t.Fatalf("register auth: %v", err)
	}

	_, err := manager.ForceRefreshCredential(context.Background(), auth.ID, "")
	if !errors.Is(err, ErrCredentialRefreshPersistFailed) {
		t.Fatalf("error = %v, want persistence failure", err)
	}
	current, ok := manager.GetByID(auth.ID)
	if !ok || authAccessToken(current) != "stale-access-token" || !current.LastRefreshedAt.IsZero() {
		t.Fatalf("runtime auth changed after failed persistence: %#v", current)
	}
}

func TestForceRefreshCredentialRejectsSelectorMismatchAndNonOAuth(t *testing.T) {
	store := &credentialRefreshStore{location: "/tmp/auth.json"}
	manager := NewManager(store, nil, nil)
	manager.RegisterExecutor(credentialRefreshExecutor{provider: "codex", token: "fresh"})
	first, _ := manager.Register(WithSkipPersist(context.Background()), &Auth{
		ID: "first", Provider: "codex", FileName: "first.json",
		Metadata: map[string]any{"access_token": "old", "refresh_token": "refresh"},
	})
	second, _ := manager.Register(WithSkipPersist(context.Background()), &Auth{
		ID: "second", Provider: "codex", FileName: "second.json",
		Metadata: map[string]any{"access_token": "api-key-only"},
	})

	if _, err := manager.ForceRefreshCredential(context.Background(), first.ID, second.EnsureIndex()); !errors.Is(err, ErrCredentialRefreshSelectorMismatch) {
		t.Fatalf("selector mismatch error = %v", err)
	}
	if _, err := manager.ForceRefreshCredential(context.Background(), second.ID, ""); !errors.Is(err, ErrCredentialRefreshUnsupported) {
		t.Fatalf("non-OAuth error = %v", err)
	}
}

func TestForceRefreshCredentialClassifiesInvalidGrantWithoutActivatingChanges(t *testing.T) {
	store := &credentialRefreshStore{location: "/tmp/codex.json"}
	manager := NewManager(store, nil, nil)
	manager.RegisterExecutor(credentialRefreshExecutor{
		provider: "codex",
		err:      errors.New(`token refresh failed with status 400: {"error":"invalid_grant","code":"refresh_token_reused"}`),
	})
	auth := &Auth{
		ID: "codex-account", Provider: "codex", FileName: "codex.json", Status: StatusActive,
		Metadata: map[string]any{"access_token": "stale-access-token", "refresh_token": "refresh-secret"},
	}
	if _, err := manager.Register(WithSkipPersist(context.Background()), auth); err != nil {
		t.Fatalf("register auth: %v", err)
	}

	_, err := manager.ForceRefreshCredential(context.Background(), auth.ID, "")
	if !errors.Is(err, ErrCredentialReauthorizationRequired) {
		t.Fatalf("error = %v, want reauthorization required", err)
	}
	if strings.Contains(err.Error(), "invalid_grant") || strings.Contains(err.Error(), "refresh-secret") {
		t.Fatalf("classified error exposed upstream details: %v", err)
	}
	current, ok := manager.GetByID(auth.ID)
	if !ok || authAccessToken(current) != "stale-access-token" {
		t.Fatalf("runtime auth changed after invalid grant: %#v", current)
	}
}

func TestForceRefreshCredentialPreservesConcurrentOperatorState(t *testing.T) {
	store := &credentialRefreshStore{location: "/tmp/codex.json"}
	manager := NewManager(store, nil, nil)
	started := make(chan struct{})
	release := make(chan struct{})
	manager.RegisterExecutor(blockingCredentialRefreshExecutor{
		credentialRefreshExecutor: credentialRefreshExecutor{provider: "codex", token: "fresh-access-token"},
		started:                   started,
		release:                   release,
	})
	auth := &Auth{
		ID: "codex-account", Provider: "codex", FileName: "codex.json", Status: StatusActive,
		Metadata: map[string]any{
			"access_token":  "stale-access-token",
			"refresh_token": "refresh-secret",
			"disabled":      false,
			"note":          "before",
		},
	}
	if _, err := manager.Register(WithSkipPersist(context.Background()), auth); err != nil {
		t.Fatalf("register auth: %v", err)
	}

	resultCh := make(chan error, 1)
	go func() {
		_, err := manager.ForceRefreshCredential(context.Background(), auth.ID, "")
		resultCh <- err
	}()
	<-started

	concurrent, ok := manager.GetByID(auth.ID)
	if !ok || concurrent == nil {
		t.Fatal("auth disappeared before concurrent update")
	}
	concurrent.Disabled = true
	concurrent.Status = StatusDisabled
	concurrent.StatusMessage = "disabled via management API"
	concurrent.Metadata["disabled"] = true
	concurrent.Metadata["note"] = "changed while refreshing"
	if _, err := manager.Update(WithSkipPersist(context.Background()), concurrent); err != nil {
		t.Fatalf("concurrent update: %v", err)
	}
	close(release)

	if err := <-resultCh; err != nil {
		t.Fatalf("ForceRefreshCredential returned error: %v", err)
	}
	for label, got := range map[string]*Auth{
		"persisted": store.savedAuth(),
		"runtime": func() *Auth {
			current, _ := manager.GetByID(auth.ID)
			return current
		}(),
	} {
		if got == nil || authAccessToken(got) != "fresh-access-token" {
			t.Fatalf("%s auth did not receive refreshed token: %#v", label, got)
		}
		if !got.Disabled || got.Status != StatusDisabled || got.StatusMessage != "disabled via management API" {
			t.Fatalf("%s operator state was overwritten: %#v", label, got)
		}
		if disabled, _ := got.Metadata["disabled"].(bool); !disabled || got.Metadata["note"] != "changed while refreshing" {
			t.Fatalf("%s operator metadata was overwritten: %#v", label, got.Metadata)
		}
	}
}

func TestForceRefreshCredentialDoesNotRecreateConcurrentlyRemovedAuth(t *testing.T) {
	store := &credentialRefreshStore{location: "/tmp/codex.json"}
	manager := NewManager(store, nil, nil)
	started := make(chan struct{})
	release := make(chan struct{})
	manager.RegisterExecutor(blockingCredentialRefreshExecutor{
		credentialRefreshExecutor: credentialRefreshExecutor{provider: "codex", token: "fresh-access-token"},
		started:                   started,
		release:                   release,
	})
	auth := &Auth{
		ID: "codex-account", Provider: "codex", FileName: "codex.json", Status: StatusActive,
		Metadata: map[string]any{"access_token": "stale-access-token", "refresh_token": "refresh-secret"},
	}
	if _, err := manager.Register(WithSkipPersist(context.Background()), auth); err != nil {
		t.Fatalf("register auth: %v", err)
	}

	resultCh := make(chan error, 1)
	go func() {
		_, err := manager.ForceRefreshCredential(context.Background(), auth.ID, "")
		resultCh <- err
	}()
	<-started
	manager.Remove(context.Background(), auth.ID)
	close(release)

	if err := <-resultCh; !errors.Is(err, ErrCredentialRefreshAuthNotFound) {
		t.Fatalf("error = %v, want auth not found", err)
	}
	if _, ok := manager.GetByID(auth.ID); ok {
		t.Fatal("concurrently removed auth was restored to runtime")
	}
	if saves, deletes := store.operationCounts(); saves != 0 || deletes != 0 {
		t.Fatalf("store operations = saves:%d deletes:%d, want no persistence after removal", saves, deletes)
	}
}
