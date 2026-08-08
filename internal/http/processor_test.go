// Copyright (c) 2023 - 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package http

import (
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/nuxencs/seasonpackarr/internal/config"
	"github.com/nuxencs/seasonpackarr/internal/domain"
	"github.com/nuxencs/seasonpackarr/internal/logger"
	"github.com/nuxencs/seasonpackarr/internal/torrentclient"
	"github.com/nuxencs/seasonpackarr/internal/torrents"

	"github.com/puzpuzpuz/xsync/v3"
	"github.com/stretchr/testify/require"
)

// mockTorrentClient is a configurable in-memory torrentclient.TorrentClient for
// exercising the processor without a live torrent client.
type mockTorrentClient struct {
	torrents    []torrentclient.Torrent
	torrentsErr error
	files       []torrentclient.File            // returned for any hash when filesByHash is nil
	filesByHash map[string][]torrentclient.File // per-hash file lists
	filesErr    error
	gotHash     string

	importRoot    string
	importRootErr error
	flatImport    bool
	importErr     error
	importCalled  bool
	importReq     torrentclient.ImportRequest
}

func (m *mockTorrentClient) GetTorrents() ([]torrentclient.Torrent, error) {
	return m.torrents, m.torrentsErr
}

func (m *mockTorrentClient) GetFiles(hash string) ([]torrentclient.File, error) {
	m.gotHash = hash
	if m.filesErr != nil {
		return nil, m.filesErr
	}
	if m.filesByHash != nil {
		return m.filesByHash[hash], nil
	}
	return m.files, nil
}

func (m *mockTorrentClient) ImportDestination() (torrentclient.ImportDestination, error) {
	if m.importRootErr != nil {
		return torrentclient.ImportDestination{}, m.importRootErr
	}
	if m.flatImport {
		return torrentclient.NewFlatImportDestination(m.importRoot), nil
	}
	return torrentclient.NewRootedImportDestination(m.importRoot), nil
}

func (m *mockTorrentClient) Import(req torrentclient.ImportRequest) error {
	m.importCalled = true
	m.importReq = req
	return m.importErr
}

func newTestProcessor(client torrentclient.TorrentClient) *processor {
	return &processor{req: &request{Client: client}}
}

// TestTorrentClientPathContract locks in the load-bearing contract documented on
// torrentclient.TorrentClient: the hardlink source the processor uses is
// filepath.Join(Torrent.SavePath, <file name from GetFiles>). File names keep
// their torrent-root-folder prefix and SavePath is the absolute on-disk download
// dir, so the join is the real file path. A future TorrentClient implementation
// that returns a bare basename or a non-absolute SavePath would break hardlinking
// silently — this test fails first.
func TestTorrentClientPathContract(t *testing.T) {
	const savePath = "/data/torrents/tv"
	mock := &mockTorrentClient{
		torrents: []torrentclient.Torrent{
			{Hash: "abc123", Name: "Some.Show.S01.1080p", SavePath: savePath},
		},
		files: []torrentclient.File{
			{Name: "Some.Show.S01.1080p/Some.Show.S01E01.1080p.mkv", Size: 1_000_000},
		},
	}
	p := newTestProcessor(mock)

	ts, err := p.req.Client.GetTorrents()
	if err != nil {
		t.Fatalf("GetTorrents: %v", err)
	}
	if len(ts) != 1 {
		t.Fatalf("len(torrents) = %d, want 1", len(ts))
	}

	fileName, size, err := p.getFiles(ts[0].Hash)
	if err != nil {
		t.Fatalf("getFiles: %v", err)
	}
	if mock.gotHash != "abc123" {
		t.Errorf("GetFiles called with hash %q, want abc123", mock.gotHash)
	}
	if size != 1_000_000 {
		t.Errorf("size = %d, want 1000000", size)
	}

	gotSource := filepath.Join(ts[0].SavePath, fileName)
	const wantSource = "/data/torrents/tv/Some.Show.S01.1080p/Some.Show.S01E01.1080p.mkv"
	if gotSource != wantSource {
		t.Errorf("hardlink source = %q, want %q", gotSource, wantSource)
	}
}

