package auth

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/registry"
)

var (
	ErrCredentialRefreshSelectorRequired = errors.New("credential refresh selector is required")
	ErrCredentialRefreshAuthNotFound     = errors.New("credential refresh auth not found")
	ErrCredentialRefreshSelectorMismatch = errors.New("credential refresh selectors do not identify the same auth")
	ErrCredentialRefreshUnsupported      = errors.New("credential refresh is unsupported for this auth")
	ErrCredentialReauthorizationRequired = errors.New("credential reauthorization is required")
	ErrCredentialRefreshFailed           = errors.New("credential refresh failed")
	ErrCredentialRefreshPersistFailed    = errors.New("credential refresh persistence failed")
)

// CredentialRefreshResult 只包含非敏感标识及刷新元数据。
type CredentialRefreshResult struct {
	ID          string
	AuthIndex   string
	Provider    string
	RefreshedAt time.Time
}

// ForceRefreshCredential 刷新并持久化一个 OAuth 凭据。
// 只有新凭据成功保存后，才会替换运行时认证。
func (m *Manager) ForceRefreshCredential(ctx context.Context, id string, authIndex string) (CredentialRefreshResult, error) {
	if m == nil {
		return CredentialRefreshResult{}, ErrCredentialRefreshAuthNotFound
	}
	if ctx == nil {
		ctx = context.Background()
	}
	target, err := m.resolveCredentialRefreshTarget(id, authIndex)
	if err != nil {
		return CredentialRefreshResult{}, err
	}

	unlock := m.lockCredentialMutation(target.ID)
	defer unlock()

	m.mu.RLock()
	current := m.auths[target.ID]
	var executor ProviderExecutor
	if current != nil {
		executor = m.executors[current.Provider]
	}
	m.mu.RUnlock()
	if current == nil {
		return CredentialRefreshResult{}, ErrCredentialRefreshAuthNotFound
	}
	current = current.Clone()
	if err = validateCredentialRefreshTarget(ctx, m, current, executor); err != nil {
		return CredentialRefreshResult{}, err
	}

	updated, err := executor.Refresh(ctx, current.Clone())
	if err != nil {
		if credentialRefreshNeedsReauthorization(err) {
			return CredentialRefreshResult{}, ErrCredentialReauthorizationRequired
		}
		return CredentialRefreshResult{}, fmt.Errorf("%w: %v", ErrCredentialRefreshFailed, err)
	}
	if updated == nil {
		return CredentialRefreshResult{}, fmt.Errorf("%w: executor returned no auth", ErrCredentialRefreshFailed)
	}
	if strings.TrimSpace(updated.ID) != current.ID || !strings.EqualFold(strings.TrimSpace(updated.Provider), strings.TrimSpace(current.Provider)) {
		return CredentialRefreshResult{}, fmt.Errorf("%w: executor changed auth identity", ErrCredentialRefreshFailed)
	}

	// 刷新可能执行较慢的网络请求，因此不能一直持有 Manager 互斥锁。
	// 请求返回后重新读取认证，只合并执行器产生的凭据变化，避免覆盖管理员并发修改的状态。
	m.mu.RLock()
	latest := m.auths[current.ID]
	if latest != nil {
		latest = latest.Clone()
	}
	m.mu.RUnlock()
	if latest == nil {
		return CredentialRefreshResult{}, ErrCredentialRefreshAuthNotFound
	}
	if !strings.EqualFold(strings.TrimSpace(latest.Provider), strings.TrimSpace(current.Provider)) {
		return CredentialRefreshResult{}, fmt.Errorf("%w: auth provider changed during refresh", ErrCredentialRefreshFailed)
	}
	updated = mergeCredentialRefreshResult(current, updated, latest)
	if !authHasRefreshCredential(updated) {
		return CredentialRefreshResult{}, fmt.Errorf("%w: refreshed auth lost its refresh credential", ErrCredentialRefreshFailed)
	}

	now := time.Now().UTC()
	updated.LastRefreshedAt = now
	updated.NextRefreshAfter = time.Time{}
	updated.LastError = nil
	updated.Unavailable = false
	if updated.Disabled || updated.Status == StatusDisabled {
		updated.Disabled = true
		updated.Status = StatusDisabled
	} else {
		updated.StatusMessage = ""
		if updated.Status == StatusError {
			updated.Status = StatusActive
		}
	}
	updated.UpdatedAt = now
	modelsToResume := clearUnauthorizedModelStates(updated, now)
	if m.shouldRefresh(updated, now) {
		updated.NextRefreshAfter = now.Add(refreshIneffectiveBackoff)
	}

	location, err := m.store.Save(ctx, updated)
	if err != nil {
		return CredentialRefreshResult{}, fmt.Errorf("%w: %v", ErrCredentialRefreshPersistFailed, err)
	}
	if strings.TrimSpace(location) == "" {
		return CredentialRefreshResult{}, fmt.Errorf("%w: store returned no persisted location", ErrCredentialRefreshPersistFailed)
	}

	saved, err := m.Update(WithSkipPersist(ctx), updated)
	if err != nil {
		return CredentialRefreshResult{}, fmt.Errorf("%w: %v", ErrCredentialRefreshFailed, err)
	}
	if saved == nil {
		_ = m.store.Delete(ctx, location)
		return CredentialRefreshResult{}, ErrCredentialRefreshAuthNotFound
	}
	for _, model := range modelsToResume {
		registry.GetGlobalRegistry().ResumeClientModel(saved.ID, model)
	}

	return CredentialRefreshResult{
		ID:          saved.ID,
		AuthIndex:   saved.EnsureIndex(),
		Provider:    saved.Provider,
		RefreshedAt: saved.LastRefreshedAt,
	}, nil
}

