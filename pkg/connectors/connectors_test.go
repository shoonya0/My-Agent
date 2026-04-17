package connectors

import (
	"context"
	"testing"

	"myAgent/pkg/types"
)

// Compile-time assertions: each connector must satisfy types.PlatformConnector.
var (
	_ types.PlatformConnector = (*Discord)(nil)
	_ types.PlatformConnector = (*Instagram)(nil)
	_ types.PlatformConnector = (*Telegram)(nil)
	_ types.PlatformConnector = (*WhatsApp)(nil)
)

func TestConnectors_PlatformConnectorSmoke(t *testing.T) {
	ctx := context.Background()
	tests := []struct {
		name     string
		conn     types.PlatformConnector
		wantName string
	}{
		{"discord", NewDiscord(), "discord"},
		{"instagram", NewInstagram(), "instagram"},
		{"telegram", NewTelegram(), "telegram"},
		{"whatsapp", NewWhatsApp(), "whatsapp"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.conn.Name(); got != tt.wantName {
				t.Errorf("Name() = %q, want %q", got, tt.wantName)
			}
			if err := tt.conn.Validate(ctx); err != nil {
				t.Errorf("Validate() = %v, want nil", err)
			}
		})
	}
}
