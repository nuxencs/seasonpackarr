// Copyright (c) 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package http

import (
	"bytes"
	stdhttp "net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAuthMiddleware_LogsNoAPIToken(t *testing.T) {
	const (
		configured = "sentinel-configured-api-token"
		submitted  = "sentinel-submitted-api-token"
	)
	tests := []struct {
		name       string
		header     string
		query      string
		authorized bool
	}{
		{name: "wrong header token", header: submitted},
		{name: "wrong apikey query", query: submitted},
		{name: "correct apikey query", query: configured, authorized: true},
		{name: "no token"},
	}
	for _, path := range []string{"/api/candidate", "/api/match", "/api/import", "/api/search"} {
		t.Run(path, func(t *testing.T) {
			for _, tt := range tests {
				t.Run(tt.name, func(t *testing.T) {
					log := newTraceLogger(t)
					f := newProcessorHTTPFixture(t, fixtureOptions{packEpisodes: 1, clientEpisodes: 1, log: log})
					cfg := f.config.Snapshot()
					cfg.APIToken = configured
					f.config.Store(cfg)
					body := []byte(`{}`)
					if path != "/api/search" {
						body = encodeJSON(t, f.packPayload())
					}
					target := path
					if tt.query != "" {
						target += "?" + url.Values{"apikey": {tt.query}}.Encode()
					}
					req := httptest.NewRequestWithContext(t.Context(), stdhttp.MethodPost, target, bytes.NewReader(body))
					req.Header.Set("Content-Type", "application/json")
					if tt.header != "" {
						req.Header.Set("X-API-Token", tt.header)
					}
					res := httptest.NewRecorder()
					f.handler.ServeHTTP(res, req)

					// The auth and access events must exist, or the check below proves nothing about them.
					rejected := strings.Contains(log.String(), "unauthorized access attempt")
					if tt.authorized {
						require.NotEqual(t, stdhttp.StatusUnauthorized, res.Code, res.Body.String())
						require.False(t, rejected, log.String())
					} else {
						require.Equal(t, stdhttp.StatusUnauthorized, res.Code, res.Body.String())
						require.True(t, rejected, log.String())
					}
					log.Events(t).RequireField(t, "incoming_request", "url", path)
					log.AssertNoSecrets(t, map[string]string{"configured apiToken": configured, "submitted apiToken": submitted})
				})
			}
		})
	}
}
