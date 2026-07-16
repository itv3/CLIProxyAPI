package auth

import (
	"strconv"
	"strings"
)

const (
	// MetadataCredentialDraft is the canonical generic marker for a disabled credential draft.
	MetadataCredentialDraft = "credential_draft"
	metadataLegacyProDraft  = "pro_draft"
)

// MarkCredentialDraft marks an auth as a disabled draft before its first persistence.
// The legacy marker is written for compatibility with existing management clients.
func MarkCredentialDraft(auth *Auth) {
	if auth == nil {
		return
	}
	auth.Disabled = true
	auth.Status = StatusDisabled
	if auth.Metadata == nil {
		auth.Metadata = make(map[string]any)
	}
	auth.Metadata[MetadataCredentialDraft] = true
	auth.Metadata[metadataLegacyProDraft] = true
	auth.Metadata["disabled"] = true
}

// ClearCredentialDraft removes draft markers after a credential is explicitly enabled.
func ClearCredentialDraft(auth *Auth) {
	if auth == nil || auth.Metadata == nil {
		return
	}
	delete(auth.Metadata, MetadataCredentialDraft)
	delete(auth.Metadata, metadataLegacyProDraft)
	delete(auth.Metadata, "draft")
}

// IsCredentialDraft reports whether an auth explicitly carries a draft marker.
func IsCredentialDraft(auth *Auth) bool {
	if auth == nil || len(auth.Metadata) == 0 {
		return false
	}
	for _, key := range []string{MetadataCredentialDraft, metadataLegacyProDraft, "draft"} {
		if metadataBool(auth.Metadata[key]) {
			return true
		}
	}
	return false
}

// AllowsDisabledInitialPersist reports whether a missing disabled auth may be created.
// Existing disabled auths remain persistable independently of this decision.
func AllowsDisabledInitialPersist(auth *Auth) bool {
	return auth != nil && auth.Disabled && IsCredentialDraft(auth)
}

func metadataBool(value any) bool {
	switch typed := value.(type) {
	case bool:
		return typed
	case string:
		parsed, errParse := strconv.ParseBool(strings.TrimSpace(typed))
		return errParse == nil && parsed
	case float64:
		return typed != 0
	case int:
		return typed != 0
	case int64:
		return typed != 0
	default:
		return false
	}
}
