// Copyright (c) 2023 - 2024, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package http

import (
	"net/http"

	"seasonpackarr/internal/config"
	"seasonpackarr/internal/logger"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/render"
)

type webhookHandler struct {
	log logger.Logger
	cfg *config.AppConfig
}

func newWebhookHandler(log logger.Logger, cfg *config.AppConfig) *webhookHandler {
	return &webhookHandler{
		log: log,
		cfg: cfg,
	}
}

func (h webhookHandler) Routes(r chi.Router) {
	r.Post("/pack", h.pack)
	r.Post("/parse", h.parse)
}

func (h webhookHandler) pack(w http.ResponseWriter, r *http.Request) {
	newProcessor(h.log, h.cfg).ProcessSeasonPack(w, r)
	render.Status(r, http.StatusOK)
}

func (h webhookHandler) parse(w http.ResponseWriter, r *http.Request) {
	newProcessor(h.log, h.cfg).ParseTorrent(w, r)
	render.Status(r, http.StatusOK)
}
