package main

import (
	"reflect"
	"testing"

	"github.com/overmindv/task-hunter/config"
)

// TestTelegramRuntime проверяет отключение scheduler и session readiness.
func TestTelegramRuntime(t *testing.T) {
	disabledChannels, disabledSession := telegramRuntime(&config.Config{})
	if disabledChannels != nil || disabledSession != "" {
		t.Fatalf("unexpected disabled runtime: channels=%v session=%q", disabledChannels, disabledSession)
	}

	enabled := &config.Config{
		Telegram: config.TelegramConfig{
			Enabled:     true,
			Channels:    []string{"algoses"},
			SessionPath: "/session/telegram.session",
		},
	}
	channels, session := telegramRuntime(enabled)
	if !reflect.DeepEqual(channels, enabled.Telegram.Channels) || session != enabled.Telegram.SessionPath {
		t.Fatalf("unexpected enabled runtime: channels=%v session=%q", channels, session)
	}
}
