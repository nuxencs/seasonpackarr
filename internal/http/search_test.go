// Copyright (c) 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package http

import (
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nuxencs/seasonpackarr/internal/domain"
	"github.com/nuxencs/seasonpackarr/internal/torrentclient"
	"github.com/nuxencs/seasonpackarr/internal/torrents"

	"github.com/stretchr/testify/require"
)

func TestSearch_PreviewGroupsEpisodesAndSelectsOneVariant(t *testing.T) {
	f := newSearchFixture(t, 3, 3, 0.75)
	report := f.runExact(t, true)
	require.Empty(t, report.Failures)
	require.Equal(t, 1, report.Groups)
	require.Equal(t, []string{"Lifecycle S01", "Lifecycle S01"}, f.queries)
	require.Len(t, report.Outcomes, 2)
	require.Equal(t, "would_import", report.Outcomes[0].Status)
	require.Equal(t, new(3), report.Outcomes[0].ReusableEpisodes)
	require.Equal(t, new(3), report.Outcomes[0].TotalEpisodes)
	require.Equal(t, "rejected", report.Outcomes[1].Status)
	require.Equal(t, 1, f.downloads)
	require.Zero(t, f.torrentClient.importCalls)
	entries, err := os.ReadDir(f.importDir)
	require.NoError(t, err)
	require.Empty(t, entries)
}

func TestSearch_ImportsOnceAndSkipsExistingPackAcrossTrackers(t *testing.T) {
	f := newSearchFixture(t, 2, 2, 0.75)
	first := f.runExact(t, false)
	require.Equal(t, "imported", first.Outcomes[0].Status)
	require.Equal(t, 1, f.torrentClient.importCalls)
	source, err := os.Stat(filepath.Join(f.sourceDir, "Lifecycle.S01E01.1080p.WEB-DL.H.264-RlsGrp.mkv"))
	require.NoError(t, err)
	target, err := os.Stat(filepath.Join(f.importDir, f.releaseName, "Lifecycle.S01E01.1080p.WEB-DL.H.264-RlsGrp.mkv"))
	require.NoError(t, err)
	require.True(t, os.SameFile(source, target))
	second := f.runExact(t, false)
	require.Empty(t, second.Outcomes)
	require.Zero(t, second.Groups)
	require.Zero(t, second.Requests)
	require.Equal(t, 2, second.CoveredEpisodeTorrents)
	require.Equal(t, 1, f.torrentClient.importCalls)
	require.Equal(t, 1, f.downloads)
}

func TestSearch_RespectsSmartMode(t *testing.T) {
	for _, tt := range []struct {
		name      string
		enabled   bool
		threshold float32
		want      string
	}{
		{"below threshold", true, 0.75, "rejected"},
		{"at threshold", true, 0.5, "would_import"},
		{"disabled", false, 0.75, "would_import"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			f := newSearchFixture(t, 2, 1, tt.threshold)
			cfg := f.config.Snapshot()
			cfg.SmartMode = tt.enabled
			f.config.Store(cfg)
			report := f.runExact(t, true)
			require.Equal(t, tt.want, report.Outcomes[0].Status)
			if tt.want == "rejected" {
				require.Equal(t, domain.StatusBelowThreshold.String(), report.Outcomes[0].Reason)
			}
		})
	}
}

func TestSearch_FailedTrackerDoesNotBlockOthers(t *testing.T) {
	f := newSearchFixture(t, 1, 1, 0.75)
	f.failFirst = true
	report := f.runExact(t, true)
	require.Len(t, report.Failures, 1)
	require.Equal(t, 1, report.Failures[0].IndexerID)
	require.Len(t, report.Outcomes, 1)
	require.Equal(t, "would_import", report.Outcomes[0].Status)
	require.Equal(t, 2, report.Outcomes[0].IndexerID)
}

