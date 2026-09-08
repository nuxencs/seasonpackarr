// Copyright (c) 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package http

import (
	"context"
	"errors"
	"fmt"
	stdhttp "net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nuxencs/seasonpackarr/internal/domain"
	"github.com/nuxencs/seasonpackarr/internal/logger"
	"github.com/nuxencs/seasonpackarr/internal/torrentclient"
	"github.com/stretchr/testify/require"
)

func (f *searchFixture) runRSS(t *testing.T) searchReport {
	t.Helper()
	report, err := f.search.poll(t.Context())
	require.NoError(t, err)
	require.True(t, report.RSS)
	return report
}

func TestRSS_PollsEachIndexerOnceAndRejectsBeforeDownload(t *testing.T) {
	f := newSearchFixture(t, 1, 1, 1)
	f.mock.torrents = append(f.mock.torrents, torrentclient.Torrent{Name: "Another.Show.S02E01.1080p.WEB-DL.H.264-RlsGrp", Hash: "other"})
	f.titles = []string{"Movie.2024.1080p.WEB-DL.H.264-RlsGrp", "Unknown.S01.1080p.WEB-DL.H.264-RlsGrp", "Lifecycle.S01E01.1080p.WEB-DL.H.264-RlsGrp", "Lifecycle.S01.720p.WEB-DL.H.264-RlsGrp"}
	f.respond = func(w stdhttp.ResponseWriter, r *stdhttp.Request) bool {
		if strings.HasSuffix(r.URL.Path, "/api") {
			require.Equal(t, "search", r.URL.Query().Get("t"))
			for _, field := range []string{"q", "season", "year"} {
				require.False(t, r.URL.Query().Has(field))
			}
		}
		return false
	}
	report := f.runRSS(t)
	require.Empty(t, report.Failures)
	require.Equal(t, 2, report.Groups)
	require.Equal(t, []string{"", ""}, f.queries)
	require.Equal(t, 2, report.Requests)
	require.Len(t, report.Outcomes, 2)
	for _, outcome := range report.Outcomes {
		require.Contains(t, outcome.Title, "720p")
		require.Equal(t, "rejected", outcome.Status)
		require.Nil(t, outcome.TotalEpisodes)
	}
	require.Zero(t, f.downloads)
	require.Zero(t, f.mock.fileBatchCalls)
	require.Zero(t, f.mock.importCalls)
}

func TestRSS_ImportsOneVariantAcrossTrackersAndManualSearch(t *testing.T) {
	f := newSearchFixture(t, 1, 1, 1)
	preview := f.runExact(t, true)
	require.Empty(t, preview.Failures)
	require.Equal(t, "would_import", preview.Outcomes[0].Status)
	require.Equal(t, "rejected", preview.Outcomes[1].Status)
	require.Zero(t, f.mock.importCalls)
	report := f.runRSS(t)
	require.Empty(t, report.Failures)
	require.Equal(t, "imported", report.Outcomes[0].Status)
	require.Equal(t, "rejected", report.Outcomes[1].Status)
	require.Equal(t, 1, f.mock.importCalls)
	require.Equal(t, 1, f.downloads, "verified preview metadata is shared with RSS imports")
	for _, next := range []searchReport{f.runRSS(t), f.runExact(t, false)} {
		require.Zero(t, next.Requests)
		require.Zero(t, next.TorrentDownloads)
		require.Equal(t, 1, next.CoveredEpisodeTorrents)
	}
	require.Equal(t, 1, f.mock.importCalls)
}

