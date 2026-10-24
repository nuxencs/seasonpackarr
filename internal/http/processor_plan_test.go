// Copyright (c) 2023 - 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package http

import (
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/nuxencs/seasonpackarr/internal/domain"
	"github.com/nuxencs/seasonpackarr/internal/logger"
	"github.com/nuxencs/seasonpackarr/internal/torrentclient"
	"github.com/nuxencs/seasonpackarr/internal/torrents"

	"github.com/stretchr/testify/require"
)

func TestMatchSeasonPack_UsesTorrentEpisodeCoverage(t *testing.T) {
	resetProcessorGlobals()

	const releaseName = "Coverage.S01.1080p.WEB-DL.H.264-RlsGrp"
	torrentBytes, err := torrents.TorrentFromRls(releaseName, 12)
	require.NoError(t, err)

	torrentClient := &fakeTorrentClient{filesByHash: make(map[string][]torrentclient.File)}
	for episode := 1; episode <= 10; episode++ {
		episodeName := fmt.Sprintf("Coverage.S01E%02d.1080p.WEB-DL.H.264-RlsGrp", episode)
		hash := fmt.Sprintf("ep%d", episode)
		torrentClient.torrents = append(torrentClient.torrents, torrentclient.Torrent{Name: episodeName, Hash: hash, SavePath: "/data/tv"})
		torrentClient.filesByHash[hash] = []torrentclient.File{{Name: episodeName + ".mkv", Size: 1}}
	}

	cfg := &fakeConfig{config: domain.Config{
		Clients: map[string]*domain.Client{
			"default": {Type: "qbittorrent", Import: domain.ImportPolicy{Category: "tv-hd"}},
		},
		SmartMode:          true,
		SmartModeThreshold: 0.75,
	}}
	p := newProcessor(logger.New(&domain.Config{LogLevel: "ERROR", Version: "test"}), cfg, nil, nil)
	p.req = &request{
		Name:       releaseName,
		Torrent:    []byte(base64.StdEncoding.EncodeToString(torrentBytes)),
		Client:     torrentClient,
		ClientName: "default",
	}

	statusCode, err := p.matchSeasonPack(t.Context())
	require.NoError(t, err)
	require.Equal(t, domain.StatusSuccessfulMatch, statusCode)
	require.False(t, torrentClient.importCalled, "match evaluation must remain side-effect free")
	require.Equal(t, 1, torrentClient.fileBatchCalls, "one plan must use one bulk file read")
}

func TestMatchSeasonPack_RejectsCoverageBelowTorrentThreshold(t *testing.T) {
	resetProcessorGlobals()

	const releaseName = "BelowThreshold.S01.1080p.WEB-DL.H.264-RlsGrp"
	torrentBytes, err := torrents.TorrentFromRls(releaseName, 4)
	require.NoError(t, err)

	torrentClient := &fakeTorrentClient{filesByHash: make(map[string][]torrentclient.File)}
	for episode := 1; episode <= 2; episode++ {
		episodeName := fmt.Sprintf("BelowThreshold.S01E%02d.1080p.WEB-DL.H.264-RlsGrp", episode)
		hash := fmt.Sprintf("ep%d", episode)
		torrentClient.torrents = append(torrentClient.torrents, torrentclient.Torrent{Name: episodeName, Hash: hash, SavePath: "/data/tv"})
		torrentClient.filesByHash[hash] = []torrentclient.File{{Name: episodeName + ".mkv", Size: 1}}
	}

	cfg := &fakeConfig{config: domain.Config{
		Clients: map[string]*domain.Client{
			"default": {Type: "qbittorrent", Import: domain.ImportPolicy{Category: "tv-hd"}},
		},
		SmartMode:          true,
		SmartModeThreshold: 0.75,
	}}
	p := newProcessor(logger.New(&domain.Config{LogLevel: "ERROR", Version: "test"}), cfg, nil, nil)
	p.req = &request{
		Name:       releaseName,
		Torrent:    []byte(base64.StdEncoding.EncodeToString(torrentBytes)),
		Client:     torrentClient,
		ClientName: "default",
	}

	statusCode, err := p.matchSeasonPack(t.Context())
	require.EqualError(t, err, domain.StatusBelowThreshold.String())
	require.Equal(t, domain.StatusBelowThreshold, statusCode)
	require.False(t, torrentClient.importCalled)
}

