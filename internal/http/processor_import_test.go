// Copyright (c) 2023 - 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package http

import (
	"bytes"
	"context"
	"encoding/base64"
	"os"
	"path/filepath"
	"testing"

	"github.com/nuxencs/seasonpackarr/internal/domain"
	"github.com/nuxencs/seasonpackarr/internal/logger"
	"github.com/nuxencs/seasonpackarr/internal/torrentclient"
	"github.com/nuxencs/seasonpackarr/internal/torrents"

	"github.com/autobrr/go-torrent/bencode"
	"github.com/autobrr/go-torrent/metainfo"
	"github.com/stretchr/testify/require"
)

func TestImportSeasonPack_ReusesAcceptedPlanWithoutClientReads(t *testing.T) {
	resetProcessorGlobals()

	tempDir := t.TempDir()
	sourceDir := filepath.Join(tempDir, "source")
	importDir := filepath.Join(tempDir, "import")
	require.NoError(t, os.MkdirAll(sourceDir, 0o755))
	require.NoError(t, os.MkdirAll(importDir, 0o755))

	const releaseName = "PlanReuse.S01.1080p.WEB-DL.H.264-RlsGrp"
	torrentBytes, err := torrents.TorrentFromRls(releaseName, 2)
	require.NoError(t, err)
	encodedTorrent := []byte(base64.StdEncoding.EncodeToString(torrentBytes))

	ep1 := "PlanReuse.S01E01.1080p.WEB-DL.H.264-RlsGrp.mkv"
	ep2 := "PlanReuse.S01E02.1080p.WEB-DL.H.264-RlsGrp.mkv"
	writeEpisode(t, filepath.Join(sourceDir, ep1))
	writeEpisode(t, filepath.Join(sourceDir, ep2))

	torrentClient := &fakeTorrentClient{
		torrents: []torrentclient.Torrent{
			{Name: "PlanReuse.S01E01.1080p.WEB-DL.H.264-RlsGrp", Hash: "ep1", SavePath: sourceDir},
			{Name: "PlanReuse.S01E02.1080p.WEB-DL.H.264-RlsGrp", Hash: "ep2", SavePath: sourceDir},
		},
		filesByHash: map[string][]torrentclient.File{
			"ep1": {{Name: ep1, Size: 1}},
			"ep2": {{Name: ep2, Size: 1}},
		},
		importRoot: importDir,
	}
	cfg := &fakeConfig{config: domain.Config{
		Clients: map[string]*domain.Client{
			"default": {Type: "qbittorrent", Import: domain.ImportPolicy{Category: "tv-hd"}},
		},
		SmartMode:          true,
		SmartModeThreshold: 0.75,
	}}
	matchProcessor := newProcessor(logger.New(&domain.Config{LogLevel: "ERROR", Version: "test"}), cfg, nil, nil)
	matchProcessor.req = &request{Name: releaseName, Torrent: encodedTorrent, Client: torrentClient, ClientName: "default"}

	statusCode, err := matchProcessor.matchSeasonPack(t.Context())
	require.NoError(t, err)
	require.Equal(t, domain.StatusSuccessfulMatch, statusCode)
	require.Equal(t, 1, torrentClient.torrentCalls)
	require.Equal(t, 2, torrentClient.fileCalls)

	importProcessor := newProcessor(logger.New(&domain.Config{LogLevel: "ERROR", Version: "test"}), cfg, nil, nil)
	importProcessor.req = &request{Name: releaseName, Torrent: encodedTorrent, Client: torrentClient, ClientName: "default"}
	statusCode, err = importProcessor.importSeasonPack(t.Context())
	require.NoError(t, err)
	require.Equal(t, domain.StatusSuccessfulHardlink, statusCode)
	require.Equal(t, 1, torrentClient.torrentCalls, "import must reuse the accepted inventory")
	require.Equal(t, 2, torrentClient.fileCalls, "import must reuse the accepted import plan")
	require.FileExists(t, filepath.Join(importDir, releaseName, ep1))
	require.FileExists(t, filepath.Join(importDir, releaseName, ep2))
}

