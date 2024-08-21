// Copyright (c) 2023 - 2024, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package http

import (
	"net/http"

	"seasonpackarr/internal/config"
	"seasonpackarr/internal/domain"
	"seasonpackarr/internal/logger"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/render"
)

type webhookHandler struct {
	log  logger.Logger
	cfg  *config.AppConfig
	noti domain.Sender
}

func newWebhookHandler(log logger.Logger, cfg *config.AppConfig, notification domain.Sender) *webhookHandler {
	return &webhookHandler{
		log:  log,
		cfg:  cfg,
		noti: notification,
	}
}

func (h webhookHandler) Routes(r chi.Router) {
	r.Post("/pack", h.pack)
	r.Post("/parse", h.parse)
}

func (h webhookHandler) pack(w http.ResponseWriter, r *http.Request) {
	newProcessor(h.log, h.cfg, h.noti).ProcessSeasonPackHandler(w, r)
	render.Status(r, http.StatusOK)
}

func (h webhookHandler) parse(w http.ResponseWriter, r *http.Request) {
	newProcessor(h.log, h.cfg, h.noti).ParseTorrentHandler(w, r)
	render.Status(r, http.StatusOK)
}