// TestMatchSeasonPack_IsGateOnly locks in that /api/match is a pure match gate:
// it reports a successful match without importing or hardlinking anything.
func TestMatchSeasonPack_IsGateOnly(t *testing.T) {
	resetProcessorGlobals()

	tempDir := t.TempDir()
	sourceDir := filepath.Join(tempDir, "source")
	importDir := filepath.Join(tempDir, "import")
	require.NoError(t, os.MkdirAll(sourceDir, 0o755))
	require.NoError(t, os.MkdirAll(importDir, 0o755))

	epName := "Series.S01E01.1080p.WEB-DL.H.264-RlsGrp.mkv"
	writeEpisode(t, filepath.Join(sourceDir, epName))
	torrentBytes, err := torrents.TorrentFromRls("Series.S01.1080p.WEB-DL.H.264-RlsGrp", 1)
	require.NoError(t, err)

	torrentClient := &fakeTorrentClient{
		torrents: []torrentclient.Torrent{
			{Name: "Series.S01E01.1080p.WEB-DL.H.264-RlsGrp", Hash: "ep1", SavePath: sourceDir},
		},
		filesByHash: map[string][]torrentclient.File{
			"ep1": {{Name: epName, Size: 1}},
		},
		importRoot: importDir,
	}

	p := newImportProcessor()
	p.req = &request{
		Name:       "Series.S01.1080p.WEB-DL.H.264-RlsGrp",
		Torrent:    []byte(base64.StdEncoding.EncodeToString(torrentBytes)),
		Client:     torrentClient,
		ClientName: "default",
	}

	statusCode, err := p.matchSeasonPack(t.Context())
	require.NoError(t, err)
	require.Equal(t, domain.StatusSuccessfulMatch, statusCode)
	require.False(t, torrentClient.importCalled, "gate must not import")
	require.NoFileExists(t, filepath.Join(importDir, "Series.S01.1080p.WEB-DL.H.264-RlsGrp", epName))
}

func TestBuildImportPlan_CoverageCannotExceedTorrentEpisodeCount(t *testing.T) {
	resetProcessorGlobals()

	const releaseName = "CappedCoverage.S01.1080p.WEB-DL.H.264-RlsGrp"
	torrentBytes, err := torrents.TorrentFromRls(releaseName, 6)
	require.NoError(t, err)

	torrentClient := &fakeTorrentClient{filesByHash: make(map[string][]torrentclient.File)}
	for episode := 1; episode <= 10; episode++ {
		episodeName := fmt.Sprintf("CappedCoverage.S01E%02d.1080p.WEB-DL.H.264-RlsGrp", episode)
		hash := fmt.Sprintf("ep%d", episode)
		torrentClient.torrents = append(torrentClient.torrents, torrentclient.Torrent{Name: episodeName, Hash: hash, SavePath: "/data/tv"})
		torrentClient.filesByHash[hash] = []torrentclient.File{{Name: episodeName + ".mkv", Size: 1}}
	}
	clientCfg := &domain.Client{Type: "qbittorrent", Import: domain.ImportPolicy{Category: "tv-hd"}}
	cfg := &fakeConfig{config: domain.Config{Clients: map[string]*domain.Client{"default": clientCfg}}}
	p := newProcessor(logger.New(&domain.Config{LogLevel: "ERROR", Version: "test"}), cfg, nil, nil)
	p.req = &request{
		Name:       releaseName,
		Torrent:    []byte(base64.StdEncoding.EncodeToString(torrentBytes)),
		Client:     torrentClient,
		ClientName: "default",
	}

	plan, statusCode, err := p.buildImportPlan(t.Context(), "default", clientCfg, cfg.Snapshot())
	require.NoError(t, err)
	require.Equal(t, domain.StatusSuccessfulMatch, statusCode)
	require.Equal(t, 6, plan.totalEps)
	require.Len(t, plan.links, 6, "client episodes outside the torrent cannot increase coverage")
}

func TestStoreImportPlan_SweepsExpiredPlans(t *testing.T) {
	resetProcessorGlobals()

	expiredKey := importPlanKey("default", torrents.Hashes{HasV1: true, Legacy: "expired"})
	planMap.Store(expiredKey, cachedImportPlan{expiresAt: time.Now().Add(-time.Second)})
	p := newImportProcessor()
	p.req = &request{Name: "Current.S01.1080p.WEB-DL.H.264-RlsGrp"}
	p.storeImportPlan("default", domain.Client{}, domain.FuzzyMatching{}, importPlan{
		hashes: torrents.Hashes{HasV1: true, Legacy: "current"},
	})

	_, expiredExists := planMap.Load(expiredKey)
	require.False(t, expiredExists)
	require.Equal(t, 1, planMap.Size())
}