func TestRSS_RetainedPackRechecksCoverageAfterLeavingFeed(t *testing.T) {
	f := newSearchFixture(t, 2, 2, 1)
	cfg := f.config.Snapshot()
	cfg.Search.IndexerIDs = []int{1}
	f.config.Store(cfg)
	path := filepath.Join(f.sourceDir, f.mock.filesByHash["ep2"][0].Name)
	require.NoError(t, os.Rename(path, path+"-moved"))
	first := f.runRSS(t)
	require.Equal(t, "rejected", first.Outcomes[0].Status)
	require.Equal(t, new(1), first.Outcomes[0].ReusableEpisodes)
	require.Equal(t, 1, first.TorrentDownloads)
	require.Zero(t, f.mock.importCalls)
	f.titles = nil
	require.NoError(t, os.Rename(path+"-moved", path))
	second := f.runRSS(t)
	require.Equal(t, "imported", second.Outcomes[0].Status)
	require.Equal(t, new(2), second.Outcomes[0].ReusableEpisodes)
	require.Equal(t, 1, second.TorrentCacheHits)
	require.Zero(t, second.TorrentDownloads)
	require.Equal(t, 1, f.mock.importCalls)
}

func TestRSS_YearMatchingStillGatesMetadataRetrieval(t *testing.T) {
	for _, skipYear := range []bool{false, true} {
		t.Run(fmt.Sprint(skipYear), func(t *testing.T) {
			f := newSearchFixture(t, 1, 1, 1)
			cfg := f.config.Snapshot()
			cfg.Search.IndexerIDs = []int{1}
			cfg.FuzzyMatching.SkipYearCompare = skipYear
			f.config.Store(cfg)
			f.mock.torrents[0].Name = "Lifecycle.2024.S01E01.1080p.WEB-DL.H.264-RlsGrp"
			f.titles = []string{"Lifecycle.2023.S01.1080p.WEB-DL.H.264-RlsGrp"}
			report := f.runRSS(t)
			require.Empty(t, report.Failures)
			require.Len(t, report.Outcomes, 1)
			if skipYear {
				require.Equal(t, 1, f.downloads)
				require.Equal(t, "imported", report.Outcomes[0].Status)
			} else {
				require.Zero(t, f.downloads)
				require.Zero(t, f.mock.fileBatchCalls)
				require.Equal(t, "rejected", report.Outcomes[0].Status)
			}
		})
	}
}

func TestRSS_CheckpointCatchUpAndRetainedCoverage(t *testing.T) {
	f := newSearchFixture(t, 2, 1, 1) // Keep the pack eligible after each poll.
	cfg := f.config.Snapshot()
	cfg.Search.IndexerIDs = []int{1}
	f.config.Store(cfg)
	f.pages, f.pageSize = true, 1
	f.titles = []string{"Old.S01.1080p.WEB-DL.H.264-RlsGrp"}
	first := f.runRSS(t)
	require.Empty(t, first.Failures)
	require.Equal(t, 1, first.Requests, "first poll reads only the newest page")
	f.titles = append([]string{"New.S01.1080p.WEB-DL.H.264-RlsGrp", f.releaseName}, f.titles...)
	actual := f.runRSS(t)
	require.Empty(t, actual.Failures)
	require.Equal(t, 3, actual.Requests)
	require.Equal(t, "rejected", actual.Outcomes[0].Status)
	repeat := f.runRSS(t)
	require.Empty(t, repeat.Failures)
	require.Equal(t, 1, repeat.Requests)
	require.Equal(t, 1, repeat.TorrentCacheHits, "retained result is checked even when it is not on the first page")
}

func TestRSS_CatchUpLimitReportsGap(t *testing.T) {
	f := newSearchFixture(t, 1, 1, 1)
	cfg := f.config.Snapshot()
	cfg.Search.IndexerIDs = []int{1}
	f.config.Store(cfg)
	f.pages, f.pageSize = true, 1
	f.titles = []string{"Old.S01.1080p.WEB-DL.H.264-RlsGrp"}
	f.runRSS(t)
	f.titles = nil
	for i := range 12 {
		f.titles = append(f.titles, fmt.Sprintf("Unrelated%d.S01.1080p.WEB-DL.H.264-RlsGrp", i))
	}
	report := f.runRSS(t)
	require.Equal(t, 10, report.Requests)
	require.Len(t, report.Failures, 1)
	require.Contains(t, report.Failures[0].Reason, "manual targeted search")
	require.Zero(t, f.downloads)
	require.Equal(t, 1, f.runRSS(t).Requests, "a reported gap must not cause repeated catch-up bursts")
}

