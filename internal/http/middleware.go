// Copyright (c) 2021 - 2024, Ludvig Lundgren and the autobrr contributors.
// Code is modified for use with seasonpackarr
// SPDX-License-Identifier: GPL-2.0-or-later

package http

import "net/http"

func (s Server) isAuthenticated(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// allow access if apiToken value is set to an empty string
		if s.cfg.Config.APIToken == "" {
			next.ServeHTTP(w, r)
			return
		}
		if token := r.Header.Get("X-API-Token"); token != "" {
			// check header
			if token != s.cfg.Config.APIToken {
				s.log.Error().Msgf("unauthorized access attempt with incorrect API token in header from IP: %s", r.RemoteAddr)
				http.Error(w, http.StatusText(http.StatusUnauthorized), http.StatusUnauthorized)
				return
			}
		} else if key := r.URL.Query().Get("apikey"); key != "" {
			// check query param ?apikey=TOKEN
			if key != s.cfg.Config.APIToken {
				s.log.Error().Msgf("unauthorized access attempt with incorrect API token in query parameters from IP: %s", r.RemoteAddr)
				http.Error(w, http.StatusText(http.StatusUnauthorized), http.StatusUnauthorized)
				return
			}
		} else {
			// neither header nor query parameter provided a token
			s.log.Error().Msgf("unauthorized access attempt without API token from IP: %s", r.RemoteAddr)
			http.Error(w, http.StatusText(http.StatusUnauthorized), http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}
