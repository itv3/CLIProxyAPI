package config

import "testing"

func TestParseConfigBytesProtocolModelListEnabled(t *testing.T) {
	t.Run("defaults to false", func(t *testing.T) {
		cfg, err := ParseConfigBytes([]byte("{}\n"))
		if err != nil {
			t.Fatalf("ParseConfigBytes() error = %v", err)
		}
		if cfg.ProtocolModelListEnabled {
			t.Fatal("ProtocolModelListEnabled = true, want false")
		}
	})

	t.Run("loads true", func(t *testing.T) {
		cfg, err := ParseConfigBytes([]byte("protocol-model-list-enabled: true\n"))
		if err != nil {
			t.Fatalf("ParseConfigBytes() error = %v", err)
		}
		if !cfg.ProtocolModelListEnabled {
			t.Fatal("ProtocolModelListEnabled = false, want true")
		}
	})
}
