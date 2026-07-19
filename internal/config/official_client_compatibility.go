package config

import (
	"fmt"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/officialclient"
)

func (cfg *Config) NormalizeOfficialClientCompatibility(assignCurrent bool) error {
	if cfg == nil {
		return nil
	}
	for i := range cfg.ClaudeKey {
		compatibility := cfg.ClaudeKey[i].OfficialClientCompatibility
		if err := officialclient.NormalizeCompatibility("claude", compatibility, assignCurrent); err != nil {
			return fmt.Errorf("claude-api-key[%d] official-client-compatibility: %w", i, err)
		}
	}
	for i := range cfg.CodexKey {
		compatibility := cfg.CodexKey[i].OfficialClientCompatibility
		if err := officialclient.NormalizeCompatibility("codex", compatibility, assignCurrent); err != nil {
			return fmt.Errorf("codex-api-key[%d] official-client-compatibility: %w", i, err)
		}
	}
	for i := range cfg.XAIKey {
		if cfg.XAIKey[i].OfficialClientCompatibility != nil {
			return fmt.Errorf("xai-api-key[%d] official-client-compatibility: %w", i, officialclient.ErrUnsupportedProvider)
		}
	}
	return nil
}
