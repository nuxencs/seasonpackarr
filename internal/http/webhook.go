// Copyright (c) 2023 - 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package http

import (
	"github.com/nuxencs/seasonpackarr/internal/config"
	"github.com/nuxencs/seasonpackarr/internal/domain"
	"github.com/nuxencs/seasonpackarr/internal/logger"

	"github.com/gin-gonic/gin"
)

type webhookHandler struct {
	log   logger.Logger
	cfg   config.Provider
	noti  domain.Sender
	tasks *taskGroup
}

func newWebhookHandler(log logger.Logger, cfg config.Provider, notification domain.Sender, tasks *taskGroup) *webhookHandler {
	return &webhookHandler{
		log:   log,
		cfg:   cfg,
		noti:  notification,
		tasks: tasks,
	}
}

func (h *webhookHandler) Routes(r *gin.RouterGroup) {
	r.POST("/candidate", h.candidate)
	r.POST("/match", h.match)
	r.POST("/import", h.importPack)
}

func (h *webhookHandler) candidate(c *gin.Context) {
	newProcessor(h.log, h.cfg, h.noti, h.tasks).CandidateSeasonPackHandler(c)
}

func (h *webhookHandler) match(c *gin.Context) {
	newProcessor(h.log, h.cfg, h.noti, h.tasks).MatchSeasonPackHandler(c)
}

func (h *webhookHandler) importPack(c *gin.Context) {
	newProcessor(h.log, h.cfg, h.noti, h.tasks).ImportSeasonPackHandler(c)
}