func TestSearch_RejectsIncompatibleResultsBeforeDownload(t *testing.T) {
	f := newSearchFixture(t, 1, 1, 0.75)
	f.titles = []string{"Other.S01.1080p.WEB-DL.H.264-RlsGrp", "Lifecycle.S02.1080p.WEB-DL.H.264-RlsGrp", "Lifecycle.S01.2160p.WEB-DL.H.265-RlsGrp", "Lifecycle.S01.1080p.WEB-DL.H.264-OtherGrp"}
	report := f.runExact(t, true)
	for _, outcome := range report.Outcomes {
		require.Equal(t, "rejected", outcome.Status)
	}
	require.Zero(t, f.downloads)
	require.Zero(t, f.torrentClient.fileCalls)
}

func TestSearch_AuthAndOverlappingRuns(t *testing.T) {
	f := newSearchFixture(t, 1, 1, 0.75)
	unauth := httptest.NewRecorder()
	f.handler.ServeHTTP(unauth, httptest.NewRequest("POST", "/api/search", strings.NewReader(`{"dryRun":true}`)))
	require.Equal(t, 401, unauth.Code)
	require.Empty(t, f.queries)
	started := make(chan struct{})
	resume := make(chan struct{})
	f.beforeSearch = func() {
		select {
		case <-started:
		default:
			close(started)
			<-resume
		}
	}
	first := make(chan *httptest.ResponseRecorder, 1)
	go func() { first <- f.postJSON(t, "/api/search", map[string]any{"dryRun": true}) }()
	<-started
	second := f.postJSON(t, "/api/search", map[string]any{"dryRun": true})
	require.Equal(t, 409, second.Code)
	close(resume)
	require.Equal(t, 200, (<-first).Code)
}

func TestSearch_ConcurrentWebhookCannotAddAnotherVariantCopy(t *testing.T) {
	f := newSearchFixture(t, 1, 1, 0.75)
	importing, resume := make(chan struct{}), make(chan struct{})
	cfg := f.config.Snapshot()
	clientMap.Store("default", cachedTorrentClient{config: *cfg.Clients["default"], client: &fakeSearchClient{fakeTorrentClient: f.torrentClient, importing: importing, resume: resume}})
	first := make(chan *httptest.ResponseRecorder, 1)
	go func() { first <- f.postJSON(t, "/api/search", map[string]any{"dryRun": false}) }()
	<-importing
	second := make(chan *httptest.ResponseRecorder, 1)
	go func() { second <- f.postJSON(t, "/api/import", f.packPayload()) }()
	close(resume)
	require.Equal(t, 200, (<-first).Code)
	require.Equal(t, domain.StatusAlreadyInClient.Code(), (<-second).Code)
	require.Equal(t, 1, f.torrentClient.importCalls)
}

func TestSearch_SeparateReleaseVariantsShareQuery(t *testing.T) {
	f := newSearchFixture(t, 1, 1, 0.75)
	otherPack := "Lifecycle.S01.2160p.WEB-DL.H.265-OtherGrp"
	otherEpisode := "Lifecycle.S01E01.2160p.WEB-DL.H.265-OtherGrp"
	f.titles = append(f.titles, otherPack)
	data, err := torrents.TorrentFromRls(otherPack, 1)
	require.NoError(t, err)
	f.torrentByTitle = map[string][]byte{otherPack: data}
	writeEpisode(t, filepath.Join(f.sourceDir, otherEpisode+".mkv"))
	f.torrentClient.torrents = append(f.torrentClient.torrents, torrentclient.Torrent{Name: otherEpisode, Hash: "other", SavePath: f.sourceDir})
	f.torrentClient.filesByHash["other"] = []torrentclient.File{{Name: otherEpisode + ".mkv", Size: 1}}
	report := f.runExact(t, false)
	require.Empty(t, report.Failures)
	require.Equal(t, 1, report.Groups)
	require.Equal(t, 2, f.torrentClient.importCalls)
	require.Equal(t, "imported", report.Outcomes[0].Status)
	require.Equal(t, "imported", report.Outcomes[1].Status)
	require.Equal(t, "rejected", report.Outcomes[2].Status)
}