func TestGetFilesSelectsFirstValidEpisode(t *testing.T) {
	mock := &mockTorrentClient{
		files: []torrentclient.File{
			{Name: "Some.Show.S01/poster.jpg", Size: 50},                  // not a video
			{Name: "Some.Show.S01/Some.Show.S01E01.mkv", Size: 1_000_000}, // first valid
			{Name: "Some.Show.S01/Some.Show.S01E02.mkv", Size: 1_100_000},
		},
	}
	p := newTestProcessor(mock)

	fileName, size, err := p.getFiles("h")
	if err != nil {
		t.Fatalf("getFiles: %v", err)
	}
	if fileName != "Some.Show.S01/Some.Show.S01E01.mkv" {
		t.Errorf("fileName = %q, want Some.Show.S01/Some.Show.S01E01.mkv", fileName)
	}
	if size != 1_000_000 {
		t.Errorf("size = %d, want 1000000", size)
	}
}

func TestGetFilesErrorsWhenNoValidEpisode(t *testing.T) {
	mock := &mockTorrentClient{
		files: []torrentclient.File{
			{Name: "Some.Show.S01/poster.jpg", Size: 50},
			{Name: "Some.Show.S01/readme.txt", Size: 10},
		},
	}
	p := newTestProcessor(mock)

	if _, _, err := p.getFiles("h"); err == nil {
		t.Fatal("expected error when no valid episode file is present, got nil")
	}
}

func TestGetFilesPropagatesClientError(t *testing.T) {
	errBoom := errors.New("boom")
	p := newTestProcessor(&mockTorrentClient{filesErr: errBoom})

	if _, _, err := p.getFiles("h"); !errors.Is(err, errBoom) {
		t.Fatalf("getFiles error = %v, want it to wrap errBoom", err)
	}
}

func resetProcessorGlobals() {
	clientMap = xsync.NewMapOf[string, torrentclient.TorrentClient]()
	entryMap = xsync.NewMapOf[string, *entryCache]()
}

func newImportProcessor() *processor {
	cfg := &config.AppConfig{Config: &domain.Config{
		Clients: map[string]*domain.Client{
			"default": {Type: "qbittorrent", Import: domain.ImportPolicy{Category: "tv-hd"}},
		},
	}}
	return newProcessor(logger.New(&domain.Config{LogLevel: "ERROR", Version: "test"}), cfg, nil, nil)
}

func writeEpisode(t *testing.T, path string) {
	t.Helper()
	require.NoError(t, os.WriteFile(path, []byte("0"), 0o644))
}

// TestProcessSeasonPackIsGateOnly locks in that /api/pack is a pure match gate:
// it reports a successful match without importing or hardlinking anything.
func TestProcessSeasonPackIsGateOnly(t *testing.T) {
	resetProcessorGlobals()

	tempDir := t.TempDir()
	sourceDir := filepath.Join(tempDir, "source")
	importDir := filepath.Join(tempDir, "import")
	require.NoError(t, os.MkdirAll(sourceDir, 0o755))
	require.NoError(t, os.MkdirAll(importDir, 0o755))

	epName := "Series.S01E01.1080p.WEB-DL.H.264-RlsGrp.mkv"
	writeEpisode(t, filepath.Join(sourceDir, epName))

	mock := &mockTorrentClient{
		torrents: []torrentclient.Torrent{
			{Name: "Series.S01E01.1080p.WEB-DL.H.264-RlsGrp", Hash: "ep1", SavePath: sourceDir},
		},
		filesByHash: map[string][]torrentclient.File{
			"ep1": {{Name: epName, Size: 1}},
		},
		importRoot: importDir,
	}

	p := newImportProcessor()
	p.req = &request{Name: "Series.S01.1080p.WEB-DL.H.264-RlsGrp", Client: mock, ClientName: "default"}

	statusCode, err := p.processSeasonPack()
	require.NoError(t, err)
	require.Equal(t, domain.StatusSuccessfulMatch, statusCode)
	require.False(t, mock.importCalled, "gate must not import")
	require.NoFileExists(t, filepath.Join(importDir, "Series.S01.1080p.WEB-DL.H.264-RlsGrp", epName))
}