func TestRSS_CapabilitiesAndAllowlist(t *testing.T) {
	f := newSearchFixture(t, 2, 1, 1)
	f.respond = func(w stdhttp.ResponseWriter, r *stdhttp.Request) bool {
		if r.URL.Path != "/api/v1/indexer" {
			return false
		}
		fmt.Fprint(w, `[{"id":1,"enable":true,"protocol":"torrent","supportsRss":true}, {"id":2,"enable":true,"protocol":"torrent","supportsSearch":true,"capabilities":{"searchParams":["q"]}}]`)
		return true
	}
	report := f.runRSS(t)
	require.Empty(t, report.Failures)
	require.Equal(t, 1, report.Requests)
	require.Equal(t, 1, report.Outcomes[0].IndexerID)
	search := f.runExact(t, true)
	require.Empty(t, search.Failures)
	require.Equal(t, 2, search.Outcomes[0].IndexerID)
	cfg := f.config.Snapshot()
	cfg.Search.IndexerIDs = []int{2}
	f.config.Store(cfg)
	blocked := f.runRSS(t)
	require.Zero(t, blocked.Requests)
	require.NotEmpty(t, blocked.Failures)
	require.Contains(t, blocked.Failures[0].Reason, "RSS-capable")
}

func TestRSS_CooldownSharedWithTargetedSearch(t *testing.T) {
	for _, firstRSS := range []bool{true, false} {
		t.Run(fmt.Sprint(firstRSS), func(t *testing.T) {
			f := newSearchFixture(t, 2, 1, 1)
			f.failFirst = true
			var report searchReport
			if firstRSS {
				f.runRSS(t)
				report = f.runExact(t, true)
			} else {
				f.runExact(t, true)
				report = f.runRSS(t)
			}
			require.Len(t, report.Failures, 1)
			require.Equal(t, 1, report.Failures[0].IndexerID)
			require.Equal(t, 1, report.Requests)
			require.Equal(t, "rejected", report.Outcomes[0].Status)
		})
	}
}

func TestRSS_FailedCatchUpPreservesCheckpoint(t *testing.T) {
	f := newSearchFixture(t, 2, 1, 1)
	cfg := f.config.Snapshot()
	cfg.Search.IndexerIDs = []int{1}
	f.config.Store(cfg)
	f.pages, f.pageSize = true, 1
	f.titles = []string{"Old.S01.1080p.WEB-DL.H.264-RlsGrp"}
	f.runRSS(t)
	f.titles = append([]string{f.releaseName}, f.titles...)
	f.respond = func(w stdhttp.ResponseWriter, r *stdhttp.Request) bool {
		if r.URL.Path == "/1/api" && r.URL.Query().Get("offset") == "1" {
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(503)
			return true
		}
		return false
	}
	failed := f.runRSS(t)
	require.Len(t, failed.Failures, 1)
	require.Zero(t, f.downloads, "do not download from an indexer after a feed failure")
	f.respond = nil
	recovered := f.runRSS(t)
	require.Empty(t, recovered.Failures)
	require.Equal(t, 2, recovered.Requests, "failed poll must not advance the checkpoint")
	require.Equal(t, "rejected", recovered.Outcomes[0].Status)
}

func TestRSS_ExistingPackSuppressesOnlyMatchingVariant(t *testing.T) {
	f := newSearchFixture(t, 1, 1, 1)
	f.mock.torrents = append(f.mock.torrents,
		torrentclient.Torrent{Name: f.releaseName, Hash: "pack"},
		torrentclient.Torrent{Name: "Lifecycle.S01E01.1080p.WEB-DL.H.264-OtherGrp", Hash: "other"},
	)
	f.titles = append(f.titles, "Lifecycle.S01.1080p.WEB-DL.H.264-OtherGrp")
	report := f.runRSS(t)
	require.Empty(t, report.Failures)
	require.Equal(t, 1, report.CoveredEpisodeTorrents)
	require.Len(t, report.Outcomes, 4)
	for _, outcome := range report.Outcomes {
		if outcome.Title == f.releaseName {
			require.Equal(t, "rejected", outcome.Status)
		} else {
			require.Equal(t, "rejected", outcome.Status)
			require.Contains(t, outcome.Reason, "no accessible episode files")
		}
	}
	require.Zero(t, f.downloads)
}

