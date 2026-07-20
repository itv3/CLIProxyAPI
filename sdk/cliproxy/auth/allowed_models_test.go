package auth

import (
	"reflect"
	"testing"
)

func TestAllowedModelPolicyForAuthReadsAttributesAndMetadata(t *testing.T) {
	authFromAttributes := &Auth{Attributes: map[string]string{
		AttributeAllowedModels:       `["gpt-5","gemini-*"]`,
		AttributeAllowedModelAliases: `["friendly-name"]`,
	}}
	policy := AllowedModelPolicyForAuth(authFromAttributes)
	if !policy.Configured {
		t.Fatal("attribute policy was not configured")
	}
	if !reflect.DeepEqual(policy.Patterns, []string{"gpt-5", "gemini-*"}) {
		t.Fatalf("patterns = %#v", policy.Patterns)
	}
	if !reflect.DeepEqual(policy.Aliases, []string{"friendly-name"}) {
		t.Fatalf("aliases = %#v", policy.Aliases)
	}

	authFromMetadata := &Auth{Metadata: map[string]any{
		"allowed_models": []any{"claude-*"},
		"model_aliases": []any{
			map[string]any{"name": "claude-sonnet", "alias": "sonnet"},
			map[string]any{"name": "identity", "alias": "identity"},
			map[string]any{"name": "implicit-identity"},
		},
	}}
	metadataPolicy := AllowedModelPolicyForAuth(authFromMetadata)
	if !reflect.DeepEqual(metadataPolicy.Patterns, []string{"claude-*"}) {
		t.Fatalf("metadata patterns = %#v", metadataPolicy.Patterns)
	}
	if !reflect.DeepEqual(metadataPolicy.Aliases, []string{"sonnet"}) {
		t.Fatalf("metadata aliases = %#v", metadataPolicy.Aliases)
	}

	aliasOnlyAuth := &Auth{Attributes: map[string]string{
		AttributeAllowedModelAliases: `["glm-5.2"]`,
	}}
	aliasOnlyPolicy := AllowedModelPolicyForAuth(aliasOnlyAuth)
	if aliasOnlyPolicy.Configured {
		t.Fatal("alias-only policy unexpectedly configured an allowlist")
	}
	if len(aliasOnlyPolicy.Patterns) != 0 {
		t.Fatalf("alias-only patterns = %#v", aliasOnlyPolicy.Patterns)
	}
	if !reflect.DeepEqual(aliasOnlyPolicy.Aliases, []string{"glm-5.2"}) {
		t.Fatalf("alias-only aliases = %#v", aliasOnlyPolicy.Aliases)
	}
}

func TestAllowedModelPatternValidation(t *testing.T) {
	tests := map[string]bool{
		"gpt-5":     true,
		"gpt-*":     true,
		"*":         true,
		"gpt-*mini": false,
		"gpt-**":    false,
		"":          false,
		"   ":       false,
	}
	for pattern, want := range tests {
		if got := IsValidAllowedModelPattern(pattern); got != want {
			t.Fatalf("IsValidAllowedModelPattern(%q) = %t, want %t", pattern, got, want)
		}
	}
}

func TestAllowedModelRuleVersionChangesWithPolicy(t *testing.T) {
	auth := &Auth{ID: "auth", Prefix: "team"}
	SetAllowedModelsAttribute(auth, []string{"model-a"})
	first := AllowedModelRuleVersion(auth)
	if first == "" {
		t.Fatal("rule version is empty")
	}
	if repeated := AllowedModelRuleVersion(auth); repeated != first {
		t.Fatalf("rule version is not stable: %q != %q", repeated, first)
	}
	SetAllowedModelsAttribute(auth, []string{"model-b"})
	if changed := AllowedModelRuleVersion(auth); changed == first {
		t.Fatal("rule version did not change after allowlist update")
	}
}
