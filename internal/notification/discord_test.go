// Copyright (c) 2023 - 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package notification

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/nuxencs/seasonpackarr/internal/domain"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"
)

type staticNotificationConfig struct {
	config domain.Config
}

func (c staticNotificationConfig) Snapshot() domain.Config {
	return c.config
}

func TestDiscordSenderUsesOutcomeKindInsteadOfStatusCode(t *testing.T) {
	tests := []struct {
		name          string
		outcome       domain.Outcome
		levels        []string
		wantSend      bool
		wantColor     EmbedColors
		wantErrorText string
	}{
		{
			name:      "445 rejection uses info",
			outcome:   domain.Rejected(domain.StatusFailedMatchToTorrentEps),
			levels:    []string{LevelInfo},
			wantSend:  true,
			wantColor: GRAY,
		},
		{
			name:    "445 failure does not use info",
			outcome: domain.FailedBecause(domain.StatusFailedMatchToTorrentEps, errors.New("client file lookup failed")),
			levels:  []string{LevelInfo},
		},
		{
			name:          "445 failure uses error",
			outcome:       domain.FailedBecause(domain.StatusFailedMatchToTorrentEps, errors.New("client file lookup failed")),
			levels:        []string{LevelError},
			wantSend:      true,
			wantColor:     RED,
			wantErrorText: "client file lookup failed",
		},
		{
			name:      "success uses match",
			outcome:   domain.Successful(domain.StatusSuccessfulMatch),
			levels:    []string{LevelMatch},
			wantSend:  true,
			wantColor: GREEN,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var mu sync.Mutex
			var messages []DiscordMessage
			var decodeErr error
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var message DiscordMessage
				mu.Lock()
				decodeErr = json.NewDecoder(r.Body).Decode(&message)
				messages = append(messages, message)
				mu.Unlock()
				w.WriteHeader(http.StatusNoContent)
			}))
			t.Cleanup(server.Close)

			sender := &discordSender{
				log: zerolog.Nop(),
				cfg: staticNotificationConfig{config: domain.Config{Notifications: domain.Notifications{
					NotificationLevel: tt.levels,
					Discord:           server.URL,
				}}},
				httpClient: server.Client(),
			}

			err := sender.Send(tt.outcome, Payload{ReleaseName: "Show.S01", Client: "default", Action: "Pack"})
			require.NoError(t, err)

			mu.Lock()
			defer mu.Unlock()
			require.NoError(t, decodeErr)
			if !tt.wantSend {
				require.Empty(t, messages)
				return
			}

			require.Len(t, messages, 1)
			require.Len(t, messages[0].Embeds, 1)
			embed := messages[0].Embeds[0]
			require.Equal(t, int(tt.wantColor), embed.Color)
			if tt.wantErrorText == "" {
				require.False(t, hasDiscordField(embed.Fields, "Error"))
			} else {
				require.Contains(t, discordFieldValue(embed.Fields, "Error"), tt.wantErrorText)
			}
		})
	}
}

func TestDiscordSenderRejectsInvalidOutcome(t *testing.T) {
	sender := &discordSender{}

	require.Error(t, sender.Send(domain.Outcome{}, Payload{}))
}

func hasDiscordField(fields []DiscordEmbedsFields, name string) bool {
	return discordFieldValue(fields, name) != ""
}

func discordFieldValue(fields []DiscordEmbedsFields, name string) string {
	for _, field := range fields {
		if field.Name == name {
			return field.Value
		}
	}
	return ""
}