func TestRSS_ConnectionChangeClearsFeedState(t *testing.T) {
	f := newSearchFixture(t, 2, 1, 1)
	cfg := f.config.Snapshot()
	cfg.Search.IndexerIDs = []int{1}
	f.config.Store(cfg)
	f.pages, f.pageSize = true, 1
	f.titles = []string{"Old.S01.1080p.WEB-DL.H.264-RlsGrp"}
	f.runRSS(t)
	f.titles = append([]string{f.releaseName}, f.titles...)
	report := f.runRSS(t)
	require.Empty(t, report.Failures)
	require.Equal(t, 2, report.Requests)
	cfg.Search.ProwlarrURL += "/"
	f.config.Store(cfg)
	f.titles = []string{"Unrelated.S01.1080p.WEB-DL.H.264-RlsGrp"}
	reset := f.runRSS(t)
	require.Empty(t, reset.Failures)
	require.Empty(t, reset.Outcomes, "connection changes clear retained candidates")
	require.Equal(t, 1, reset.Requests)
}

func TestRSSSchedule_PollsRSSAndCancels(t *testing.T) {
	f := newSearchFixture(t, 1, 1, 1)
	cfg := f.config.Snapshot()
	cfg.Search.RSSInterval = "1ms" // Bypass config validation to exercise the worker.
	f.config.Store(cfg)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	f.beforeSearch = cancel
	runner := &searchRunner{cfg: f.config, log: logger.New(&domain.Config{LogLevel: "ERROR"})}
	done := make(chan struct{})
	go func() { runner.schedule(ctx); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("RSS scheduler did not stop after cancellation")
	}
	require.Equal(t, []string{""}, f.queries, "the only automatic request must be an RSS poll")
}

func TestRSS_UnavailableClientDoesNotLoseFeedEntries(t *testing.T) {
	f := newSearchFixture(t, 2, 1, 1)
	cfg := f.config.Snapshot()
	cfg.Search.IndexerIDs = []int{1}
	otherCfg := cloneClientConfig(*cfg.Clients["default"])
	otherCfg.Host = "http://independent:8080"
	cfg.Clients["other"] = &otherCfg
	f.config.Store(cfg)
	other := &mockTorrentClient{torrents: []torrentclient.Torrent{{Name: "Other.Show.S01E01.1080p.WEB-DL.H.264-RlsGrp", Hash: "other"}}}
	clientMap.Store("other", cachedTorrentClient{config: otherCfg, client: other})
	f.pages, f.pageSize = true, 1
	f.titles = []string{"Old.S01.1080p.WEB-DL.H.264-RlsGrp"}
	f.runRSS(t)
	other.torrentsErr = errors.New("client unavailable")
	f.titles = append([]string{"Newest.S01.1080p.WEB-DL.H.264-RlsGrp", "Other.Show.S01.1080p.WEB-DL.H.264-RlsGrp"}, f.titles...)
	partial := f.runRSS(t)
	require.Len(t, partial.Failures, 1)
	require.Equal(t, "other", partial.Failures[0].ClientName)
	other.torrentsErr = nil
	recovered := f.runRSS(t)
	require.Empty(t, recovered.Failures)
	require.Equal(t, 3, recovered.Requests)
	require.Len(t, recovered.Outcomes, 1)
	require.Equal(t, "other", recovered.Outcomes[0].ClientName)
	require.Equal(t, "rejected", recovered.Outcomes[0].Status)
	require.Contains(t, recovered.Outcomes[0].Reason, "no accessible episode files")
}
