// Copyright (c) 2023 - 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package http

import (
	"encoding/json"

	"github.com/nuxencs/seasonpackarr/internal/config"
	"github.com/nuxencs/seasonpackarr/internal/domain"
	"github.com/nuxencs/seasonpackarr/internal/logger"
	"github.com/nuxencs/seasonpackarr/internal/torrentclient"

	"github.com/rs/zerolog"
)

type processor struct {
	log   zerolog.Logger
	cfg   config.Provider
	noti  domain.Sender
	tasks *taskGroup
	req   *request
}

type request struct {
	Name       string
	Torrent    json.RawMessage
	Client     torrentclient.TorrentClient
	ClientName string
}

func newProcessor(log logger.Logger, config config.Provider, notification domain.Sender, tasks *taskGroup) *processor {
	return &processor{
		log:   log.With().Str("module", "processor").Logger(),
		cfg:   config,
		noti:  notification,
		tasks: tasks,
	}
}

func (p *processor) getClientName() string {
	if len(p.req.ClientName) == 0 {
		p.req.ClientName = "default"
		p.log.Info().Msg("no clientname defined. trying to use default client")

		return "default"
	}

	return p.req.ClientName
}
