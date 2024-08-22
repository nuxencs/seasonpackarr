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
	"slices"
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

func (s *discordSender) Send(statusCode int, payload domain.NotificationPayload) error {
	if !s.isEnabled() {
		s.log.Warn().Msg("no webhook defined, skipping notification")
		return nil
	}

	if !s.shouldSend(statusCode) {
		s.log.Warn().Msg("no notification wanted for this status code, skipping notification")
		return nil
	}

	m := DiscordMessage{
		Content: nil,
		Embeds:  []DiscordEmbeds{s.buildEmbed(statusCode, payload)},
	}

	jsonData, err := json.Marshal(m)
	if err != nil {
		return errors.Wrap(err, "discord client could not marshal data: %+v", m)
	}

	req, err := http.NewRequest(http.MethodPost, s.cfg.Config.Notifications.Discord, bytes.NewBuffer(jsonData))
	if err != nil {
		return errors.Wrap(err, "discord client could not create request")
	}

	req.Header.Set("Content-Type", "application/json")
	//req.Header.Set("User-Agent", "seasonpackarr")

	res, err := s.httpClient.Do(req)
	if err != nil {
		return errors.Wrap(err, "discord client could not make request: %+v", req)
	}

	defer res.Body.Close()

	body, err := io.ReadAll(res.Body)
	if err != nil {
		return errors.Wrap(err, "discord client could not read data")
	}

	s.log.Trace().Msgf("discord status: %v response: %v", res.StatusCode, string(body))

	// discord responds with 204, Notifiarr with 204 so lets take all 200 as ok
	if res.StatusCode >= 300 {
		return errors.New("bad discord client status: %v body: %v", res.StatusCode, string(body))
	}

	s.log.Debug().Msg("notification successfully sent to discord")

	return nil
}

func (s *discordSender) isEnabled() bool {
	return s.cfg.Config.Notifications.Discord != ""
}

func (s *discordSender) shouldSend(statusCode int) bool {
	var statusCodes []int

	if len(s.cfg.Config.Notifications.NotificationLevel) == 0 {
		return false
	}

	for _, level := range s.cfg.Config.Notifications.NotificationLevel {
		if level == domain.NotificationLevelMatch {
			statusCodes = append(statusCodes, domain.GetMatchStatusCodes()...)
		}
		if level == domain.NotificationLevelInfo {
			statusCodes = append(statusCodes, domain.GetInfoStatusCodes()...)
		}
		if level == domain.NotificationLevelError {
			statusCodes = append(statusCodes, domain.GetErrorStatusCodes()...)
		}
	}
	fmt.Println(s.cfg.Config.Notifications.NotificationLevel, statusCodes)

	return slices.Contains(statusCodes, statusCode)
}

func (s *discordSender) buildEmbed(statusCode int, payload domain.NotificationPayload) DiscordEmbeds {
	color := LIGHT_BLUE

	if slices.Contains(domain.GetInfoStatusCodes(), statusCode) { // not matching
		color = GRAY
	} else if slices.Contains(domain.GetErrorStatusCodes(), statusCode) { // error processing
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