// WithCredentialMutation 将同一认证 ID 的外部修改与令牌刷新串行化。
// 修改函数必须在取得锁后重新读取认证。
func (m *Manager) WithCredentialMutation(id string, mutation func() error) error {
	if mutation == nil {
		return nil
	}
	if m == nil || strings.TrimSpace(id) == "" {
		return ErrCredentialRefreshAuthNotFound
	}
	unlock := m.lockCredentialMutation(id)
	defer unlock()
	return mutation()
}

func (m *Manager) lockCredentialMutation(id string) func() {
	id = strings.TrimSpace(id)
	lockValue, _ := m.refreshLocks.LoadOrStore(id, &authRefreshLock{})
	lock, _ := lockValue.(*authRefreshLock)
	if lock == nil {
		lock = &authRefreshLock{}
		m.refreshLocks.Store(id, lock)
	}
	lock.mu.Lock()
	return lock.mu.Unlock
}

func mergeCredentialRefreshResult(base *Auth, refreshed *Auth, latest *Auth) *Auth {
	merged := latest.Clone()
	if merged == nil {
		return nil
	}
	if merged.Metadata == nil {
		merged.Metadata = make(map[string]any)
	}
	for key, refreshedValue := range refreshed.Metadata {
		baseValue, existed := base.Metadata[key]
		if existed && reflect.DeepEqual(baseValue, refreshedValue) {
			continue
		}
		merged.Metadata[key] = refreshedValue
	}
	if refreshed.Runtime != nil {
		merged.Runtime = refreshed.Runtime
	}
	return merged
}

func credentialRefreshNeedsReauthorization(err error) bool {
	if err == nil {
		return false
	}
	if isUnauthorizedError(err) {
		return true
	}
	message := strings.ToLower(err.Error())
	for _, marker := range []string{
		"invalid_grant",
		"refresh_token_reused",
		"refresh token revoked",
		"refresh token has been revoked",
		"refresh token expired",
		"refresh token is expired",
	} {
		if strings.Contains(message, marker) {
			return true
		}
	}
	return false
}

func (m *Manager) resolveCredentialRefreshTarget(id string, authIndex string) (*Auth, error) {
	id = strings.TrimSpace(id)
	authIndex = strings.TrimSpace(authIndex)
	if id == "" && authIndex == "" {
		return nil, ErrCredentialRefreshSelectorRequired
	}

	m.mu.RLock()
	defer m.mu.RUnlock()
	var target *Auth
	if id != "" {
		target = m.auths[id]
		if target == nil {
			return nil, ErrCredentialRefreshAuthNotFound
		}
	}
	if authIndex != "" {
		var indexMatch *Auth
		for _, candidate := range m.auths {
			if candidate == nil {
				continue
			}
			candidateCopy := candidate.Clone()
			if strings.TrimSpace(candidateCopy.EnsureIndex()) != authIndex {
				continue
			}
			if indexMatch != nil && indexMatch.ID != candidate.ID {
				return nil, ErrCredentialRefreshSelectorMismatch
			}
			indexMatch = candidate
		}
		if indexMatch == nil {
			return nil, ErrCredentialRefreshAuthNotFound
		}
		if target != nil && target.ID != indexMatch.ID {
			return nil, ErrCredentialRefreshSelectorMismatch
		}
		target = indexMatch
	}
	return target.Clone(), nil
}

func validateCredentialRefreshTarget(ctx context.Context, manager *Manager, auth *Auth, executor ProviderExecutor) error {
	if executor == nil || !authHasRefreshCredential(auth) {
		return ErrCredentialRefreshUnsupported
	}
	if manager.store == nil || auth.Metadata == nil || shouldSkipPersist(ctx) || IsConfigAPIKeyAuth(auth) || IsPluginVirtualAuth(auth) {
		return ErrCredentialRefreshUnsupported
	}
	if auth.Attributes != nil && strings.EqualFold(strings.TrimSpace(auth.Attributes["runtime_only"]), "true") {
		return ErrCredentialRefreshUnsupported
	}
	return nil
}
