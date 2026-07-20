package cliproxy

import (
	"strings"

	coreauth "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/auth"
)

func filterAllowedModelsForAuth(auth *coreauth.Auth, models []*ModelInfo) []*ModelInfo {
	policy := coreauth.AllowedModelPolicyForAuth(auth)
	if len(models) == 0 {
		return models
	}
	if !policy.Configured && len(policy.Aliases) == 0 {
		return models
	}

	filtered := make([]*ModelInfo, 0, len(models))
	for _, model := range models {
		if model == nil {
			continue
		}
		modelID := strings.TrimSpace(model.ID)
		if modelID == "" {
			continue
		}
		if matchesAllowedModelPattern(modelID, policy.Patterns) || matchesImplicitAllowedAlias(modelID, auth.Prefix, policy.Aliases) {
			filtered = append(filtered, model)
		}
	}
	return filtered
}

func matchesAllowedModelPattern(modelID string, patterns []string) bool {
	modelID = strings.ToLower(strings.TrimSpace(modelID))
	if modelID == "" {
		return false
	}
	for _, rawPattern := range patterns {
		pattern := strings.ToLower(strings.TrimSpace(rawPattern))
		if !coreauth.IsValidAllowedModelPattern(pattern) {
			continue
		}
		if strings.HasSuffix(pattern, "*") {
			if strings.HasPrefix(modelID, strings.TrimSuffix(pattern, "*")) {
				return true
			}
			continue
		}
		if modelID == pattern {
			return true
		}
	}
	return false
}

func matchesImplicitAllowedAlias(modelID, prefix string, aliases []string) bool {
	modelID = strings.TrimSpace(modelID)
	prefix = strings.Trim(strings.TrimSpace(prefix), "/")
	for _, alias := range aliases {
		alias = strings.TrimSpace(alias)
		if alias == "" {
			continue
		}
		if matchesAllowedModelPattern(modelID, []string{alias}) {
			return true
		}
		if prefix != "" && matchesAllowedModelPattern(modelID, []string{prefix + "/" + alias}) {
			return true
		}
	}
	return false
}
