// Copyright (c) 2023 - 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package http

import (
	"context"
	"encoding/json"

	"github.com/nuxencs/seasonpackarr/internal/config"
	"github.com/nuxencs/seasonpackarr/internal/domain"
	"github.com/nuxencs/seasonpackarr/internal/inventory"
	"github.com/nuxencs/seasonpackarr/internal/logger"

	"github.com/rs/zerolog"
)

type processor struct {
	log   zerolog.Logger
	cfg   config.Provider
	inv   *inventory.Store
	noti  domain.Sender
	tasks *taskGroup
	req   *request
	// client is the connection of this request, so a config reload during the
	// request cannot switch the adapter between the read and the import.
	client *inventory.Client
}

type request struct {
	Name       string
	Torrent    json.RawMessage
	ClientName string
}

func newProcessor(log logger.Logger, config config.Provider, inv *inventory.Store, notification domain.Sender, tasks *taskGroup) *processor {
	return &processor{
		log:   log.With().Str("module", "processor").Logger(),
		cfg:   config,
		inv:   inv,
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

// connect returns the client handle of this request. The first call connects
// with cfg, and later calls return the same handle.
func (p *processor) connect(ctx context.Context, clientName string, cfg domain.Config) (*inventory.Client, error) {
	if p.client == nil {
		client, err := p.inv.Client(ctx, clientName, cfg)
		if err != nil {
			return nil, err
		}
		p.client = client
	}
	return p.client, nil
}
