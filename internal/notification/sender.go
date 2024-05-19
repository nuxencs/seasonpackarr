// Copyright (c) 2021 - 2024, Ludvig Lundgren and the autobrr contributors.
// Code is heavily modified for use with seasonpackarr
// SPDX-License-Identifier: GPL-2.0-or-later

package notification

import (
	"fmt"
	"seasonpackarr/internal/config"
	"seasonpackarr/internal/domain"
	"seasonpackarr/internal/logger"

	"github.com/containrrr/shoutrrr"
	"github.com/rs/zerolog"
)

type Sender struct {
	log     zerolog.Logger
	cfg     *config.AppConfig
	builder MessageBuilderPlainText
}

func NewSender(log logger.Logger, config *config.AppConfig) Sender {
	return Sender{
		log:     log.With().Str("module", "notification").Logger(),
		cfg:     config,
		builder: MessageBuilderPlainText{},
	}
}

func (s *Sender) Send(event domain.NotificationEvent, payload domain.NotificationPayload) error {
	if !s.isEnabled() {
		return nil
	}

	message := s.builder.BuildBody(payload)
	url := s.cfg.Config.NotificationHost + fmt.Sprintf("?Title=%s", BuildTitle(event))

	if err := shoutrrr.Send(url, message); err != nil {
		return err
	}

	s.log.Debug().Msg("notification successfully sent")

	return nil
}

func (s *Sender) isEnabled() bool {
	if s.cfg.Config.NotificationHost == "" {
		s.log.Warn().Msg("no notification host defined, skipping notification")
		return false
	}

	return true
}
