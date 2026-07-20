package auth

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
)

const (
	// AttributeAllowedModels stores the client-visible model allowlist for one auth.
	AttributeAllowedModels = "allowed_models"
	// AttributeAllowedModelAliases stores client-visible aliases that are implicitly allowed.
	AttributeAllowedModelAliases = "allowed_model_aliases"
)

// AllowedModelPolicy is the normalized model policy attached to one auth.
// A policy is configured only when at least one allowlist entry is present.
type AllowedModelPolicy struct {
	Configured bool
	Patterns   []string
	Aliases    []string
}

// NormalizeAllowedModels trims and de-duplicates model names while preserving order.
// Invalid wildcard forms are retained so runtime matching fails closed instead of
// accidentally turning a malformed non-empty allowlist into "allow all".
func NormalizeAllowedModels(models []string) []string {
	if len(models) == 0 {
		return nil
	}
	out := make([]string, 0, len(models))
	seen := make(map[string]struct{}, len(models))
	for _, raw := range models {
		model := strings.TrimSpace(raw)
		if model == "" {
			continue
		}
		key := strings.ToLower(model)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, model)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// IsValidAllowedModelPattern reports whether a pattern is an exact model name
// or contains exactly one trailing wildcard.
func IsValidAllowedModelPattern(pattern string) bool {
	pattern = strings.TrimSpace(pattern)
	if pattern == "" {
		return false
	}
	count := strings.Count(pattern, "*")
	return count == 0 || (count == 1 && strings.HasSuffix(pattern, "*"))
}

// SetAllowedModelsAttribute stores a normalized allowlist on an auth entry.
// Empty lists remove the attribute; when mapped aliases exist, registration retains only them.
func SetAllowedModelsAttribute(auth *Auth, models []string) {
	setModelListAttribute(auth, AttributeAllowedModels, models)
}

// SetAllowedModelAliasesAttribute stores aliases that model mappings implicitly allow.
func SetAllowedModelAliasesAttribute(auth *Auth, aliases []string) {
	setModelListAttribute(auth, AttributeAllowedModelAliases, aliases)
}

func setModelListAttribute(auth *Auth, key string, models []string) {
	if auth == nil {
		return
	}
	models = NormalizeAllowedModels(models)
	if len(models) == 0 {
		if auth.Attributes != nil {
			delete(auth.Attributes, key)
		}
		return
	}
	raw, errMarshal := json.Marshal(models)
	if errMarshal != nil {
		return
	}
	if auth.Attributes == nil {
		auth.Attributes = make(map[string]string)
	}
	auth.Attributes[key] = string(raw)
}

// AllowedModelsFromAttributes returns the configured client-visible allowlist.
func AllowedModelsFromAttributes(attributes map[string]string) []string {
	return modelListFromAttribute(attributes, AttributeAllowedModels)
}

// AllowedModelAliasesFromAttributes returns aliases implicitly allowed by mappings.
func AllowedModelAliasesFromAttributes(attributes map[string]string) []string {
	return modelListFromAttribute(attributes, AttributeAllowedModelAliases)
}

func modelListFromAttribute(attributes map[string]string, key string) []string {
	if len(attributes) == 0 {
		return nil
	}
	raw := strings.TrimSpace(attributes[key])
	if raw == "" {
		return nil
	}
	var models []string
	if errUnmarshal := json.Unmarshal([]byte(raw), &models); errUnmarshal != nil {
		models = strings.Split(raw, ",")
	}
	return NormalizeAllowedModels(models)
}

// AllowedModelPolicyForAuth returns the effective allowlist policy for an auth.
func AllowedModelPolicyForAuth(auth *Auth) AllowedModelPolicy {
	if auth == nil {
		return AllowedModelPolicy{}
	}
	patterns := AllowedModelsFromAttributes(auth.Attributes)
	if len(patterns) == 0 {
		patterns = modelListFromMetadata(auth.Metadata, "allowed_models", "allowed-models")
	}
	aliases := AllowedModelAliasesFromAttributes(auth.Attributes)
	if len(aliases) == 0 {
		aliases = modelAliasesFromMetadata(auth.Metadata)
	}
	return AllowedModelPolicy{
		Configured: len(patterns) > 0,
		Patterns:   patterns,
		Aliases:    aliases,
	}
}

func modelListFromMetadata(metadata map[string]any, keys ...string) []string {
	if len(metadata) == 0 {
		return nil
	}
	for _, key := range keys {
		raw, ok := metadata[key]
		if !ok || raw == nil {
			continue
		}
		models := make([]string, 0)
		switch value := raw.(type) {
		case []string:
			models = append(models, value...)
		case []any:
			for _, item := range value {
				if model, okString := item.(string); okString {
					models = append(models, model)
				}
			}
		case string:
			models = append(models, strings.Split(value, ",")...)
		}
		if normalized := NormalizeAllowedModels(models); len(normalized) > 0 {
			return normalized
		}
	}
	return nil
}

func modelAliasesFromMetadata(metadata map[string]any) []string {
	if len(metadata) == 0 {
		return nil
	}
	var raw any
	for _, key := range []string{"model_aliases", "model-aliases"} {
		if value, ok := metadata[key]; ok && value != nil {
			raw = value
			break
		}
	}
	if raw == nil {
		return nil
	}
	data, errMarshal := json.Marshal(raw)
	if errMarshal != nil {
		return nil
	}
	var mappings []struct {
		Name  string `json:"name"`
		Alias string `json:"alias"`
	}
	if errUnmarshal := json.Unmarshal(data, &mappings); errUnmarshal != nil {
		return nil
	}
	aliases := make([]string, 0, len(mappings))
	for _, mapping := range mappings {
		name := strings.TrimSpace(mapping.Name)
		alias := strings.TrimSpace(mapping.Alias)
		if alias != "" && !strings.EqualFold(alias, name) {
			aliases = append(aliases, alias)
		}
	}
	return NormalizeAllowedModels(aliases)
}

// AllowedModelRuleVersion returns a stable digest of the runtime model policy.
func AllowedModelRuleVersion(auth *Auth) string {
	if auth == nil {
		return ""
	}
	policy := AllowedModelPolicyForAuth(auth)
	payload := struct {
		AllowedModels []string `json:"allowed_models"`
		Aliases       []string `json:"aliases"`
		ModelAliases  string   `json:"model_aliases"`
		ModelsHash    string   `json:"models_hash"`
		Prefix        string   `json:"prefix"`
	}{
		AllowedModels: policy.Patterns,
		Aliases:       policy.Aliases,
		Prefix:        strings.TrimSpace(auth.Prefix),
	}
	if auth.Attributes != nil {
		payload.ModelAliases = strings.TrimSpace(auth.Attributes[oauthModelAliasesAttributeKey])
		payload.ModelsHash = strings.TrimSpace(auth.Attributes["models_hash"])
	}
	raw, errMarshal := json.Marshal(payload)
	if errMarshal != nil {
		return ""
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}
