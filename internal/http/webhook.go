// Copyright (c) 2023 - 2024, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package http

import (
	"net/http"

	"github.com/nuxencs/seasonpackarr/internal/config"
	"github.com/nuxencs/seasonpackarr/internal/domain"
	"github.com/nuxencs/seasonpackarr/internal/logger"

	"github.com/gin-gonic/gin"
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

func (h *webhookHandler) Routes(r *gin.RouterGroup) {
	r.POST("/pack", h.pack)
	r.POST("/parse", h.parse)
}

func (h *webhookHandler) pack(c *gin.Context) {
	newProcessor(h.log, h.cfg, h.noti).ProcessSeasonPackHandler(c.Writer, c.Request)
	c.Status(http.StatusOK)
}

func (h *webhookHandler) parse(c *gin.Context) {
	newProcessor(h.log, h.cfg, h.noti).ParseTorrentHandler(c.Writer, c.Request)
	c.Status(http.StatusOK)
}