// TestParseTorrentImportsAndPassesResolvedRoot verifies the /api/parse flow
// resolves the import root, hardlinks the matched episodes under it, and hands
// the decoded torrent + info hash + resolved root to the client's Import.
func TestParseTorrentImportsAndPassesResolvedRoot(t *testing.T) {
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

	mock := &mockTorrentClient{
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
		Client:     mock,
		ClientName: "default",
	}

	statusCode, err := p.parseTorrent()
	require.NoError(t, err)
	require.Equal(t, domain.StatusSuccessfulHardlink, statusCode)

	require.True(t, mock.importCalled)
	require.Equal(t, infoHashes.Legacy, mock.importReq.LegacyHash)
	require.Equal(t, infoHashes.V2, mock.importReq.V2Hash)
	require.Equal(t, infoHashes.HasV1, mock.importReq.HasV1)
	require.Equal(t, importDir, mock.importReq.SavePath)
	require.NotEmpty(t, mock.importReq.TorrentBytes)

	require.FileExists(t, filepath.Join(importDir, releaseName, ep1))
	require.FileExists(t, filepath.Join(importDir, releaseName, ep2))
}

func TestParseTorrentUsesFlatImportDestination(t *testing.T) {
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

	mock := &mockTorrentClient{
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
		Client:     mock,
		ClientName: "default",
	}

	statusCode, err := p.parseTorrent()
	require.NoError(t, err)
	require.Equal(t, domain.StatusSuccessfulHardlink, statusCode)
	require.FileExists(t, filepath.Join(importDir, episodeName))
	require.NoFileExists(t, filepath.Join(importDir, releaseName, episodeName))
}

func TestParseTorrentRetryReusesExistingHardlinks(t *testing.T) {
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

	mock := &mockTorrentClient{
		torrents: []torrentclient.Torrent{
			{Name: "RetryImport.S01E01.1080p.WEB-DL.H.264-RlsGrp", Hash: "ep1", SavePath: sourceDir},
		},
		filesByHash: map[string][]torrentclient.File{
			"ep1": {{Name: episodeName, Size: 1}},
		},
		importRoot: importDir,
	}

	p := newImportProcessor()
	p.req = &request{Name: releaseName, Client: mock, ClientName: "default"}

	for attempt := range 2 {
		p.req.Torrent = encodedTorrent
		mock.importCalled = false

		statusCode, err := p.parseTorrent()
		require.NoError(t, err, "attempt %d", attempt+1)
		require.Equal(t, domain.StatusSuccessfulHardlink, statusCode)
		require.True(t, mock.importCalled, "attempt %d must reach client import", attempt+1)
	}
}

// TestParseTorrentSkipsCrossSeedDuplicates ensures a target is hardlinked only
// once even when multiple cross-seeded client torrents match the same episode.
func TestParseTorrentSkipsCrossSeedDuplicates(t *testing.T) {
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

	mock := &mockTorrentClient{
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
		Client:     mock,
		ClientName: "default",
	}

	statusCode, err := p.parseTorrent()
	require.NoError(t, err)
	require.Equal(t, domain.StatusSuccessfulHardlink, statusCode)

	require.FileExists(t, filepath.Join(importDir, releaseName, ep1))
	require.FileExists(t, filepath.Join(importDir, releaseName, ep2))
	require.True(t, mock.importCalled)
}