// TestImportSeasonPack_ImportsAndPassesResolvedRoot verifies the /api/import flow
// resolves the import root, hardlinks the matched episodes under it, and hands
// the decoded torrent + info hash + resolved root to the client's Import.
func TestImportSeasonPack_ImportsAndPassesResolvedRoot(t *testing.T) {
	resetProcessorGlobals()

	tempDir := t.TempDir()
	sourceDir := filepath.Join(tempDir, "source")
	importDir := filepath.Join(tempDir, "import")
	require.NoError(t, os.MkdirAll(sourceDir, 0o755))
	require.NoError(t, os.MkdirAll(importDir, 0o755))

	releaseName := "ParseImport.S01.1080p.WEB-DL.H.264-RlsGrp"
	torrentBytes, err := torrents.TorrentFromRls(releaseName, 2)
	require.NoError(t, err)
	infoHashes, err := torrents.InfoHashes(torrentBytes)
	require.NoError(t, err)

	ep1 := "ParseImport.S01E01.1080p.WEB-DL.H.264-RlsGrp.mkv"
	ep2 := "ParseImport.S01E02.1080p.WEB-DL.H.264-RlsGrp.mkv"
	writeEpisode(t, filepath.Join(sourceDir, ep1))
	writeEpisode(t, filepath.Join(sourceDir, ep2))

	torrentClient := &fakeTorrentClient{
		torrents: []torrentclient.Torrent{
			{Name: "ParseImport.S01E01.1080p.WEB-DL.H.264-RlsGrp", Hash: "ep1", SavePath: sourceDir},
			{Name: "ParseImport.S01E02.1080p.WEB-DL.H.264-RlsGrp", Hash: "ep2", SavePath: sourceDir},
		},
		filesByHash: map[string][]torrentclient.File{
			"ep1": {{Name: ep1, Size: 1}},
			"ep2": {{Name: ep2, Size: 1}},
		},
		importRoot: importDir,
	}

	p := newImportProcessor()
	p.req = &request{
		Name:       releaseName,
		Torrent:    []byte(base64.StdEncoding.EncodeToString(torrentBytes)),
		Client:     torrentClient,
		ClientName: "default",
	}

	statusCode, err := p.importSeasonPack(t.Context())
	require.NoError(t, err)
	require.Equal(t, domain.StatusSuccessfulHardlink, statusCode)

	require.True(t, torrentClient.importCalled)
	require.Equal(t, infoHashes.Legacy, torrentClient.importReq.LegacyHash)
	require.Equal(t, infoHashes.V2, torrentClient.importReq.V2Hash)
	require.Equal(t, infoHashes.HasV1, torrentClient.importReq.HasV1)
	require.Equal(t, importDir, torrentClient.importReq.SavePath)
	require.NotEmpty(t, torrentClient.importReq.TorrentBytes)
	require.True(t, torrentClient.importReq.DataComplete, "every torrent file is hardlinked with the expected size")

	require.FileExists(t, filepath.Join(importDir, releaseName, ep1))
	require.FileExists(t, filepath.Join(importDir, releaseName, ep2))
}