func TestSearch_GroupIdentityAndUnrelatedTorrents(t *testing.T) {
	f := newSearchFixture(t, 1, 1, 0.75)
	for _, name := range []string{
		"Example.2024.S01E01.1080p.WEB-DL.H.264-RlsGrp",
		"Example.2025.S01E01.1080p.WEB-DL.H.264-RlsGrp",
		"Example.S01E01.1080p.WEB-DL.H.264-RlsGrp",
		"Example.2024.S02E01.1080p.WEB-DL.H.264-RlsGrp",
		"Film.2024.1080p.BluRay.x264-RlsGrp",
		"Packed.S01.1080p.WEB-DL.H.264-RlsGrp",
		"readme.txt",
	} {
		f.torrentClient.torrents = append(f.torrentClient.torrents, torrentclient.Torrent{Name: name, Hash: name})
	}
	report := f.runExact(t, true)
	require.Equal(t, 3, report.Groups)
	require.ElementsMatch(t, []string{
		"Example S01", "Example S01", // all years and a missing year share two indexer queries
		"Example S02", "Example S02",
		"Lifecycle S01", "Lifecycle S01",
	}, f.queries)
}

func TestSearch_PaginationFindsPackAfterRejectedResult(t *testing.T) {
	f := newSearchFixture(t, 1, 1, 0.75)
	f.pages = true
	f.pageSize = 1
	f.titles = []string{"Unrelated.S01.1080p.WEB-DL.H.264-RlsGrp", f.releaseName}
	report := f.runExact(t, true)
	require.Empty(t, report.Failures)
	require.Equal(t, 4, report.Requests) // three pages on first tracker, one on second
	require.Equal(t, "would_import", report.Outcomes[1].Status)
	require.Equal(t, 1, f.downloads)
}

func TestSearch_PreviewDeduplicatesClientAliases(t *testing.T) {
	f := newSearchFixture(t, 1, 1, 0.75)
	cfg := f.config.Snapshot()
	alias := cloneClientConfig(*cfg.Clients["default"])
	alias.Import.Category = "other-category"
	cfg.Clients["alias"] = &alias
	f.config.Store(cfg)
	clientMap.Store("alias", cachedTorrentClient{config: alias, client: f.torrentClient})
	report := f.runExact(t, true)
	accepted := 0
	for _, outcome := range report.Outcomes {
		if outcome.Status == "would_import" {
			accepted++
		}
	}
	require.Equal(t, 1, accepted)
	require.Equal(t, 2, report.Requests)
}

func TestSearch_HonorsRateLimitAcrossRuns(t *testing.T) {
	f := newSearchFixture(t, 1, 1, 0.75)
	f.failFirst = true
	first := f.runExact(t, true)
	require.Len(t, first.Failures, 1)
	f.queries = nil
	second := f.runExact(t, true)
	require.Len(t, f.queries, 1, "rate-limited tracker must not receive another search")
	require.Len(t, second.Failures, 1)
	require.Contains(t, second.Failures[0].Reason, "Retry-After")
	require.Equal(t, "would_import", second.Outcomes[0].Status)
}

func TestSearch_RejectsMalformedRequests(t *testing.T) {
	f := newSearchFixture(t, 1, 1, 0.75)
	for _, body := range []string{`{"verify":true}`, "null", `{"unknown":true}`, `{} {}`, `{"dryRun":true} trailing`} {
		response := f.postRaw(t, "/api/search", []byte(body), processorTestToken)
		require.Equal(t, 400, response.Code)
	}
	require.Empty(t, f.queries)
}

