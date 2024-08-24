// Copyright (c) 2021 - 2024, Ludvig Lundgren and the autobrr contributors.
// Code is slightly modified for use with seasonpackarr
// SPDX-License-Identifier: GPL-2.0-or-later

package http

import (
	"net/http"

	"github.com/go-chi/chi/v5"
)

type healthHandler struct{}

func newHealthHandler() *healthHandler {
	return &healthHandler{}
}

func (h *healthHandler) Routes(r chi.Router) {
	r.Get("/liveness", h.handleLiveness)
	r.Get("/readiness", h.handleReadiness)
}

func (h *healthHandler) handleLiveness(w http.ResponseWriter, r *http.Request) {
	writeHealthy(w, r)
}

func (h *healthHandler) handleReadiness(w http.ResponseWriter, r *http.Request) {
	writeHealthy(w, r)
}

func writeHealthy(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("OK"))
}

func writeUnhealthy(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain")
	w.WriteHeader(http.StatusInternalServerError)
	w.Write([]byte("Unhealthy"))
}
