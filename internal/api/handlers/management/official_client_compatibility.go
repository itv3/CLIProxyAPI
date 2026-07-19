package management

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/officialclient"
)

func decodeOfficialClientCompatibilityPatch(raw json.RawMessage) (*officialclient.CompatibilityConfig, bool, error) {
	if len(raw) == 0 {
		return nil, false, nil
	}
	if strings.EqualFold(strings.TrimSpace(string(raw)), "null") {
		return nil, true, fmt.Errorf("%w: null is not allowed", officialclient.ErrInvalidAttributes)
	}
	compatibility, err := officialclient.DecodeCompatibility(string(raw))
	if err != nil {
		return nil, true, err
	}
	return compatibility, true, nil
}

func normalizeManagementOfficialClientCompatibility(provider string, compatibility *officialclient.CompatibilityConfig) error {
	return officialclient.NormalizeCompatibility(provider, compatibility, true)
}

func hasOfficialClientCompatibilityField(data []byte) bool {
	var payload any
	if err := json.Unmarshal(data, &payload); err != nil {
		return false
	}
	entries := make([]any, 0)
	switch value := payload.(type) {
	case []any:
		entries = value
	case map[string]any:
		if items, ok := value["items"].([]any); ok {
			entries = items
		}
	}
	for _, entry := range entries {
		object, ok := entry.(map[string]any)
		if !ok {
			continue
		}
		for name := range object {
			if strings.EqualFold(name, "official-client-compatibility") {
				return true
			}
		}
	}
	return false
}
