// Copyright (c) 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package http

import (
	"errors"
	"fmt"
	stdhttp "net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/synctest"

	"github.com/nuxencs/seasonpackarr/internal/domain"
	"github.com/nuxencs/seasonpackarr/internal/torrentclient"
	"github.com/nuxencs/seasonpackarr/internal/torrentclient/torrentclienttest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRSS_PollsEachIndexerOnceAndRejectsBeforeDownload(t *testing.T) {
	f := newSearchFixture(t, fixtureOptions{packEpisodes: 1, clientEpisodes: 1, threshold: 1})
	f.torrentClient.Torrents = append(f.torrentClient.Torrents, torrentclient.Torrent{Name: "Another.Show.S02E01.1080p.WEB-DL.H.264-RlsGrp", Hash: "other"})
	f.titles = []string{"Movie.2024.1080p.WEB-DL.H.264-RlsGrp", "Unknown.S01.1080p.WEB-DL.H.264-RlsGrp", "Lifecycle.S01E01.1080p.WEB-DL.H.264-RlsGrp", "Lifecycle.S01.720p.WEB-DL.H.264-RlsGrp"}
	f.respond = func(w stdhttp.ResponseWriter, r *stdhttp.Request) bool {
		if strings.HasSuffix(r.URL.Path, "/api") {
			assert.Equal(t, "search", r.URL.Query().Get("t"))
			for _, field := range []string{"q", "season", "year"} {
				assert.False(t, r.URL.Query().Has(field))
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
	require.Zero(t, f.torrentClient.FileBatchCalls)
	require.Zero(t, f.torrentClient.ImportCalls)
}

func TestRSS_ImportsOneVariantAcrossTrackersAndManualSearch(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		f := newSearchFixture(t, fixtureOptions{packEpisodes: 1, clientEpisodes: 1, threshold: 1})
		preview := f.runExact(t, true)
		require.Empty(t, preview.Failures)
		require.Equal(t, "would_import", preview.Outcomes[0].Status)
		require.Equal(t, "rejected", preview.Outcomes[1].Status)
		require.Zero(t, f.torrentClient.ImportCalls)
		report := f.runRSS(t)
		require.Empty(t, report.Failures)
		require.Equal(t, "imported", report.Outcomes[0].Status)
		require.Equal(t, "rejected", report.Outcomes[1].Status)
		require.Equal(t, 1, f.torrentClient.ImportCalls)
		require.Equal(t, 1, f.downloads, "the RSS import uses the cached torrent of the verified preview")
		for _, next := range []searchReport{f.runRSS(t), f.runExact(t, false)} {
			require.Zero(t, next.Requests)
			require.Zero(t, next.TorrentDownloads)
			require.Equal(t, 1, next.CoveredEpisodeTorrents)
		}
		require.Equal(t, 1, f.torrentClient.ImportCalls)
	})
}

func TestRSS_ImportedPackSendsRSSNotification(t *testing.T) {
	f := newSearchFixture(t, fixtureOptions{packEpisodes: 1, clientEpisodes: 1, threshold: 1})
	report := f.runRSS(t)
	require.Equal(t, "imported", report.Outcomes[0].Status)

	sent := f.sentNotifications(t)
	require.Len(t, sent, 1)
	assert.Equal(t, domain.StatusSuccessfulHardlink, sent[0].statusCode)
	assert.Equal(t, "RSS", sent[0].payload.Action)
	assert.Equal(t, f.releaseName, sent[0].payload.ReleaseName)
	assert.NoError(t, sent[0].payload.Error)
}

func TestRSS_RetainedPackRechecksCoverageAfterLeavingFeed(t *testing.T) {
	f := newSearchFixture(t, fixtureOptions{packEpisodes: 2, clientEpisodes: 2, threshold: 1})
	cfg := f.config.Snapshot()
	cfg.Search.IndexerIDs = []int{1}
	f.config.Store(cfg)
	path := filepath.Join(f.sourceDir, f.torrentClient.FilesByHash["ep2"][0].Name)
	require.NoError(t, os.Rename(path, path+"-moved"))
	first := f.runRSS(t)
	require.Equal(t, "rejected", first.Outcomes[0].Status)
	require.Equal(t, new(1), first.Outcomes[0].ReusableEpisodes)
	require.Equal(t, 1, first.TorrentDownloads)
	require.Zero(t, f.torrentClient.ImportCalls)
	f.restart(t)
	f.titles = nil
	require.NoError(t, os.Rename(path+"-moved", path))
	second := f.runRSS(t)
	require.Equal(t, "imported", second.Outcomes[0].Status)
	require.Equal(t, new(2), second.Outcomes[0].ReusableEpisodes)
	require.Equal(t, 1, second.TorrentCacheHits)
	require.Zero(t, second.TorrentDownloads)
	require.Equal(t, 1, f.torrentClient.ImportCalls)
}

func TestRSS_YearMatchingStillGatesTorrentDownload(t *testing.T) {
	for _, skipYear := range []bool{false, true} {
		t.Run(fmt.Sprint(skipYear), func(t *testing.T) {
			f := newSearchFixture(t, fixtureOptions{packEpisodes: 1, clientEpisodes: 1, threshold: 1})
			cfg := f.config.Snapshot()
			cfg.Search.IndexerIDs = []int{1}
			cfg.FuzzyMatching.SkipYearCompare = skipYear
			f.config.Store(cfg)
			f.torrentClient.Torrents[0].Name = "Lifecycle.2024.S01E01.1080p.WEB-DL.H.264-RlsGrp"
			f.titles = []string{"Lifecycle.2023.S01.1080p.WEB-DL.H.264-RlsGrp"}
			report := f.runRSS(t)
			require.Empty(t, report.Failures)
			require.Len(t, report.Outcomes, 1)
			if skipYear {
				require.Equal(t, 1, f.downloads)
				require.Equal(t, "imported", report.Outcomes[0].Status)
			} else {
				require.Zero(t, f.downloads)
				require.Zero(t, f.torrentClient.FileBatchCalls)
				require.Equal(t, "rejected", report.Outcomes[0].Status)
			}
		})
	}
}

func TestRSS_ExistingPackSuppressesOnlyMatchingVariant(t *testing.T) {
	f := newSearchFixture(t, fixtureOptions{packEpisodes: 1, clientEpisodes: 1, threshold: 1})
	f.torrentClient.Torrents = append(f.torrentClient.Torrents,
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

func TestRSS_UnavailableClientDoesNotLoseFeedEntries(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		f := newSearchFixture(t, fixtureOptions{packEpisodes: 2, clientEpisodes: 1, threshold: 1})
		cfg := f.config.Snapshot()
		cfg.Search.IndexerIDs = []int{1}
		otherCfg := cfg.Clients["default"].Clone()
		otherCfg.Host = "http://independent:8080"
		cfg.Clients["other"] = &otherCfg
		f.config.Store(cfg)
		other := &torrentclienttest.Client{Torrents: []torrentclient.Torrent{{Name: "Other.Show.S01E01.1080p.WEB-DL.H.264-RlsGrp", Hash: "other"}}}
		f.connector.use(otherCfg.Host, other)
		f.pages, f.pageSize = true, 1
		f.titles = []string{"Old.S01.1080p.WEB-DL.H.264-RlsGrp"}
		f.runRSS(t)
		other.TorrentsErr = errors.New("client unavailable")
		f.titles = append([]string{"Newest.S01.1080p.WEB-DL.H.264-RlsGrp", "Other.Show.S01.1080p.WEB-DL.H.264-RlsGrp"}, f.titles...)
		partial := f.runRSS(t)
		require.Len(t, partial.Failures, 1)
		require.Equal(t, "other", partial.Failures[0].ClientName)
		f.restart(t)
		other.TorrentsErr = nil
		recovered := f.runRSS(t)
		require.Empty(t, recovered.Failures)
		require.Equal(t, 3, recovered.Requests)
		require.Len(t, recovered.Outcomes, 1)
		require.Equal(t, "other", recovered.Outcomes[0].ClientName)
		require.Equal(t, "rejected", recovered.Outcomes[0].Status)
		require.Contains(t, recovered.Outcomes[0].Reason, "no accessible episode files")
	})
}

func TestRSS_CheckpointWriteFailureDoesNotImport(t *testing.T) {
	f := newSearchFixture(t, fixtureOptions{packEpisodes: 1, clientEpisodes: 1, threshold: 1})
	f.beforeSearch = func() { assert.NoError(t, f.store.Close()) }
	report := f.runRSS(t)
	require.Len(t, report.Failures, 1)
	require.Equal(t, "could not access database; check service logs", report.Failures[0].Reason)
	require.Len(t, f.queries, 1)
	require.Zero(t, f.downloads)
	require.Zero(t, f.torrentClient.ImportCalls)
	f.restart(t)
	f.beforeSearch = nil
	recovered := f.runRSS(t)
	require.Empty(t, recovered.Failures)
	require.Equal(t, "imported", recovered.Outcomes[0].Status)
}

func (f *searchFixture) runRSS(t *testing.T) searchReport {
	t.Helper()
	report, err := f.search.poll(t.Context())
	require.NoError(t, err)
	require.True(t, report.RSS)
	return report
}
