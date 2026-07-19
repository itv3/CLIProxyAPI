package watcher

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/officialclient"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/runtime/executor/helps"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/watcher/synthesizer"
	coreauth "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/auth"
)

func TestOfficialClientCompatibilityHotReloadProducesModify(t *testing.T) {
	synthesize := func(enabled bool) []*coreauth.Auth {
		cfg := &config.Config{CodexKey: []config.CodexKey{{
			APIKey:  "codex-secret",
			BaseURL: "https://api.openai.com/v1",
			OfficialClientCompatibility: &officialclient.CompatibilityConfig{
				Enabled: enabled,
				Profile: "codex-desktop-0.145.0-alpha.18-v1",
			},
		}}}
		auths, err := synthesizer.NewConfigSynthesizer().Synthesize(&synthesizer.SynthesisContext{
			Config:      cfg,
			Now:         time.Now(),
			IDGenerator: synthesizer.NewStableIDGenerator(),
		})
		if err != nil {
			t.Fatalf("Synthesize() error = %v", err)
		}
		return auths
	}

	oldAuths := synthesize(false)
	newAuths := synthesize(true)
	if oldAuths[0].ID != newAuths[0].ID {
		t.Fatalf("auth ID changed: %q != %q", oldAuths[0].ID, newAuths[0].ID)
	}
	w := &Watcher{
		currentAuths: map[string]*coreauth.Auth{oldAuths[0].ID: oldAuths[0]},
		authQueue:    make(chan AuthUpdate, 1),
	}
	updates := w.prepareAuthUpdatesLocked(newAuths, false)
	if len(updates) != 1 || updates[0].Action != AuthUpdateActionModify || updates[0].ID != oldAuths[0].ID {
		t.Fatalf("updates = %#v, want one modify", updates)
	}
	manager := coreauth.NewManager(nil, nil, nil)
	if _, err := manager.Register(context.Background(), oldAuths[0]); err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	if _, err := manager.Update(context.Background(), updates[0].Auth); err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	updated, ok := manager.GetByID(oldAuths[0].ID)
	if !ok {
		t.Fatal("updated auth is missing")
	}
	decision, err := helps.ResolveOfficialClientCompatibility(updated, http.Header{"User-Agent": {"curl/8"}}, false)
	if err != nil {
		t.Fatalf("ResolveOfficialClientCompatibility() error = %v", err)
	}
	if decision.State != officialclient.DecisionApply {
		t.Fatalf("decision state = %q, want %q", decision.State, officialclient.DecisionApply)
	}
}

func TestOfficialClientCompatibilityInvalidReloadKeepsCurrentConfig(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "config.yaml")
	data := []byte(`
auth-dir: ` + filepath.Join(tempDir, "auth") + `
codex-api-key:
  - api-key: replacement-secret
    base-url: https://api.openai.com/v1
    official-client-compatibility:
      enabled: true
`)
	if err := os.WriteFile(configPath, data, 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	current := &config.Config{Debug: true}
	w := &Watcher{configPath: configPath, config: current}

	if ok := w.reloadConfig(); ok {
		t.Fatal("reloadConfig() unexpectedly succeeded")
	}
	if w.config != current || !w.config.Debug {
		t.Fatalf("current config changed: %#v", w.config)
	}
}
