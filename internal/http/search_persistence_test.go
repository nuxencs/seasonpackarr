// Copyright (c) 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package http

import (
	"encoding/json"
	stdhttp "net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nuxencs/seasonpackarr/internal/state"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"
)

func (f *searchFixture) restart(t *testing.T) {
	t.Helper()
	require.NoError(t, f.search.state.Close())
	store, err := state.Open(t.Context(), f.statePath, zerolog.Nop())
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })
	server := NewServer(f.search.log, f.config, noopNotificationSender{}, store)
	f.search, f.handler = server.search, server.Handler()
}

func TestSearch_MetadataSurvivesRestartButDecisionsDoNot(t *testing.T) {
	f := newSearchFixture(t, 2, 2, 1)
	cfg := f.config.Snapshot()
	cfg.Search.IndexerIDs = []int{1}
	f.config.Store(cfg)
	path := filepath.Join(f.sourceDir, f.mock.filesByHash["ep2"][0].Name)
	require.NoError(t, os.Rename(path, path+"-moved"))
	first := f.runExact(t, true)
	require.Equal(t, "rejected", first.Outcomes[0].Status)
	require.Equal(t, 1, first.TorrentDownloads)
	f.restart(t)
	require.NoError(t, os.Rename(path+"-moved", path))
	next := f.runExact(t, true)
	require.Empty(t, next.Failures)
	require.Equal(t, "would_import", next.Outcomes[0].Status)
	require.Equal(t, 1, next.TorrentCacheHits)
	require.Zero(t, next.TorrentDownloads)
	require.Zero(t, f.mock.importCalls)
}

func TestSearch_DatabaseFailureStopsDiscovery(t *testing.T) {
	f := newSearchFixture(t, 1, 1, 1)
	require.NoError(t, f.search.state.Close())
	response := f.postJSON(t, "/api/search", map[string]any{"dryRun": false})
	require.Equal(t, 500, response.Code)
	require.JSONEq(t, `{"error":"could not access database; check service logs"}`, response.Body.String())
	require.Zero(t, f.discoveryCalls)
	require.Zero(t, f.downloads)
	require.Zero(t, f.mock.importCalls)
}

func TestSearch_MetadataFailureDoesNotImport(t *testing.T) {
	for _, stage := range []string{"read", "write"} {
		t.Run(stage, func(t *testing.T) {
			f := newSearchFixture(t, 1, 1, 1)
			f.pages, f.pageSize = true, 1
			if stage == "read" {
				f.beforeSearch = func() { require.NoError(t, f.search.state.Close()) }
			} else {
				f.respond = func(_ stdhttp.ResponseWriter, r *stdhttp.Request) bool {
					if strings.HasSuffix(r.URL.Path, "/download") {
						require.NoError(t, f.search.state.Close())
					}
					return false
				}
			}
			response := f.postJSON(t, "/api/search", map[string]any{"dryRun": false})
			require.Equal(t, 200, response.Code)
			var report searchReport
			require.NoError(t, json.Unmarshal(response.Body.Bytes(), &report))
			require.Len(t, report.Failures, 1)
			require.Equal(t, "could not access database; check service logs", report.Failures[0].Reason)
			require.Len(t, f.queries, 1, "storage failure stops pagination and other indexers")
			if stage == "read" {
				require.Zero(t, f.downloads)
			} else {
				require.Equal(t, 1, f.downloads)
			}
			require.Zero(t, f.mock.importCalls)
		})
	}
}

func TestRSS_CheckpointWriteFailureDoesNotImport(t *testing.T) {
	f := newSearchFixture(t, 1, 1, 1)
	f.beforeSearch = func() { require.NoError(t, f.search.state.Close()) }
	report := f.runRSS(t)
	require.Len(t, report.Failures, 1)
	require.Equal(t, "could not access database; check service logs", report.Failures[0].Reason)
	require.Len(t, f.queries, 1)
	require.Zero(t, f.downloads)
	require.Zero(t, f.mock.importCalls)
	f.restart(t)
	f.beforeSearch = nil
	recovered := f.runRSS(t)
	require.Empty(t, recovered.Failures)
	require.Equal(t, "imported", recovered.Outcomes[0].Status)
}
