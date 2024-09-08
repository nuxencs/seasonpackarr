// Copyright (c) 2021 - 2024, Ludvig Lundgren and the autobrr contributors.
// Code is slightly modified for use with seasonpackarr
// SPDX-License-Identifier: GPL-2.0-or-later

package http

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

type healthHandler struct{}

func newHealthHandler() *healthHandler {
	return &healthHandler{}
}

func (h *healthHandler) Routes(r *gin.RouterGroup) {
	r.GET("/liveness", h.handleLiveness)
	r.GET("/readiness", h.handleReadiness)
}

func (h *healthHandler) handleLiveness(c *gin.Context) {
	writeHealthy(c)
}

func (h *healthHandler) handleReadiness(c *gin.Context) {
	writeHealthy(c)
}

func writeHealthy(c *gin.Context) {
	c.Header("Content-Type", "text/plain")
	c.String(http.StatusOK, "OK")
}

func writeUnhealthy(c *gin.Context) {
	c.Header("Content-Type", "text/plain")
	c.String(http.StatusInternalServerError, "Unhealthy")
}