// TestImportSeasonPack_ReportsPartialPackData guards the client hash-check
// decision: a missing torrent file must never let a client skip its check.
func TestImportSeasonPack_ReportsPartialPackData(t *testing.T) {
	resetProcessorGlobals()

	tempDir := t.TempDir()
	sourceDir := filepath.Join(tempDir, "source")
	importDir := filepath.Join(tempDir, "import")
	require.NoError(t, os.MkdirAll(sourceDir, 0o755))
	require.NoError(t, os.MkdirAll(importDir, 0o755))

	releaseName := "PartialImport.S01.1080p.WEB-DL.H.264-RlsGrp"
	torrentBytes, err := torrents.TorrentFromRls(releaseName, 2)
	require.NoError(t, err)

	ep1 := "PartialImport.S01E01.1080p.WEB-DL.H.264-RlsGrp.mkv"
	writeEpisode(t, filepath.Join(sourceDir, ep1))

	torrentClient := &fakeTorrentClient{
		torrents: []torrentclient.Torrent{
			{Name: "PartialImport.S01E01.1080p.WEB-DL.H.264-RlsGrp", Hash: "ep1", SavePath: sourceDir},
		},
		filesByHash: map[string][]torrentclient.File{
			"ep1": {{Name: ep1, Size: 1}},
		},
		importRoot: importDir,
	}

	p := newImportProcessor()
	p.req = &request{
		Name:       releaseName,
		Torrent:    []byte(base64.StdEncoding.EncodeToString(torrentBytes)),
		Client:     torrentClient,
		ClientName: "default",
	}

	statusCode, err := p.importSeasonPack(t.Context())
	require.NoError(t, err)
	require.Equal(t, domain.StatusSuccessfulHardlink, statusCode)
	require.True(t, torrentClient.importCalled)
	require.False(t, torrentClient.importReq.DataComplete)
}

// TestImportSeasonPack_ClientImportIgnoresCallerCancel guards the production
// failure where autobrr's request timeout cancelled a started client import and
// left the torrent stopped.
func TestImportSeasonPack_ClientImportIgnoresCallerCancel(t *testing.T) {
	resetProcessorGlobals()

	tempDir := t.TempDir()
	sourceDir := filepath.Join(tempDir, "source")
	importDir := filepath.Join(tempDir, "import")
	require.NoError(t, os.MkdirAll(sourceDir, 0o755))
	require.NoError(t, os.MkdirAll(importDir, 0o755))

	releaseName := "CancelImport.S01.1080p.WEB-DL.H.264-RlsGrp"
	torrentBytes, err := torrents.TorrentFromRls(releaseName, 1)
	require.NoError(t, err)

	ep1 := "CancelImport.S01E01.1080p.WEB-DL.H.264-RlsGrp.mkv"
	writeEpisode(t, filepath.Join(sourceDir, ep1))

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	torrentClient := &fakeTorrentClient{
		torrents: []torrentclient.Torrent{
			{Name: "CancelImport.S01E01.1080p.WEB-DL.H.264-RlsGrp", Hash: "ep1", SavePath: sourceDir},
		},
		filesByHash: map[string][]torrentclient.File{
			"ep1": {{Name: ep1, Size: 1}},
		},
		importRoot:          importDir,
		onImportDestination: cancel,
	}

	p := newImportProcessor()
	p.req = &request{
		Name:       releaseName,
		Torrent:    []byte(base64.StdEncoding.EncodeToString(torrentBytes)),
		Client:     torrentClient,
		ClientName: "default",
	}

	_, _ = p.importSeasonPack(ctx)
	require.True(t, torrentClient.importCalled)
	require.NoError(t, torrentClient.importCtxErr, "a caller disconnect must not cancel the client import")
}

