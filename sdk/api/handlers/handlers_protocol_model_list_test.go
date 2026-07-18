package handlers

import (
	"testing"

	sdkconfig "github.com/router-for-me/CLIProxyAPI/v7/sdk/config"
)

func TestProtocolModelListEnabled(t *testing.T) {
	if NewBaseAPIHandlers(nil, nil).ProtocolModelListEnabled() {
		t.Fatal("nil config enabled protocol model list")
	}

	handler := NewBaseAPIHandlers(&sdkconfig.SDKConfig{ProtocolModelListEnabled: true}, nil)
	if !handler.ProtocolModelListEnabled() {
		t.Fatal("enabled config was not applied")
	}

	handler.UpdateClients(&sdkconfig.SDKConfig{})
	if handler.ProtocolModelListEnabled() {
		t.Fatal("disabled hot-reload config was not applied")
	}

	handler.UpdateClients(&sdkconfig.SDKConfig{ProtocolModelListEnabled: true})
	if !handler.ProtocolModelListEnabled() {
		t.Fatal("enabled hot-reload config was not applied")
	}

	handler.UpdateClients(nil)
	if handler.ProtocolModelListEnabled() {
		t.Fatal("nil hot-reload config enabled protocol model list")
	}
}
