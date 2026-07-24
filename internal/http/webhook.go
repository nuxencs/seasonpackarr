// Copyright (c) 2023 - 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package http

import (
	"github.com/nuxencs/seasonpackarr/internal/config"
	"github.com/nuxencs/seasonpackarr/internal/domain"
	"github.com/nuxencs/seasonpackarr/internal/logger"
	"github.com/nuxencs/seasonpackarr/internal/metadata"

	"github.com/gin-gonic/gin"
)

type webhookHandler struct {
	log  logger.Logger
	cfg  *config.AppConfig
	noti domain.Sender
	meta *metadata.Provider
}

func newWebhookHandler(log logger.Logger, cfg *config.AppConfig, notification domain.Sender, metadata *metadata.Provider) *webhookHandler {
	return &webhookHandler{
		log:  log,
		cfg:  cfg,
		noti: notification,
		meta: metadata,
	}
}

func (h *webhookHandler) Routes(r *gin.RouterGroup) {
	r.POST("/pack", h.pack)
	r.POST("/parse", h.parse)
}

func (h *webhookHandler) pack(c *gin.Context) {
	newProcessor(h.log, h.cfg, h.noti, h.meta).ProcessSeasonPackHandler(c)
}

func (h *webhookHandler) parse(c *gin.Context) {
	newProcessor(h.log, h.cfg, h.noti, h.meta).ParseTorrentHandler(c)
}
