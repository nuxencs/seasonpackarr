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
	"github.com/nuxencs/seasonpackarr/internal/torrentclient/torrentclienttest"
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

	torrentClient := &torrentclienttest.Client{
		Torrents: []torrentclient.Torrent{
			{Name: "PlanReuse.S01E01.1080p.WEB-DL.H.264-RlsGrp", Hash: "ep1", SavePath: sourceDir},
			{Name: "PlanReuse.S01E02.1080p.WEB-DL.H.264-RlsGrp", Hash: "ep2", SavePath: sourceDir},
		},
		FilesByHash: map[string][]torrentclient.File{
			"ep1": {{Name: ep1, Size: 1}},
			"ep2": {{Name: ep2, Size: 1}},
		},
		ImportRoot: importDir,
	}
	cfg := &fakeConfig{config: domain.Config{
		Clients: map[string]*domain.Client{
			"default": {Type: "qbittorrent", Import: domain.ImportPolicy{Category: "tv-hd"}},
		},
		SmartMode:          true,
		SmartModeThreshold: 0.75,
	}}
	inv := newTestInventory(torrentClient)
	matchProcessor := newProcessor(logger.New(&domain.Config{LogLevel: "ERROR", Version: "test"}), cfg, inv, nil, nil)
	matchProcessor.req = &request{Name: releaseName, Torrent: encodedTorrent, ClientName: "default"}

	statusCode, err := matchProcessor.matchSeasonPack(t.Context())
	require.NoError(t, err)
	require.Equal(t, domain.StatusSuccessfulMatch, statusCode)
	require.Equal(t, 1, torrentClient.TorrentCalls)
	require.Equal(t, 2, torrentClient.FileCalls)

	importProcessor := newProcessor(logger.New(&domain.Config{LogLevel: "ERROR", Version: "test"}), cfg, inv, nil, nil)
	importProcessor.req = &request{Name: releaseName, Torrent: encodedTorrent, ClientName: "default"}
	statusCode, err = importProcessor.importSeasonPack(t.Context())
	require.NoError(t, err)
	require.Equal(t, domain.StatusSuccessfulHardlink, statusCode)
	require.Equal(t, 1, torrentClient.TorrentCalls, "import must reuse the accepted inventory")
	require.Equal(t, 2, torrentClient.FileCalls, "import must reuse the accepted import plan")
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

	torrentClient := &torrentclienttest.Client{
		Torrents: []torrentclient.Torrent{
			{Name: "ParseImport.S01E01.1080p.WEB-DL.H.264-RlsGrp", Hash: "ep1", SavePath: sourceDir},
			{Name: "ParseImport.S01E02.1080p.WEB-DL.H.264-RlsGrp", Hash: "ep2", SavePath: sourceDir},
		},
		FilesByHash: map[string][]torrentclient.File{
			"ep1": {{Name: ep1, Size: 1}},
			"ep2": {{Name: ep2, Size: 1}},
		},
		ImportRoot: importDir,
	}

	p := newImportProcessor(newTestInventory(torrentClient))
	p.req = &request{
		Name:       releaseName,
		Torrent:    []byte(base64.StdEncoding.EncodeToString(torrentBytes)),
		ClientName: "default",
	}

	statusCode, err := p.importSeasonPack(t.Context())
	require.NoError(t, err)
	require.Equal(t, domain.StatusSuccessfulHardlink, statusCode)

	require.True(t, torrentClient.ImportCalled)
	require.Equal(t, infoHashes.Legacy, torrentClient.ImportReq.LegacyHash)
	require.Equal(t, infoHashes.V2, torrentClient.ImportReq.V2Hash)
	require.Equal(t, infoHashes.HasV1, torrentClient.ImportReq.HasV1)
	require.Equal(t, importDir, torrentClient.ImportReq.SavePath)
	require.NotEmpty(t, torrentClient.ImportReq.TorrentBytes)
	require.True(t, torrentClient.ImportReq.DataComplete, "every torrent file is hardlinked with the expected size")

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

	torrentClient := &torrentclienttest.Client{
		Torrents: []torrentclient.Torrent{
			{Name: "PartialImport.S01E01.1080p.WEB-DL.H.264-RlsGrp", Hash: "ep1", SavePath: sourceDir},
		},
		FilesByHash: map[string][]torrentclient.File{
			"ep1": {{Name: ep1, Size: 1}},
		},
		ImportRoot: importDir,
	}

	p := newImportProcessor(newTestInventory(torrentClient))
	p.req = &request{
		Name:       releaseName,
		Torrent:    []byte(base64.StdEncoding.EncodeToString(torrentBytes)),
		ClientName: "default",
	}

	statusCode, err := p.importSeasonPack(t.Context())
	require.NoError(t, err)
	require.Equal(t, domain.StatusSuccessfulHardlink, statusCode)
	require.True(t, torrentClient.ImportCalled)
	require.False(t, torrentClient.ImportReq.DataComplete)
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
	torrentClient := &torrentclienttest.Client{
		Torrents: []torrentclient.Torrent{
			{Name: "CancelImport.S01E01.1080p.WEB-DL.H.264-RlsGrp", Hash: "ep1", SavePath: sourceDir},
		},
		FilesByHash: map[string][]torrentclient.File{
			"ep1": {{Name: ep1, Size: 1}},
		},
		ImportRoot:          importDir,
		OnImportDestination: cancel,
	}

	p := newImportProcessor(newTestInventory(torrentClient))
	p.req = &request{
		Name:       releaseName,
		Torrent:    []byte(base64.StdEncoding.EncodeToString(torrentBytes)),
		ClientName: "default",
	}

	_, _ = p.importSeasonPack(ctx)
	require.True(t, torrentClient.ImportCalled)
	require.NoError(t, torrentClient.ImportCtxErr, "a caller disconnect must not cancel the client import")
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

	torrentClient := &torrentclienttest.Client{
		Torrents: []torrentclient.Torrent{
			{Name: "ArchivePack.S01E01.1080p.WEB.h264-RlsGrp", Hash: "ep1", SavePath: sourceDir},
		},
		FilesByHash: map[string][]torrentclient.File{
			"ep1": {{Name: episodeName, Size: 1}},
		},
		ImportRoot: importDir,
	}

	p := newImportProcessor(newTestInventory(torrentClient))
	p.req = &request{
		Name:       releaseName,
		Torrent:    []byte(base64.StdEncoding.EncodeToString(torrent.Bytes())),
		ClientName: "default",
	}

	statusCode, err := p.importSeasonPack(t.Context())
	require.EqualError(t, err, domain.StatusFailedMatchToTorrentEps.String())
	require.Equal(t, domain.StatusFailedMatchToTorrentEps, statusCode)
	require.False(t, torrentClient.ImportCalled, "rejected archive pack must not be imported")
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

	torrentClient := &torrentclienttest.Client{
		Torrents: []torrentclient.Torrent{
			{Name: "FlatImport.S01E01.1080p.WEB-DL.H.264-RlsGrp", Hash: "ep1", SavePath: sourceDir},
		},
		FilesByHash: map[string][]torrentclient.File{
			"ep1": {{Name: episodeName, Size: 1}},
		},
		ImportRoot: importDir,
		FlatImport: true,
	}

	p := newImportProcessor(newTestInventory(torrentClient))
	p.req = &request{
		Name:       releaseName,
		Torrent:    []byte(base64.StdEncoding.EncodeToString(torrentBytes)),
		ClientName: "default",
	}

	statusCode, err := p.importSeasonPack(t.Context())
	require.NoError(t, err)
	require.Equal(t, domain.StatusSuccessfulHardlink, statusCode)
	require.FileExists(t, filepath.Join(importDir, episodeName))
	require.NoFileExists(t, filepath.Join(importDir, releaseName, episodeName))
}