func TestSearch_QueryOmitsYearButMatchingRespectsSettings(t *testing.T) {
	for _, skipYear := range []bool{false, true} {
		t.Run(fmt.Sprintf("skip year %t", skipYear), func(t *testing.T) {
			f := newSearchFixture(t, 1, 1, 0.75)
			cfg := f.config.Snapshot()
			cfg.Search.IndexerIDs = []int{1}
			cfg.FuzzyMatching.SkipYearCompare = skipYear
			f.config.Store(cfg)
			f.torrentClient.torrents[0].Name = "Lifecycle.2024.S01E01.1080p.WEB-DL.H.264-RlsGrp"
			f.torrentClient.torrents = append(f.torrentClient.torrents, torrentclient.Torrent{
				Name: "Lifecycle.2025.S01E01.1080p.WEB-DL.H.264-RlsGrp", Hash: "year2025",
			})
			f.titles = []string{
				"Lifecycle.2024.S01.1080p.WEB-DL.H.264-RlsGrp",
				"Lifecycle.2025.S01.1080p.WEB-DL.H.264-RlsGrp",
				"Lifecycle.2023.S01.1080p.WEB-DL.H.264-RlsGrp",
			}
			response := f.postJSON(t, "/api/search", SearchRequest{DryRun: true})
			require.Equal(t, 200, response.Code, response.Body.String())
			var report searchReport
			require.NoError(t, json.Unmarshal(response.Body.Bytes(), &report))
			require.Equal(t, []string{"Lifecycle S01"}, f.queries)
			require.Equal(t, 1, report.Groups)
			require.Len(t, report.Outcomes, 3)
			require.Equal(t, "candidate", report.Outcomes[0].Status)
			require.Equal(t, "candidate", report.Outcomes[1].Status)
			if skipYear {
				require.Equal(t, "candidate", report.Outcomes[2].Status)
			} else {
				require.Equal(t, "rejected", report.Outcomes[2].Status)
			}
			require.Zero(t, f.downloads)
		})
	}
}

func TestSearch_DryRunDoesNotRetrieveFilesOrMetadata(t *testing.T) {
	f := newSearchFixture(t, 10, 1, 0.75)
	// Even an inaccessible source must not turn discovery into exact verification.
	require.NoError(t, os.Rename(f.sourceDir, f.sourceDir+"-moved"))
	response := f.postJSON(t, "/api/search", map[string]any{"dryRun": true})
	require.Equal(t, 200, response.Code)
	var report searchReport
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &report))
	require.Len(t, report.Outcomes, 2)
	for _, outcome := range report.Outcomes {
		require.Equal(t, "candidate", outcome.Status)
		require.Nil(t, outcome.ReusableEpisodes)
		require.Nil(t, outcome.TotalEpisodes)
	}
	require.Zero(t, f.downloads)
	require.Zero(t, report.TorrentDownloads)
	require.Zero(t, f.torrentClient.fileBatchCalls)
	require.Zero(t, f.torrentClient.importCalls)
}

func TestSearch_IndexerAllowlist(t *testing.T) {
	for _, tt := range []struct {
		name                       string
		ids                        []int
		wantID, requests, failures int
	}{
		{"selected", []int{2}, 2, 1, 0},
		{"missing", []int{99}, 0, 0, 2},
		{"partial", []int{2, 99}, 2, 1, 1},
	} {
		t.Run(tt.name, func(t *testing.T) {
			f := newSearchFixture(t, 1, 1, 0.75)
			cfg := f.config.Snapshot()
			cfg.Search.IndexerIDs = tt.ids
			f.config.Store(cfg)
			report := f.runExact(t, true)
			require.Equal(t, tt.requests, report.Requests)
			require.Len(t, report.Failures, tt.failures)
			require.Len(t, report.Outcomes, tt.requests)
			for _, outcome := range report.Outcomes {
				require.Equal(t, tt.wantID, outcome.IndexerID)
			}
		})
	}
}