// TestEpisodeFileFromFiles_FollowsTorrentClientPathContract locks in the load-bearing contract documented on
// torrentclient.TorrentClient: the hardlink source the processor uses is
// filepath.Join(Torrent.SavePath, <file name from GetFiles>). File names keep
// their torrent-root-folder prefix and SavePath is the absolute on-disk download
// dir, so the parsed episode path is the real file path. A future TorrentClient
// implementation that returns a bare basename or a non-absolute SavePath would
// break hardlinking silently. This test fails first.
func TestEpisodeFileFromFiles_FollowsTorrentClientPathContract(t *testing.T) {
	const savePath = "/data/torrents/tv"
	torrentClient := &fakeTorrentClient{
		torrents: []torrentclient.Torrent{
			{Hash: "abc123", Name: "Some.Show.S01.1080p", SavePath: savePath},
		},
		files: []torrentclient.File{
			{Name: "Some.Show.S01.1080p/Some.Show.S01E01.1080p.mkv", Size: 1_000_000},
		},
	}
	p := newTestProcessor(torrentClient)

	ts, err := p.req.Client.GetTorrents(t.Context())
	require.NoError(t, err)
	require.Len(t, ts, 1)

	results := p.req.Client.GetFiles(t.Context(), []string{ts[0].Hash})
	require.Len(t, results, 1)
	require.NoError(t, results[0].Err)
	episode, err := episodeFileFromFiles(results[0].Files, ts[0].SavePath)
	require.NoError(t, err)
	require.Equal(t, "abc123", torrentClient.gotHash)
	require.Equal(t, "/data/torrents/tv/Some.Show.S01.1080p/Some.Show.S01E01.1080p.mkv", episode.Path(), "hardlink source")
}

func TestEpisodeFileFromFiles_SelectsFirstValidEpisode(t *testing.T) {
	torrentClient := &fakeTorrentClient{
		files: []torrentclient.File{
			{Name: "Some.Show.S01/poster.jpg", Size: 50},                  // not a video
			{Name: "Some.Show.S01/Some.Show.S01E01.mkv", Size: 1_000_000}, // first valid
			{Name: "Some.Show.S01/Some.Show.S01E02.mkv", Size: 1_100_000},
		},
	}
	episode, err := episodeFileFromFiles(torrentClient.files, "")
	require.NoError(t, err)
	require.Equal(t, "Some.Show.S01/Some.Show.S01E01.mkv", episode.Path())
}

func TestEpisodeFileFromFiles_ErrorsWhenNoValidEpisode(t *testing.T) {
	torrentClient := &fakeTorrentClient{
		files: []torrentclient.File{
			{Name: "Some.Show.S01/poster.jpg", Size: 50},
			{Name: "Some.Show.S01/readme.txt", Size: 10},
		},
	}
	_, err := episodeFileFromFiles(torrentClient.files, "")
	require.Error(t, err)
}

func TestGetEpisodeFiles_KeepsSuccessfulBulkResults(t *testing.T) {
	torrentClient := &fakeTorrentClient{
		filesByHash: map[string][]torrentclient.File{
			"one": {{Name: "Show.S01E01.1080p.WEB-DL-GROUP.mkv", Size: 100}},
		},
		fileErrByHash: map[string]error{"two": errors.New("boom")},
	}
	p := newTestProcessor(torrentClient)
	candidates := []entry{
		{torrent: torrentclient.Torrent{Hash: "one", Name: "Show.S01E01", SavePath: "/one"}},
		{torrent: torrentclient.Torrent{Hash: "two", Name: "Show.S01E02", SavePath: "/two"}},
	}

	episodes := p.getEpisodeFiles(t.Context(), candidates)
	require.Len(t, episodes, 1)
	require.Equal(t, "/one/Show.S01E01.1080p.WEB-DL-GROUP.mkv", episodes[0].Path())
	require.Equal(t, 1, torrentClient.fileBatchCalls)
	require.Equal(t, 2, torrentClient.fileCalls)
	require.Equal(t, []string{"one", "two"}, torrentClient.gotHashes)
}
