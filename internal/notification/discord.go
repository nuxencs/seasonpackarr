// Copyright (c) 2021 - 2026, Ludvig Lundgren and the autobrr contributors.
// Code is heavily modified for use with seasonpackarr
// SPDX-License-Identifier: GPL-2.0-or-later

package notification

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"slices"
	"time"

	"github.com/nuxencs/seasonpackarr/internal/config"
	"github.com/nuxencs/seasonpackarr/internal/domain"
	"github.com/nuxencs/seasonpackarr/internal/errtrace"
	"github.com/nuxencs/seasonpackarr/internal/logger"
	"github.com/rs/zerolog"
)

type DiscordMessage struct {
	Content any             `json:"content"`
	Embeds  []DiscordEmbeds `json:"embeds,omitempty"`
}

type DiscordEmbeds struct {
	Title       string                `json:"title"`
	Description string                `json:"description"`
	Color       int                   `json:"color"`
	Fields      []DiscordEmbedsFields `json:"fields,omitempty"`
	Timestamp   time.Time             `json:"timestamp"`
}

type DiscordEmbedsFields struct {
	Name   string `json:"name"`
	Value  string `json:"value"`
	Inline bool   `json:"inline,omitzero"`
}

type EmbedColors int

const (
	RED   EmbedColors = 0xed4245
	GREEN EmbedColors = 0x57f287
	GRAY  EmbedColors = 0x99aab5
)

type discordSender struct {
	log zerolog.Logger
	cfg config.Provider

	httpClient *http.Client
}

func NewDiscordSender(log logger.Logger, config config.Provider) domain.Sender {
	return &discordSender{
		log: log.With().Str("sender", "discord").Logger(),
		cfg: config,
		httpClient: &http.Client{
			Timeout: time.Second * 30,
		},
	}
}

func (s *discordSender) Name() string {
	return "discord"
}

func (s *discordSender) Send(ctx context.Context, statusCode domain.StatusCode, payload domain.NotificationPayload) error {
	notifications := s.cfg.Snapshot().Notifications
	if !s.isEnabled(notifications) {
		s.log.Debug().Msg("no webhook defined, skipping notification")
		return nil
	}

	if !s.shouldSend(statusCode, notifications) {
		s.log.Debug().Msg("no notification wanted for this status, skipping notification")
		return nil
	}

	m := DiscordMessage{
		Content: nil,
		Embeds:  []DiscordEmbeds{s.buildEmbed(statusCode, payload)},
	}

	jsonData, err := json.Marshal(m)
	if err != nil {
		return errtrace.WithStack(fmt.Errorf("could not marshal JSON request for status %v and payload %v: %w", statusCode, payload, err))
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, notifications.Discord, bytes.NewReader(jsonData))
	if err != nil {
		return errtrace.WithStack(fmt.Errorf("could not create request for status %v and payload %v: %w", statusCode, payload, err))
	}

	req.Header.Set("Content-Type", "application/json")
	// req.Header.Set("User-Agent", "seasonpackarr")

	res, err := s.httpClient.Do(req)
	if err != nil {
		return errtrace.WithStack(fmt.Errorf("client request error for status %v and payload %v: %w", statusCode, payload, err))
	}

	defer func() { _ = res.Body.Close() }()

	s.log.Trace().Msgf("discord response status: %d", res.StatusCode)

	// discord responds with 204, Notifiarr with 204 so lets take all 200 as ok
	if res.StatusCode != http.StatusOK && res.StatusCode != http.StatusNoContent {
		body, err := io.ReadAll(bufio.NewReader(res.Body))
		if err != nil {
			return errtrace.WithStack(fmt.Errorf("could not read response body for status %v and payload %v: %w", statusCode, payload, err))
		}

		return errtrace.WithStack(fmt.Errorf("unexpected status %v with body %v", res.StatusCode, string(body)))
	}

	s.log.Debug().Msg("notification successfully sent to discord")

	return nil
}

func (s *discordSender) isEnabled(notifications domain.Notifications) bool {
	return len(notifications.Discord) != 0
}

func (s *discordSender) shouldSend(statusCode domain.StatusCode, notifications domain.Notifications) bool {
	if len(notifications.NotificationLevel) == 0 {
		return false
	}

	statusCodes := make(map[domain.StatusCode]struct{})

	for _, level := range notifications.NotificationLevel {
		if codes, ok := domain.NotificationStatusMap[level]; ok {
			for _, code := range codes {
				statusCodes[code] = struct{}{}
			}
		}
	}

	_, shouldSend := statusCodes[statusCode]
	return shouldSend
}

func (s *discordSender) buildEmbed(statusCode domain.StatusCode, payload domain.NotificationPayload) DiscordEmbeds {
	var color EmbedColors

	if slices.Contains(domain.NotificationStatusMap[domain.NotificationLevelInfo], statusCode) { // not matching
		color = GRAY
	} else if slices.Contains(domain.NotificationStatusMap[domain.NotificationLevelError], statusCode) { // error processing
		color = RED
	} else { // success
		color = GREEN
	}

	var fields []DiscordEmbedsFields

	if payload.ReleaseName != "" {
		f := DiscordEmbedsFields{
			Name:   "Release Name",
			Value:  payload.ReleaseName,
			Inline: true,
		}
		fields = append(fields, f)
	}

	if payload.Client != "" {
		f := DiscordEmbedsFields{
			Name:   "Client",
			Value:  payload.Client,
			Inline: true,
		}
		fields = append(fields, f)
	}

	if payload.Action != "" {
		f := DiscordEmbedsFields{
			Name:   "Action",
			Value:  payload.Action,
			Inline: true,
		}
		fields = append(fields, f)
	}

	if payload.Error != nil {
		// actual error?
		if slices.Contains(domain.NotificationStatusMap[domain.NotificationLevelError], statusCode) {
			f := DiscordEmbedsFields{
				Name:   "Error",
				Value:  fmt.Sprintf("```%s```", payload.Error.Error()),
				Inline: false,
			}
			fields = append(fields, f)
		}
	}

	embed := DiscordEmbeds{
		Title:     BuildTitle(statusCode),
		Color:     int(color),
		Fields:    fields,
		Timestamp: time.Now(),
	}

	return embed
}