func TestSearch_PreflightStopsBeforeMetadata(t *testing.T) {
	for _, mode := range []string{"missing", "empty", "wrong size", "directory", "client error"} {
		t.Run(mode, func(t *testing.T) {
			f := newSearchFixture(t, 1, 1, 0.75)
			path := filepath.Join(f.sourceDir, f.torrentClient.filesByHash["ep1"][0].Name)
			switch mode {
			case "missing":
				require.NoError(t, os.Rename(path, path+"-moved"))
			case "empty":
				require.NoError(t, os.WriteFile(path, nil, 0o644))
			case "wrong size":
				require.NoError(t, os.WriteFile(path, []byte("wrong"), 0o644))
			case "directory":
				require.NoError(t, os.Rename(path, path+"-moved"))
				require.NoError(t, os.Mkdir(path, 0o755))
			case "client error":
				f.torrentClient.filesErr = fmt.Errorf("client failed")
			}
			report := f.runExact(t, true)
			require.Zero(t, f.downloads)
			require.Zero(t, f.torrentClient.importCalls)
			require.Nil(t, report.Outcomes[0].TotalEpisodes)
			if mode == "client error" {
				require.Equal(t, "failed", report.Outcomes[0].Status)
			} else {
				require.Equal(t, "rejected", report.Outcomes[0].Status)
				require.Contains(t, report.Outcomes[0].Reason, "no accessible episode files")
			}
		})
	}
}

func TestSearch_MetadataReuseRechecksSourcesAndSettings(t *testing.T) {
	f := newSearchFixture(t, 2, 2, 0.75)
	cfg := f.config.Snapshot()
	cfg.Search.IndexerIDs = []int{1}
	f.config.Store(cfg)
	path := filepath.Join(f.sourceDir, f.torrentClient.filesByHash["ep2"][0].Name)
	require.NoError(t, os.Rename(path, path+"-moved"))
	first := f.runExact(t, true)
	require.Equal(t, "rejected", first.Outcomes[0].Status)
	require.Equal(t, new(1), first.Outcomes[0].ReusableEpisodes)
	require.Equal(t, 1, first.TorrentDownloads)
	require.Equal(t, 1, f.torrentClient.fileBatchCalls)
	// Lowering the threshold must re-evaluate cached metadata.
	cfg.SmartModeThreshold = 0.5
	f.config.Store(cfg)
	second := f.runExact(t, true)
	require.Equal(t, "would_import", second.Outcomes[0].Status)
	require.Equal(t, 1, second.TorrentCacheHits)
	require.Zero(t, second.TorrentDownloads)
	// Restoring a source changes coverage without another torrent download.
	require.NoError(t, os.Rename(path+"-moved", path))
	cfg.SmartModeThreshold = 1
	f.config.Store(cfg)
	third := f.runExact(t, true)
	require.Equal(t, "would_import", third.Outcomes[0].Status)
	require.Equal(t, new(2), third.Outcomes[0].ReusableEpisodes)
	require.Equal(t, 1, third.TorrentCacheHits)
	require.Equal(t, 1, f.downloads)
	// A removed source is checked even when metadata is cached.
	require.NoError(t, os.Rename(f.sourceDir, f.sourceDir+"-moved"))
	fourth := f.runExact(t, true)
	require.Equal(t, "rejected", fourth.Outcomes[0].Status)
	require.Zero(t, fourth.TorrentCacheHits)
	require.Equal(t, 1, f.downloads)
}

func TestSearch_MetadataConnectionChangeAndInvalidResponse(t *testing.T) {
	for _, invalid := range []bool{false, true} {
		t.Run(fmt.Sprint(invalid), func(t *testing.T) {
			f := newSearchFixture(t, 1, 1, 0.75)
			cfg := f.config.Snapshot()
			cfg.Search.IndexerIDs = []int{1}
			f.config.Store(cfg)
			if invalid {
				f.torrentByTitle = map[string][]byte{f.releaseName: []byte("invalid torrent")}
			}
			f.runExact(t, true)
			if !invalid {
				cfg.Search.ProwlarrURL += "/"
				f.config.Store(cfg)
			}
			second := f.runExact(t, true)
			require.Zero(t, second.TorrentCacheHits)
			require.Equal(t, 2, f.downloads)
		})
	}
}

