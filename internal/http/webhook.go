// Copyright (c) 2023 - 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package http

import (
	"github.com/nuxencs/seasonpackarr/internal/config"
	"github.com/nuxencs/seasonpackarr/internal/logger"
	"github.com/nuxencs/seasonpackarr/internal/notification"

	"github.com/gin-gonic/gin"
)

type webhookHandler struct {
	log  logger.Logger
	cfg  config.Provider
	noti notification.Sender
}

func newWebhookHandler(log logger.Logger, cfg config.Provider, sender notification.Sender) *webhookHandler {
	return &webhookHandler{
		log:  log,
		cfg:  cfg,
		noti: sender,
	}
}

func (h *webhookHandler) Routes(r *gin.RouterGroup) {
	r.POST("/candidate", h.candidate)
	r.POST("/pack", h.pack)
	r.POST("/parse", h.parse)
}

func (h *webhookHandler) candidate(c *gin.Context) {
	newProcessor(h.log, h.cfg, h.noti).CandidateSeasonPackHandler(c)
}

func (h *webhookHandler) pack(c *gin.Context) {
	newProcessor(h.log, h.cfg, h.noti).ProcessSeasonPackHandler(c)
}

func (h *webhookHandler) parse(c *gin.Context) {
	newProcessor(h.log, h.cfg, h.noti).ParseTorrentHandler(c)
}
