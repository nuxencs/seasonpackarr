// Copyright (c) 2021 - 2023, Ludvig Lundgren and the autobrr contributors.
// Code is slightly modified for use with seasonpackarr
// SPDX-License-Identifier: GPL-2.0-or-later

package http

import (
	"net/http"

	"github.com/go-chi/render"
)

func (s Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeHealthy(w, r)
}

func writeHealthy(w http.ResponseWriter, r *http.Request) {
	render.PlainText(w, r, "OK")
}

func writeUnhealthy(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusInternalServerError)
	render.PlainText(w, r, "Unhealthy")
}
