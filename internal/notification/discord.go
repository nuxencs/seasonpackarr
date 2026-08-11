// Copyright (c) 2021 - 2026, Ludvig Lundgren and the autobrr contributors.
// Code is heavily modified for use with seasonpackarr
// SPDX-License-Identifier: GPL-2.0-or-later

package notification

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"slices"
	"time"

	"github.com/nuxencs/seasonpackarr/internal/config"
	"github.com/nuxencs/seasonpackarr/internal/domain"
	"github.com/nuxencs/seasonpackarr/internal/logger"
	"github.com/nuxencs/seasonpackarr/pkg/errors"

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
	Inline bool   `json:"inline,omitempty"`
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

func NewDiscordSender(log logger.Logger, config config.Provider) Sender {
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

func (s *discordSender) Send(outcome domain.Outcome, payload Payload) error {
	if err := outcome.Validate(); err != nil {
		return fmt.Errorf("invalid processing outcome: %w", err)
	}

	notifications := s.cfg.Snapshot().Notifications
	if !s.isEnabled(notifications) {
		s.log.Debug().Msg("no webhook defined, skipping notification")
		return nil
	}

	if !s.shouldSend(outcome, notifications) {
		s.log.Debug().Msg("no notification wanted for this status, skipping notification")
		return nil
	}

	m := DiscordMessage{
		Content: nil,
		Embeds:  []DiscordEmbeds{s.buildEmbed(outcome, payload)},
	}

	jsonData, err := json.Marshal(m)
	if err != nil {
		return errors.Wrap(err, "could not marshal json request for status: %v payload: %v", outcome.StatusCode(), payload)
	}

	req, err := http.NewRequest(http.MethodPost, notifications.Discord, bytes.NewBuffer(jsonData))
	if err != nil {
		return errors.Wrap(err, "could not create request for status: %v payload: %v", outcome.StatusCode(), payload)
	}

	req.Header.Set("Content-Type", "application/json")
	// req.Header.Set("User-Agent", "seasonpackarr")

	res, err := s.httpClient.Do(req)
	if err != nil {
		return errors.Wrap(err, "client request error for status: %v payload: %v", outcome.StatusCode(), payload)
	}

	defer res.Body.Close()

	s.log.Trace().Msgf("discord response status: %d", res.StatusCode)

	// discord responds with 204, Notifiarr with 204 so lets take all 200 as ok
	if res.StatusCode != http.StatusOK && res.StatusCode != http.StatusNoContent {
		body, err := io.ReadAll(bufio.NewReader(res.Body))
		if err != nil {
			return errors.Wrap(err, "could not read body for status: %v payload: %v", outcome.StatusCode(), payload)
		}

		return errors.New("unexpected status: %v body: %v", res.StatusCode, string(body))
	}

	s.log.Debug().Msg("notification successfully sent to discord")

	return nil
}

func (s *discordSender) isEnabled(notifications domain.Notifications) bool {
	return len(notifications.Discord) != 0
}

func (s *discordSender) shouldSend(outcome domain.Outcome, notifications domain.Notifications) bool {
	if len(notifications.NotificationLevel) == 0 {
		return false
	}

	presentation := presentDiscordOutcome(outcome.Kind())
	return presentation.level != "" && slices.Contains(notifications.NotificationLevel, presentation.level)
}

func (s *discordSender) buildEmbed(outcome domain.Outcome, payload Payload) DiscordEmbeds {
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

	if outcome.Kind() == domain.OutcomeFailure && outcome.Cause() != nil {
		f := DiscordEmbedsFields{
			Name:   "Error",
			Value:  fmt.Sprintf("```%s```", outcome.Cause().Error()),
			Inline: false,
		}
		fields = append(fields, f)
	}

	embed := DiscordEmbeds{
		Title:     BuildTitle(outcome.StatusCode()),
		Color:     int(presentDiscordOutcome(outcome.Kind()).color),
		Fields:    fields,
		Timestamp: time.Now(),
	}

	return embed
}

type discordOutcomePresentation struct {
	level string
	color EmbedColors
}

func presentDiscordOutcome(kind domain.OutcomeKind) discordOutcomePresentation {
	switch kind {
	case domain.OutcomeSuccess:
		return discordOutcomePresentation{level: LevelMatch, color: GREEN}
	case domain.OutcomeRejection:
		return discordOutcomePresentation{level: LevelInfo, color: GRAY}
	case domain.OutcomeFailure:
		return discordOutcomePresentation{level: LevelError, color: RED}
	default:
		return discordOutcomePresentation{color: RED}
	}
}