func TestImportSeasonPack_RejectsArchivePackWithSampleVideos(t *testing.T) {
	resetProcessorGlobals()

	tempDir := t.TempDir()
	sourceDir := filepath.Join(tempDir, "source")
	importDir := filepath.Join(tempDir, "import")
	require.NoError(t, os.MkdirAll(sourceDir, 0o755))
	require.NoError(t, os.MkdirAll(importDir, 0o755))

	releaseName := "ArchivePack.S01.1080p.WEB.h264-RlsGrp"
	episodeName := "ArchivePack.S01E01.1080p.WEB.h264-RlsGrp.mkv"
	writeEpisode(t, filepath.Join(sourceDir, episodeName))

	infoBytes, err := bencode.Marshal(metainfo.Info{
		Name:        releaseName,
		PieceLength: 256 * 1024,
		Files: []metainfo.FileInfo{
			{Path: []string{"Episode 01", "episode01.rar"}, Length: 1},
			{Path: []string{"Episode 01", "Sample", "ArchivePack.S01E01.1080p.WEB.h264-RlsGrp-SAMPLE.mkv"}, Length: 1},
		},
	})
	require.NoError(t, err)

	var torrent bytes.Buffer
	require.NoError(t, (&metainfo.MetaInfo{InfoBytes: infoBytes}).Write(&torrent))

	torrentClient := &fakeTorrentClient{
		torrents: []torrentclient.Torrent{
			{Name: "ArchivePack.S01E01.1080p.WEB.h264-RlsGrp", Hash: "ep1", SavePath: sourceDir},
		},
		filesByHash: map[string][]torrentclient.File{
			"ep1": {{Name: episodeName, Size: 1}},
		},
		importRoot: importDir,
	}

	p := newImportProcessor()
	p.req = &request{
		Name:       releaseName,
		Torrent:    []byte(base64.StdEncoding.EncodeToString(torrent.Bytes())),
		Client:     torrentClient,
		ClientName: "default",
	}

	statusCode, err := p.importSeasonPack(t.Context())
	require.EqualError(t, err, domain.StatusFailedMatchToTorrentEps.String())
	require.Equal(t, domain.StatusFailedMatchToTorrentEps, statusCode)
	require.False(t, torrentClient.importCalled, "rejected archive pack must not be imported")
	require.NoFileExists(t, filepath.Join(importDir, releaseName, episodeName))
}

func TestImportSeasonPack_UsesFlatImportDestination(t *testing.T) {
	resetProcessorGlobals()

	tempDir := t.TempDir()
	sourceDir := filepath.Join(tempDir, "source")
	importDir := filepath.Join(tempDir, "import")
	require.NoError(t, os.MkdirAll(sourceDir, 0o755))
	require.NoError(t, os.MkdirAll(importDir, 0o755))

	releaseName := "FlatImport.S01.1080p.WEB-DL.H.264-RlsGrp"
	torrentBytes, err := torrents.TorrentFromRls(releaseName, 1)
	require.NoError(t, err)

	episodeName := "FlatImport.S01E01.1080p.WEB-DL.H.264-RlsGrp.mkv"
	writeEpisode(t, filepath.Join(sourceDir, episodeName))

	torrentClient := &fakeTorrentClient{
		torrents: []torrentclient.Torrent{
			{Name: "FlatImport.S01E01.1080p.WEB-DL.H.264-RlsGrp", Hash: "ep1", SavePath: sourceDir},
		},
		filesByHash: map[string][]torrentclient.File{
			"ep1": {{Name: episodeName, Size: 1}},
		},
		importRoot: importDir,
		flatImport: true,
	}

	p := newImportProcessor()
	p.req = &request{
		Name:       releaseName,
		Torrent:    []byte(base64.StdEncoding.EncodeToString(torrentBytes)),
		Client:     torrentClient,
		ClientName: "default",
	}

	statusCode, err := p.importSeasonPack(t.Context())
	require.NoError(t, err)
	require.Equal(t, domain.StatusSuccessfulHardlink, statusCode)
	require.FileExists(t, filepath.Join(importDir, episodeName))
	require.NoFileExists(t, filepath.Join(importDir, releaseName, episodeName))
}

