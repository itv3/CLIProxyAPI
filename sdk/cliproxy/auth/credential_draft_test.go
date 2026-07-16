package auth

import "testing"

func TestCredentialDraftPolicyRequiresExplicitMarker(t *testing.T) {
	ordinary := &Auth{Disabled: true, Metadata: map[string]any{"disabled": true}}
	if AllowsDisabledInitialPersist(ordinary) {
		t.Fatal("ordinary disabled auth was allowed to persist initially")
	}

	draft := &Auth{}
	MarkCredentialDraft(draft)
	if !draft.Disabled || draft.Status != StatusDisabled {
		t.Fatalf("draft disabled/status = %t/%q", draft.Disabled, draft.Status)
	}
	if !AllowsDisabledInitialPersist(draft) {
		t.Fatal("explicit credential draft was not allowed to persist initially")
	}
	if draft.Metadata["pro_draft"] != true || draft.Metadata[MetadataCredentialDraft] != true {
		t.Fatalf("draft metadata = %#v", draft.Metadata)
	}
}

func TestCredentialDraftPolicyAcceptsLegacyMarker(t *testing.T) {
	auth := &Auth{Disabled: true, Metadata: map[string]any{"pro_draft": "true"}}
	if !AllowsDisabledInitialPersist(auth) {
		t.Fatal("legacy draft marker was not accepted")
	}
}