// TestImportSeasonPack_RefusesRelativeImportRoot: a relative import root must
// not make seasonpackarr link below its own working directory (issue #351).
func TestImportSeasonPack_RefusesRelativeImportRoot(t *testing.T) {
	resetProcessorGlobals()

	tempDir := t.TempDir()
	sourceDir := filepath.Join(tempDir, "source")
	workDir := filepath.Join(tempDir, "work")
	require.NoError(t, os.MkdirAll(sourceDir, 0o755))
	require.NoError(t, os.MkdirAll(workDir, 0o755))
	t.Chdir(workDir)

	releaseName := "RelativeImport.S01.1080p.WEB-DL.H.264-RlsGrp"
	torrentBytes, err := torrents.TorrentFromRls(releaseName, 1)
	require.NoError(t, err)

	episodeName := "RelativeImport.S01E01.1080p.WEB-DL.H.264-RlsGrp.mkv"
	writeEpisode(t, filepath.Join(sourceDir, episodeName))

	torrentClient := &torrentclienttest.Client{
		Torrents: []torrentclient.Torrent{
			{Name: "RelativeImport.S01E01.1080p.WEB-DL.H.264-RlsGrp", Hash: "ep1", SavePath: sourceDir},
		},
		FilesByHash: map[string][]torrentclient.File{
			"ep1": {{Name: episodeName, Size: 1}},
		},
		ImportRoot: "import",
	}

	p := newImportProcessor(newTestInventory(torrentClient))
	p.req = &request{
		Name:       releaseName,
		Torrent:    []byte(base64.StdEncoding.EncodeToString(torrentBytes)),
		ClientName: "default",
	}

	statusCode, err := p.importSeasonPack(t.Context())
	require.EqualError(t, err, domain.StatusFailedHardlink.String())
	require.Equal(t, domain.StatusFailedHardlink, statusCode)
	require.False(t, torrentClient.ImportCalled, "the client must not get a relative save path")
	entries, err := os.ReadDir(workDir)
	require.NoError(t, err)
	require.Empty(t, entries, "nothing must appear below the working directory")
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

	torrentClient := &torrentclienttest.Client{
		Torrents: []torrentclient.Torrent{
			{Name: "RetryImport.S01E01.1080p.WEB-DL.H.264-RlsGrp", Hash: "ep1", SavePath: sourceDir},
		},
		FilesByHash: map[string][]torrentclient.File{
			"ep1": {{Name: episodeName, Size: 1}},
		},
		ImportRoot: importDir,
	}

	p := newImportProcessor(newTestInventory(torrentClient))
	p.req = &request{Name: releaseName, ClientName: "default"}

	for attempt := range 2 {
		p.req.Torrent = encodedTorrent
		torrentClient.ImportCalled = false

		statusCode, err := p.importSeasonPack(t.Context())
		require.NoError(t, err, "attempt %d", attempt+1)
		require.Equal(t, domain.StatusSuccessfulHardlink, statusCode)
		require.True(t, torrentClient.ImportCalled, "attempt %d must reach client import", attempt+1)
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

	torrentClient := &torrentclienttest.Client{
		Torrents: []torrentclient.Torrent{
			{Name: "CrossSeed.S01E01.1080p.WEB-DL.H.264-RlsGrp", Hash: "ep1a", SavePath: sourceDir1},
			{Name: "CrossSeed.S01E01.1080p.WEB-DL.H.264-RlsGrp", Hash: "ep1b", SavePath: sourceDir2},
			{Name: "CrossSeed.S01E02.1080p.WEB-DL.H.264-RlsGrp", Hash: "ep2a", SavePath: sourceDir1},
			{Name: "CrossSeed.S01E02.1080p.WEB-DL.H.264-RlsGrp", Hash: "ep2b", SavePath: sourceDir2},
		},
		FilesByHash: map[string][]torrentclient.File{
			"ep1a": {{Name: ep1, Size: 1}},
			"ep1b": {{Name: ep1, Size: 1}},
			"ep2a": {{Name: ep2, Size: 1}},
			"ep2b": {{Name: ep2, Size: 1}},
		},
		ImportRoot: importDir,
	}

	p := newImportProcessor(newTestInventory(torrentClient))
	p.req = &request{
		Name:       releaseName,
		Torrent:    []byte(base64.StdEncoding.EncodeToString(torrentBytes)),
		ClientName: "default",
	}

	statusCode, err := p.importSeasonPack(t.Context())
	require.NoError(t, err)
	require.Equal(t, domain.StatusSuccessfulHardlink, statusCode)

	require.FileExists(t, filepath.Join(importDir, releaseName, ep1))
	require.FileExists(t, filepath.Join(importDir, releaseName, ep2))
	require.True(t, torrentClient.ImportCalled)
}
