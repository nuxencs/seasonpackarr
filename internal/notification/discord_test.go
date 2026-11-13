// Copyright (c) 2023 - 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package notification

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/nuxencs/seasonpackarr/internal/domain"
	"github.com/nuxencs/seasonpackarr/internal/logger"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeConfig struct{ config domain.Config }

func (f fakeConfig) Snapshot() domain.Config { return f.config }

// TestDiscordSend_ErrorOmitsWebhookURL: the webhook URL holds the token, and the
// caller logs the error.
func TestDiscordSend_ErrorOmitsWebhookURL(t *testing.T) {
	server := httptest.NewServer(http.NotFoundHandler())
	unreachable := server.URL
	server.Close()

	tests := map[string]string{
		"unreachable server": unreachable + "/api/webhooks/123/SECRET-TOKEN",
		"invalid URL":        "http://discord.invalid/api/webhooks/123/SECRET-TOKEN\n",
	}
	for name, webhook := range tests {
		t.Run(name, func(t *testing.T) {
			sender := NewDiscordSender(
				logger.New(&domain.Config{LogLevel: "ERROR", Version: "test"}),
				fakeConfig{config: domain.Config{Notifications: domain.Notifications{
					Discord:           webhook,
					NotificationLevel: []string{domain.NotificationLevelError},
				}}},
			)

			err := sender.Send(t.Context(), domain.StatusFailedHardlink, domain.NotificationPayload{ReleaseName: "Show.S01"})

			require.Error(t, err)
			assert.NotContains(t, err.Error(), "SECRET-TOKEN")
			assert.NotContains(t, err.Error(), "/api/webhooks")
		})
	}
}
