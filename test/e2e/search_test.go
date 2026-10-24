// Copyright (c) 2023 - 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

//go:build e2e

package e2e

import (
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/nuxencs/seasonpackarr/internal/daemontest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// searchTimeout bounds one search request. seasonpackarr spaces its Prowlarr
// requests by search.requestInterval (10s), and one search sends three:
// indexer discovery, the search, and the torrent download.
const searchTimeout = 2 * time.Minute

// TestSearchE2E_PartialPack runs the partial pack against qBittorrent, with a
// search through Prowlarr in place of an announce (ADR-0003).
func TestSearchE2E_PartialPack(t *testing.T) {
	daemontest.RequireSettings(t, envQbitHost)
	h := requireProwlarr(t)
	c := newQbit(t)
	partialPack(t, h, c, func(t *testing.T, name string, torrent []byte) {
		h.addRelease(t, name, torrent)
		report := search(t, c.configName())

		assert.Empty(t, report.Failures, "search failures")
		if assert.Len(t, report.Outcomes, 1, "search outcomes") {
			outcome := report.Outcomes[0]
			assert.Equal(t, name, outcome.Title, "search result")
			assert.Equal(t, "imported", outcome.Status, "search outcome: %s", outcome.Reason)
		}
		// The download proves that seasonpackarr accepted the proxy link of Prowlarr.
		assert.Equal(t, 1, report.TorrentDownloads, "torrent downloads through Prowlarr")
		assert.Zero(t, report.TorrentCacheHits, "torrent cache hits")
	})
}

// searchReport is the part of the POST /api/search report that the test checks.
type searchReport struct {
	TorrentDownloads int `json:"torrentDownloads"`
	TorrentCacheHits int `json:"torrentCacheHits"`
	Outcomes         []struct {
		Title  string `json:"title"`
		Status string `json:"status"`
		Reason string `json:"reason"`
	} `json:"outcomes"`
	Failures []struct {
		Query     string `json:"query"`
		IndexerID int    `json:"indexerId"`
		Reason    string `json:"reason"`
	} `json:"failures"`
}

// search sends the request of seasonpackarr search for clientName, the user's
// entry point of search, and returns the report. It imports, so it returns
// after seasonpackarr sent the import to the client.
func search(t *testing.T, clientName string) searchReport {
	t.Helper()
	api := jsonAPI{
		baseURL:   strings.TrimSuffix(os.Getenv(envSeasonpackarrURL), "/"),
		client:    &http.Client{Timeout: searchTimeout},
		keyHeader: "X-API-Token",
		apiKey:    os.Getenv(envAPIToken),
	}
	var report searchReport
	// The other clients of the harness config do not run in this entry.
	body := map[string]any{"clientname": clientName, "dryRun": false}
	require.NoError(t, api.do(http.MethodPost, "/api/search", body, http.StatusOK, &report), "search through seasonpackarr")
	t.Logf("search report: %+v", report)
	return report
}
