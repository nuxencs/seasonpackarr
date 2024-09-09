// Copyright (c) 2021 - 2024, Ludvig Lundgren and the autobrr contributors.
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
	"strings"
	"time"

	"github.com/nuxencs/seasonpackarr/internal/config"
	"github.com/nuxencs/seasonpackarr/internal/domain"
	"github.com/nuxencs/seasonpackarr/internal/logger"
	"github.com/nuxencs/seasonpackarr/pkg/errors"

	"github.com/rs/zerolog"
)

type DiscordMessage struct {
	Content interface{}     `json:"content"`
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
	LIGHT_BLUE EmbedColors = 5814783  // 58b9ff
	RED        EmbedColors = 15548997 // ed4245
	GREEN      EmbedColors = 5763719  // 57f287
	GRAY       EmbedColors = 10070709 // 99aab5
)

type discordSender struct {
	log zerolog.Logger
	cfg *config.AppConfig

	httpClient *http.Client
}

func NewDiscordSender(log logger.Logger, config *config.AppConfig) domain.Sender {
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

func (s *discordSender) Send(statusCode int, payload domain.NotificationPayload) error {
	if !s.isEnabled() {
		s.log.Debug().Msg("no webhook defined, skipping notification")
		return nil
	}

	if !s.shouldSend(statusCode) {
		s.log.Debug().Msg("no notification wanted for this status, skipping notification")
		return nil
	}

	m := DiscordMessage{
		Content: nil,
		Embeds:  []DiscordEmbeds{s.buildEmbed(statusCode, payload)},
	}

	jsonData, err := json.Marshal(m)
	if err != nil {
		return errors.Wrap(err, "could not marshal json request for status: %v payload: %v", statusCode, payload)
	}

	req, err := http.NewRequest(http.MethodPost, s.cfg.Config.Notifications.Discord, bytes.NewBuffer(jsonData))
	if err != nil {
		return errors.Wrap(err, "could not create request for status: %v payload: %v", statusCode, payload)
	}

	req.Header.Set("Content-Type", "application/json")
	// req.Header.Set("User-Agent", "seasonpackarr")

	res, err := s.httpClient.Do(req)
	if err != nil {
		return errors.Wrap(err, "client request error for status: %v payload: %v", statusCode, payload)
	}

	defer res.Body.Close()

	s.log.Trace().Msgf("discord response status: %d", res.StatusCode)

	// discord responds with 204, Notifiarr with 204 so lets take all 200 as ok
	if res.StatusCode != http.StatusOK && res.StatusCode != http.StatusNoContent {
		body, err := io.ReadAll(bufio.NewReader(res.Body))
		if err != nil {
			return errors.Wrap(err, "could not read body for status: %v payload: %v", statusCode, payload)
		}

		return errors.New("unexpected status: %v body: %v", res.StatusCode, string(body))
	}

	s.log.Debug().Msg("notification successfully sent to discord")

	return nil
}

func (s *discordSender) isEnabled() bool {
	return len(s.cfg.Config.Notifications.Discord) != 0
}

func (s *discordSender) shouldSend(statusCode int) bool {
	if len(s.cfg.Config.Notifications.NotificationLevel) == 0 {
		return false
	}

	statusCodes := make(map[int]struct{})

	for _, level := range s.cfg.Config.Notifications.NotificationLevel {
		if codes, ok := domain.StatusMap[level]; ok {
			for _, code := range codes {
				statusCodes[code] = struct{}{}
			}
		}
	}

	_, shouldSend := statusCodes[statusCode]
	return shouldSend
}

func (s *discordSender) buildEmbed(statusCode int, payload domain.NotificationPayload) DiscordEmbeds {
	color := LIGHT_BLUE

	if slices.Contains(domain.StatusMap[domain.NotificationLevelInfo], statusCode) { // not matching
		color = GRAY
	} else if slices.Contains(domain.StatusMap[domain.NotificationLevelError], statusCode) { // error processing
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
		if slices.Contains(domain.StatusMap[domain.NotificationLevelError], statusCode) {
			f := DiscordEmbedsFields{
				Name:   "Error",
				Value:  fmt.Sprintf("```%s```", payload.Error.Error()),
				Inline: false,
			}
			fields = append(fields, f)
		} else {
			payload.Message = payload.Error.Error()
		}
	}

	embed := DiscordEmbeds{
		Title:     BuildTitle(statusCode),
		Color:     int(color),
		Fields:    fields,
		Timestamp: time.Now(),
	}

	if payload.Message != "" {
		embed.Description = strings.ToUpper(string(payload.Message[0])) + payload.Message[1:]
	}

	return embed
}
