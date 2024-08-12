// Copyright (c) 2021 - 2024, Ludvig Lundgren and the autobrr contributors.
// Code is heavily modified for use with seasonpackarr
// SPDX-License-Identifier: GPL-2.0-or-later

package notification

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"seasonpackarr/internal/config"
	"seasonpackarr/internal/domain"
	"seasonpackarr/internal/logger"
	"seasonpackarr/pkg/errors"

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

type DiscordSender struct {
	log zerolog.Logger
	cfg *config.AppConfig

	httpClient *http.Client
}

func NewDiscordSender(log logger.Logger, config *config.AppConfig) *DiscordSender {
	return &DiscordSender{
		log: log.With().Str("sender", "discord").Logger(),
		cfg: config,
		httpClient: &http.Client{
			Timeout: time.Second * 30,
		},
	}
}

func (s *DiscordSender) Send(statusCode int, payload domain.NotificationPayload) error {
	if !s.isEnabled() {
		return nil
	}

	m := DiscordMessage{
		Content: nil,
		Embeds:  []DiscordEmbeds{s.buildEmbed(statusCode, payload)},
	}

	jsonData, err := json.Marshal(m)
	if err != nil {
		s.log.Error().Err(err).Msgf("discord client could not marshal data: %v", m)
		return errors.Wrap(err, "could not marshal data: %+v", m)
	}

	req, err := http.NewRequest(http.MethodPost, s.cfg.Config.Notifications.Discord, bytes.NewBuffer(jsonData))
	if err != nil {
		s.log.Error().Err(err).Msgf("discord client request error: %v", statusCode)
		return errors.Wrap(err, "could not create request")
	}

	req.Header.Set("Content-Type", "application/json")
	//req.Header.Set("User-Agent", "seasonpackarr")

	res, err := s.httpClient.Do(req)
	if err != nil {
		s.log.Error().Err(err).Msgf("discord client request error: %v", statusCode)
		return errors.Wrap(err, "could not make request: %+v", req)
	}

	defer res.Body.Close()

	body, err := io.ReadAll(res.Body)
	if err != nil {
		s.log.Error().Err(err).Msgf("discord client request error: %v", statusCode)
		return errors.Wrap(err, "could not read data")
	}

	s.log.Trace().Msgf("discord status: %v response: %v", res.StatusCode, string(body))

	// discord responds with 204, Notifiarr with 204 so lets take all 200 as ok
	if res.StatusCode >= 300 {
		s.log.Error().Err(err).Msgf("discord client request error: %v", string(body))
		return errors.New("bad status: %v body: %v", res.StatusCode, string(body))
	}

	s.log.Debug().Msg("notification successfully sent to discord")

	return nil
}

func (s *DiscordSender) isEnabled() bool {
	if s.cfg.Config.Notifications.Discord == "" {
		s.log.Warn().Msg("no webhook defined, skipping notification")
		return false
	}

	return true
}

func (s *DiscordSender) buildEmbed(statusCode int, payload domain.NotificationPayload) DiscordEmbeds {
	color := LIGHT_BLUE

	if (statusCode >= 200) && (statusCode < 250) { // not matching
		color = GRAY
	} else if (statusCode >= 400) && (statusCode < 500) { // error processing
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
		if statusCode >= 400 {
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