func TestSearch_ExistingPackSkipsQueriesInEveryMode(t *testing.T) {
	for _, req := range []SearchRequest{{DryRun: true}, {DryRun: true, Verify: true}, {}} {
		t.Run(fmt.Sprintf("dry=%t verify=%t", req.DryRun, req.Verify), func(t *testing.T) {
			f := newSearchFixture(t, 1, 1, 0.75)
			f.torrentClient.torrents = append(f.torrentClient.torrents, torrentclient.Torrent{Name: f.releaseName, Hash: "pack"})
			response := f.postJSON(t, "/api/search", req)
			require.Equal(t, 200, response.Code, response.Body.String())
			var report searchReport
			require.NoError(t, json.Unmarshal(response.Body.Bytes(), &report))
			require.Zero(t, report.Groups)
			require.Equal(t, 1, report.EpisodeTorrents)
			require.Equal(t, 1, report.CoveredEpisodeTorrents)
			require.Zero(t, report.Requests)
			require.Empty(t, report.Outcomes)
			require.Empty(t, report.Failures)
			require.Zero(t, f.discoveryCalls)
			require.Empty(t, f.queries)
			require.Zero(t, f.downloads)
			require.Zero(t, f.torrentClient.fileBatchCalls)
			require.Zero(t, f.torrentClient.importCalls)
		})
	}
}

func TestSearch_ExistingPackRespectsVariantAndFuzzySettings(t *testing.T) {
	for _, tt := range []struct {
		name, pack string
		fuzzy      domain.FuzzyMatching
		covered    bool
	}{
		{name: "same variant", pack: "Lifecycle.S01.1080p.WEB-DL.H.264-RlsGrp", covered: true},
		{name: "other group", pack: "Lifecycle.S01.1080p.WEB-DL.H.264-OtherGrp"},
		{name: "other resolution", pack: "Lifecycle.S01.2160p.WEB-DL.H.265-RlsGrp"},
		{name: "other source", pack: "Lifecycle.S01.1080p.BluRay.H.264-RlsGrp"},
		{name: "other HDR", pack: "Lifecycle.S01.1080p.WEB-DL.HDR.H.264-RlsGrp"},
		{name: "other service", pack: "Lifecycle.S01.1080p.AMZN.WEB-DL.H.264-RlsGrp"},
		{name: "other edition", pack: "Lifecycle.S01.Extended.1080p.WEB-DL.H.264-RlsGrp"},
		{name: "other season", pack: "Lifecycle.S02.1080p.WEB-DL.H.264-RlsGrp"},
		{name: "other title", pack: "Different.S01.1080p.WEB-DL.H.264-RlsGrp"},
		{name: "other year", pack: "Lifecycle.2024.S01.1080p.WEB-DL.H.264-RlsGrp"},
		{name: "ignore year", pack: "Lifecycle.2024.S01.1080p.WEB-DL.H.264-RlsGrp", fuzzy: domain.FuzzyMatching{SkipYearCompare: true}, covered: true},
		{name: "other repack", pack: "Lifecycle.S01.REPACK.1080p.WEB-DL.H.264-RlsGrp"},
		{name: "ignore repack", pack: "Lifecycle.S01.REPACK.1080p.WEB-DL.H.264-RlsGrp", fuzzy: domain.FuzzyMatching{SkipRepackCompare: true}, covered: true},
		{name: "strict WEB", pack: "Lifecycle.S01.1080p.WEB.H.264-RlsGrp"},
		{name: "simplify WEB", pack: "Lifecycle.S01.1080p.WEB.H.264-RlsGrp", fuzzy: domain.FuzzyMatching{SimplifyWebCompare: true}, covered: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			f := newSearchFixture(t, 1, 1, 0.75)
			cfg := f.config.Snapshot()
			cfg.FuzzyMatching = tt.fuzzy
			f.config.Store(cfg)
			f.torrentClient.torrents = append(f.torrentClient.torrents, torrentclient.Torrent{Name: tt.pack, Hash: "pack"})
			response := f.postJSON(t, "/api/search", SearchRequest{DryRun: true})
			require.Equal(t, 200, response.Code, response.Body.String())
			var report searchReport
			require.NoError(t, json.Unmarshal(response.Body.Bytes(), &report))
			if tt.covered {
				require.Equal(t, 1, report.CoveredEpisodeTorrents)
				require.Zero(t, report.Requests)
			} else {
				require.Zero(t, report.CoveredEpisodeTorrents)
				require.Equal(t, 2, report.Requests)
			}
			require.Zero(t, f.torrentClient.fileBatchCalls)
			require.Zero(t, f.downloads)
		})
	}
}