func TestImportSeasonPack_RetryReusesExistingHardlinks(t *testing.T) {
	resetProcessorGlobals()

	tempDir := t.TempDir()
	sourceDir := filepath.Join(tempDir, "source")
	importDir := filepath.Join(tempDir, "import")
	require.NoError(t, os.MkdirAll(sourceDir, 0o755))
	require.NoError(t, os.MkdirAll(importDir, 0o755))

	releaseName := "RetryImport.S01.1080p.WEB-DL.H.264-RlsGrp"
	torrentBytes, err := torrents.TorrentFromRls(releaseName, 1)
	require.NoError(t, err)
	encodedTorrent := []byte(base64.StdEncoding.EncodeToString(torrentBytes))

	episodeName := "RetryImport.S01E01.1080p.WEB-DL.H.264-RlsGrp.mkv"
	writeEpisode(t, filepath.Join(sourceDir, episodeName))

	torrentClient := &fakeTorrentClient{
		torrents: []torrentclient.Torrent{
			{Name: "RetryImport.S01E01.1080p.WEB-DL.H.264-RlsGrp", Hash: "ep1", SavePath: sourceDir},
		},
		filesByHash: map[string][]torrentclient.File{
			"ep1": {{Name: episodeName, Size: 1}},
		},
		importRoot: importDir,
	}

	p := newImportProcessor()
	p.req = &request{Name: releaseName, Client: torrentClient, ClientName: "default"}

	for attempt := range 2 {
		p.req.Torrent = encodedTorrent
		torrentClient.importCalled = false

		statusCode, err := p.importSeasonPack(t.Context())
		require.NoError(t, err, "attempt %d", attempt+1)
		require.Equal(t, domain.StatusSuccessfulHardlink, statusCode)
		require.True(t, torrentClient.importCalled, "attempt %d must reach client import", attempt+1)
	}
}

// TestImportSeasonPack_SkipsCrossSeedDuplicates ensures a target is hardlinked only
// once even when multiple cross-seeded client torrents match the same episode.
func TestImportSeasonPack_SkipsCrossSeedDuplicates(t *testing.T) {
	resetProcessorGlobals()

	tempDir := t.TempDir()
	sourceDir1 := filepath.Join(tempDir, "source1")
	sourceDir2 := filepath.Join(tempDir, "source2")
	importDir := filepath.Join(tempDir, "import")
	require.NoError(t, os.MkdirAll(sourceDir1, 0o755))
	require.NoError(t, os.MkdirAll(sourceDir2, 0o755))
	require.NoError(t, os.MkdirAll(importDir, 0o755))

	releaseName := "CrossSeed.S01.1080p.WEB-DL.H.264-RlsGrp"
	torrentBytes, err := torrents.TorrentFromRls(releaseName, 2)
	require.NoError(t, err)

	ep1 := "CrossSeed.S01E01.1080p.WEB-DL.H.264-RlsGrp.mkv"
	ep2 := "CrossSeed.S01E02.1080p.WEB-DL.H.264-RlsGrp.mkv"
	for _, dir := range []string{sourceDir1, sourceDir2} {
		writeEpisode(t, filepath.Join(dir, ep1))
		writeEpisode(t, filepath.Join(dir, ep2))
	}

	torrentClient := &fakeTorrentClient{
		torrents: []torrentclient.Torrent{
			{Name: "CrossSeed.S01E01.1080p.WEB-DL.H.264-RlsGrp", Hash: "ep1a", SavePath: sourceDir1},
			{Name: "CrossSeed.S01E01.1080p.WEB-DL.H.264-RlsGrp", Hash: "ep1b", SavePath: sourceDir2},
			{Name: "CrossSeed.S01E02.1080p.WEB-DL.H.264-RlsGrp", Hash: "ep2a", SavePath: sourceDir1},
			{Name: "CrossSeed.S01E02.1080p.WEB-DL.H.264-RlsGrp", Hash: "ep2b", SavePath: sourceDir2},
		},
		filesByHash: map[string][]torrentclient.File{
			"ep1a": {{Name: ep1, Size: 1}},
			"ep1b": {{Name: ep1, Size: 1}},
			"ep2a": {{Name: ep2, Size: 1}},
			"ep2b": {{Name: ep2, Size: 1}},
		},
		importRoot: importDir,
	}

	p := newImportProcessor()
	p.req = &request{
		Name:       releaseName,
		Torrent:    []byte(base64.StdEncoding.EncodeToString(torrentBytes)),
		Client:     torrentClient,
		ClientName: "default",
	}

	statusCode, err := p.importSeasonPack(t.Context())
	require.NoError(t, err)
	require.Equal(t, domain.StatusSuccessfulHardlink, statusCode)

	require.FileExists(t, filepath.Join(importDir, releaseName, ep1))
	require.FileExists(t, filepath.Join(importDir, releaseName, ep2))
	require.True(t, torrentClient.importCalled)
}