func TestSearch_ExistingPackKeepsUncoveredVariantInSameSeason(t *testing.T) {
	f := newSearchFixture(t, 1, 1, 0.75)
	otherPack := "Lifecycle.S01.2160p.WEB-DL.H.265-OtherGrp"
	otherEpisode := "Lifecycle.S01E01.2160p.WEB-DL.H.265-OtherGrp"
	f.titles = append(f.titles, otherPack)
	f.torrentClient.torrents = append(f.torrentClient.torrents,
		torrentclient.Torrent{Name: f.releaseName, Hash: "pack"},
		torrentclient.Torrent{Name: otherEpisode, Hash: "other", SavePath: f.sourceDir},
	)
	writeEpisode(t, filepath.Join(f.sourceDir, otherEpisode+".mkv"))
	f.torrentClient.filesByHash["other"] = []torrentclient.File{{Name: otherEpisode + ".mkv", Size: 1}}
	data, err := torrents.TorrentFromRls(otherPack, 1)
	require.NoError(t, err)
	f.torrentByTitle = map[string][]byte{otherPack: data}
	report := f.runExact(t, true)
	require.Equal(t, 1, report.Groups)
	require.Equal(t, 2, report.Requests)
	require.Equal(t, 1, f.downloads)
	require.Equal(t, "rejected", report.Outcomes[0].Status)
	require.Equal(t, domain.StatusAlreadyInClient.String(), report.Outcomes[0].Reason)
	require.Equal(t, "would_import", report.Outcomes[1].Status)
	require.Equal(t, otherPack, report.Outcomes[1].Title)
}

func TestSearch_ExistingPackDoesNotSuppressIndependentClient(t *testing.T) {
	f := newSearchFixture(t, 1, 1, 0.75)
	other := &fakeTorrentClient{torrents: []torrentclient.Torrent{f.torrentClient.torrents[0]}}
	f.torrentClient.torrents = append(f.torrentClient.torrents, torrentclient.Torrent{Name: f.releaseName, Hash: "pack"})
	cfg := f.config.Snapshot()
	otherCfg := cloneClientConfig(*cfg.Clients["default"])
	otherCfg.Host = "http://independent:8080"
	cfg.Clients["other"] = &otherCfg
	f.config.Store(cfg)
	clientMap.Store("other", cachedTorrentClient{config: otherCfg, client: other})
	response := f.postJSON(t, "/api/search", SearchRequest{DryRun: true})
	require.Equal(t, 200, response.Code, response.Body.String())
	var report searchReport
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &report))
	require.Equal(t, 1, report.Groups)
	require.Equal(t, 2, report.Requests)
	require.Len(t, report.Outcomes, 2)
	for _, outcome := range report.Outcomes {
		require.Equal(t, "other", outcome.ClientName)
		require.Equal(t, "candidate", outcome.Status)
	}
	require.Zero(t, f.torrentClient.fileBatchCalls)
	require.Zero(t, other.fileBatchCalls)
}

func TestSearch_RemovingExistingPackRestoresQuery(t *testing.T) {
	f := newSearchFixture(t, 1, 1, 0.75)
	f.torrentClient.torrents = append(f.torrentClient.torrents, torrentclient.Torrent{Name: f.releaseName, Hash: "pack"})
	first := f.runExact(t, true)
	require.Zero(t, first.Requests)
	f.torrentClient.torrents = f.torrentClient.torrents[:1]
	second := f.runExact(t, true)
	require.Equal(t, 2, second.Requests)
	require.Equal(t, "would_import", second.Outcomes[0].Status)
}
