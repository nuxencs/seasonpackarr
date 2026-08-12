// Copyright (c) 2023 - 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package http

import (
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/autobrr/go-torrent/bencode"
	"github.com/autobrr/go-torrent/metainfo"
	"github.com/nuxencs/seasonpackarr/internal/domain"
	"github.com/nuxencs/seasonpackarr/internal/logger"
	"github.com/nuxencs/seasonpackarr/internal/torrentclient"
	"github.com/nuxencs/seasonpackarr/internal/torrents"

	"github.com/gin-gonic/gin"
	"github.com/puzpuzpuz/xsync/v3"
	"github.com/stretchr/testify/require"
)

// mockTorrentClient is a configurable in-memory torrentclient.TorrentClient for
// exercising the processor without a live torrent client.
type mockTorrentClient struct {
	torrents       []torrentclient.Torrent
	torrentsErr    error
	torrentCalls   int
	files          []torrentclient.File            // returned for any hash when filesByHash is nil
	filesByHash    map[string][]torrentclient.File // per-hash file lists
	filesErr       error
	fileErrByHash  map[string]error
	gotHash        string
	gotHashes      []string
	fileCalls      int
	fileBatchCalls int

	importRoot    string
	importRootErr error
	flatImport    bool
	importErr     error
	importReport  torrentclient.ImportReport
	importCalled  bool
	importCalls   int
	importReq     torrentclient.ImportRequest
}

type staticConfig struct {
	config domain.Config
}

var clientConfigsEqualSink bool

func (c staticConfig) Snapshot() domain.Config {
	return c.config
}

func (m *mockTorrentClient) GetTorrents() ([]torrentclient.Torrent, error) {
	m.torrentCalls++
	return m.torrents, m.torrentsErr
}

func (m *mockTorrentClient) GetFiles(hashes []string) []torrentclient.FileResult {
	m.fileBatchCalls++
	m.fileCalls += len(hashes)
	m.gotHashes = append([]string(nil), hashes...)
	if len(hashes) > 0 {
		m.gotHash = hashes[0]
	}

	results := make([]torrentclient.FileResult, len(hashes))
	for index, hash := range hashes {
		results[index].Hash = hash
		if m.filesErr != nil {
			results[index].Err = m.filesErr
			continue
		}
		if err := m.fileErrByHash[hash]; err != nil {
			results[index].Err = err
			continue
		}
		if m.filesByHash != nil {
			results[index].Files = m.filesByHash[hash]
			continue
		}
		results[index].Files = m.files
	}
	return results
}

func TestCandidateSeasonPackUsesTorrentSummariesOnly(t *testing.T) {
	resetProcessorGlobals()

	mock := &mockTorrentClient{
		torrents: []torrentclient.Torrent{
			{Name: "Candidate.S01E01.1080p.WEB-DL.H.264-RlsGrp", Hash: "ep1", SavePath: "/data/tv"},
		},
		filesByHash: map[string][]torrentclient.File{
			"ep1": {{Name: "Candidate.S01E01.1080p.WEB-DL.H.264-RlsGrp.mkv", Size: 1}},
		},
	}

	p := newImportProcessor()
	p.req = &request{
		Name:       "Candidate.S01.1080p.WEB-DL.H.264-RlsGrp",
		Client:     mock,
		ClientName: "default",
	}

	statusCode, err := p.candidateSeasonPack()
	require.NoError(t, err)
	require.Equal(t, domain.StatusSuccessfulMatch, statusCode)
	require.Equal(t, 1, mock.torrentCalls)
	require.Zero(t, mock.fileCalls, "candidate evaluation must not request torrent file details")
}

func TestCandidateMismatchField(t *testing.T) {
	t.Parallel()

	testCases := map[domain.StatusCode]string{
		domain.StatusResolutionMismatch:       "resolution",
		domain.StatusSourceMismatch:           "source",
		domain.StatusRlsGrpMismatch:           "release_group",
		domain.StatusCutMismatch:              "cut",
		domain.StatusEditionMismatch:          "edition",
		domain.StatusRepackStatusMismatch:     "repack_status",
		domain.StatusHdrMismatch:              "hdr",
		domain.StatusStreamingServiceMismatch: "streaming_service",
	}
	for statusCode, want := range testCases {
		require.Equal(t, want, candidateMismatchField(statusCode))
	}
}

func TestCandidateEndpointReturnsSuccessfulMatchWithoutFileReads(t *testing.T) {
	resetProcessorGlobals()
	gin.SetMode(gin.TestMode)

	clientCfg := &domain.Client{Type: "qbittorrent", Import: domain.ImportPolicy{Category: "tv-hd"}}
	mock := &mockTorrentClient{
		torrents: []torrentclient.Torrent{
			{Name: "Candidate.S01E01.1080p.WEB-DL.H.264-RlsGrp", Hash: "ep1", SavePath: "/data/tv"},
		},
	}
	clientMap.Store("default", cachedTorrentClient{config: cloneClientConfig(*clientCfg), client: mock})

	cfg := staticConfig{config: domain.Config{Clients: map[string]*domain.Client{"default": clientCfg}}}
	router := gin.New()
	newWebhookHandler(logger.New(&domain.Config{LogLevel: "ERROR", Version: "test"}), cfg, nil).Routes(router.Group("/api"))

	req := httptest.NewRequest(http.MethodPost, "/api/candidate", bytes.NewBufferString(`{"name":"Candidate.S01.1080p.WEB-DL.H.264-RlsGrp","clientname":"default"}`))
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	require.Equal(t, domain.StatusSuccessfulMatch.Code(), res.Code)
	require.Zero(t, mock.fileCalls)
}

func TestCandidateAndPackShareOneClientInventory(t *testing.T) {
	resetProcessorGlobals()
	torrentBytes, err := torrents.TorrentFromRls("Inventory.S01.1080p.WEB-DL.H.264-RlsGrp", 1)
	require.NoError(t, err)

	mock := &mockTorrentClient{
		torrents: []torrentclient.Torrent{
			{Name: "Inventory.S01E01.1080p.WEB-DL.H.264-RlsGrp", Hash: "ep1", SavePath: "/data/tv"},
		},
		filesByHash: map[string][]torrentclient.File{
			"ep1": {{Name: "Inventory.S01E01.1080p.WEB-DL.H.264-RlsGrp.mkv", Size: 1}},
		},
	}
	candidateProcessor := newImportProcessor()
	candidateProcessor.req = &request{
		Name:       "Inventory.S01.1080p.WEB-DL.H.264-RlsGrp",
		Client:     mock,
		ClientName: "default",
	}

	statusCode, err := candidateProcessor.candidateSeasonPack()
	require.NoError(t, err)
	require.Equal(t, domain.StatusSuccessfulMatch, statusCode)

	packProcessor := newImportProcessor()
	packProcessor.req = &request{
		Name:       "Inventory.S01.1080p.WEB-DL.H.264-RlsGrp",
		Torrent:    []byte(base64.StdEncoding.EncodeToString(torrentBytes)),
		Client:     mock,
		ClientName: "default",
	}
	statusCode, err = packProcessor.processSeasonPack()
	require.NoError(t, err)
	require.Equal(t, domain.StatusSuccessfulMatch, statusCode)
	require.Equal(t, 1, mock.torrentCalls, "candidate and pack must share one inventory snapshot")
}

func TestProcessSeasonPackUsesTorrentEpisodeCoverage(t *testing.T) {
	resetProcessorGlobals()

	const releaseName = "Coverage.S01.1080p.WEB-DL.H.264-RlsGrp"
	torrentBytes, err := torrents.TorrentFromRls(releaseName, 12)
	require.NoError(t, err)

	mock := &mockTorrentClient{filesByHash: make(map[string][]torrentclient.File)}
	for episode := 1; episode <= 10; episode++ {
		episodeName := fmt.Sprintf("Coverage.S01E%02d.1080p.WEB-DL.H.264-RlsGrp", episode)
		hash := fmt.Sprintf("ep%d", episode)
		mock.torrents = append(mock.torrents, torrentclient.Torrent{Name: episodeName, Hash: hash, SavePath: "/data/tv"})
		mock.filesByHash[hash] = []torrentclient.File{{Name: episodeName + ".mkv", Size: 1}}
	}

	cfg := staticConfig{config: domain.Config{
		Clients: map[string]*domain.Client{
			"default": {Type: "qbittorrent", Import: domain.ImportPolicy{Category: "tv-hd"}},
		},
		SmartMode:          true,
		SmartModeThreshold: 0.75,
	}}
	p := newProcessor(logger.New(&domain.Config{LogLevel: "ERROR", Version: "test"}), cfg, nil)
	p.req = &request{
		Name:       releaseName,
		Torrent:    []byte(base64.StdEncoding.EncodeToString(torrentBytes)),
		Client:     mock,
		ClientName: "default",
	}

	statusCode, err := p.processSeasonPack()
	require.NoError(t, err)
	require.Equal(t, domain.StatusSuccessfulMatch, statusCode)
	require.False(t, mock.importCalled, "pack evaluation must remain side-effect free")
	require.Equal(t, 1, mock.fileBatchCalls, "one plan must use one bulk file read")
}

func TestProcessSeasonPackRejectsCoverageBelowTorrentThreshold(t *testing.T) {
	resetProcessorGlobals()

	const releaseName = "BelowThreshold.S01.1080p.WEB-DL.H.264-RlsGrp"
	torrentBytes, err := torrents.TorrentFromRls(releaseName, 4)
	require.NoError(t, err)

	mock := &mockTorrentClient{filesByHash: make(map[string][]torrentclient.File)}
	for episode := 1; episode <= 2; episode++ {
		episodeName := fmt.Sprintf("BelowThreshold.S01E%02d.1080p.WEB-DL.H.264-RlsGrp", episode)
		hash := fmt.Sprintf("ep%d", episode)
		mock.torrents = append(mock.torrents, torrentclient.Torrent{Name: episodeName, Hash: hash, SavePath: "/data/tv"})
		mock.filesByHash[hash] = []torrentclient.File{{Name: episodeName + ".mkv", Size: 1}}
	}

	cfg := staticConfig{config: domain.Config{
		Clients: map[string]*domain.Client{
			"default": {Type: "qbittorrent", Import: domain.ImportPolicy{Category: "tv-hd"}},
		},
		SmartMode:          true,
		SmartModeThreshold: 0.75,
	}}
	p := newProcessor(logger.New(&domain.Config{LogLevel: "ERROR", Version: "test"}), cfg, nil)
	p.req = &request{
		Name:       releaseName,
		Torrent:    []byte(base64.StdEncoding.EncodeToString(torrentBytes)),
		Client:     mock,
		ClientName: "default",
	}

	statusCode, err := p.processSeasonPack()
	require.EqualError(t, err, domain.StatusBelowThreshold.String())
	require.Equal(t, domain.StatusBelowThreshold, statusCode)
	require.False(t, mock.importCalled)
}

func TestImportPlanCoverageCannotExceedTorrentEpisodeCount(t *testing.T) {
	resetProcessorGlobals()

	const releaseName = "CappedCoverage.S01.1080p.WEB-DL.H.264-RlsGrp"
	torrentBytes, err := torrents.TorrentFromRls(releaseName, 6)
	require.NoError(t, err)

	mock := &mockTorrentClient{filesByHash: make(map[string][]torrentclient.File)}
	for episode := 1; episode <= 10; episode++ {
		episodeName := fmt.Sprintf("CappedCoverage.S01E%02d.1080p.WEB-DL.H.264-RlsGrp", episode)
		hash := fmt.Sprintf("ep%d", episode)
		mock.torrents = append(mock.torrents, torrentclient.Torrent{Name: episodeName, Hash: hash, SavePath: "/data/tv"})
		mock.filesByHash[hash] = []torrentclient.File{{Name: episodeName + ".mkv", Size: 1}}
	}
	clientCfg := &domain.Client{Type: "qbittorrent", Import: domain.ImportPolicy{Category: "tv-hd"}}
	cfg := staticConfig{config: domain.Config{Clients: map[string]*domain.Client{"default": clientCfg}}}
	p := newProcessor(logger.New(&domain.Config{LogLevel: "ERROR", Version: "test"}), cfg, nil)
	p.req = &request{
		Name:       releaseName,
		Torrent:    []byte(base64.StdEncoding.EncodeToString(torrentBytes)),
		Client:     mock,
		ClientName: "default",
	}

	plan, statusCode, err := p.buildImportPlan("default", clientCfg, cfg.Snapshot())
	require.NoError(t, err)
	require.Equal(t, domain.StatusSuccessfulMatch, statusCode)
	require.Equal(t, 6, plan.totalEps)
	require.Len(t, plan.links, 6, "client episodes outside the torrent cannot increase coverage")
}

func TestParseTorrentReusesAcceptedPlanWithoutClientReads(t *testing.T) {
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

	mock := &mockTorrentClient{
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
	cfg := staticConfig{config: domain.Config{
		Clients: map[string]*domain.Client{
			"default": {Type: "qbittorrent", Import: domain.ImportPolicy{Category: "tv-hd"}},
		},
		SmartMode:          true,
		SmartModeThreshold: 0.75,
	}}
	packProcessor := newProcessor(logger.New(&domain.Config{LogLevel: "ERROR", Version: "test"}), cfg, nil)
	packProcessor.req = &request{Name: releaseName, Torrent: encodedTorrent, Client: mock, ClientName: "default"}

	statusCode, err := packProcessor.processSeasonPack()
	require.NoError(t, err)
	require.Equal(t, domain.StatusSuccessfulMatch, statusCode)
	require.Equal(t, 1, mock.torrentCalls)
	require.Equal(t, 2, mock.fileCalls)

	parseProcessor := newProcessor(logger.New(&domain.Config{LogLevel: "ERROR", Version: "test"}), cfg, nil)
	parseProcessor.req = &request{Name: releaseName, Torrent: encodedTorrent, Client: mock, ClientName: "default"}
	statusCode, err = parseProcessor.parseTorrent()
	require.NoError(t, err)
	require.Equal(t, domain.StatusSuccessfulHardlink, statusCode)
	require.Equal(t, 1, mock.torrentCalls, "parse must reuse the accepted inventory")
	require.Equal(t, 2, mock.fileCalls, "parse must reuse the accepted import plan")
	require.FileExists(t, filepath.Join(importDir, releaseName, ep1))
	require.FileExists(t, filepath.Join(importDir, releaseName, ep2))
}

func TestStoreImportPlanSweepsExpiredPlans(t *testing.T) {
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

func (m *mockTorrentClient) ImportDestination() (torrentclient.ImportDestination, error) {
	if m.importRootErr != nil {
		return torrentclient.ImportDestination{}, m.importRootErr
	}
	if m.flatImport {
		return torrentclient.NewFlatImportDestination(m.importRoot), nil
	}
	return torrentclient.NewRootedImportDestination(m.importRoot), nil
}

func (m *mockTorrentClient) Import(req torrentclient.ImportRequest) (torrentclient.ImportReport, error) {
	m.importCalled = true
	m.importCalls++
	m.importReq = req
	return m.importReport, m.importErr
}

func newTestProcessor(client torrentclient.TorrentClient) *processor {
	return &processor{req: &request{Client: client}}
}

// TestTorrentClientPathContract locks in the load-bearing contract documented on
// torrentclient.TorrentClient: the hardlink source the processor uses is
// filepath.Join(Torrent.SavePath, <file name from GetFiles>). File names keep
// their torrent-root-folder prefix and SavePath is the absolute on-disk download
// dir, so the parsed episode path is the real file path. A future TorrentClient
// implementation that returns a bare basename or a non-absolute SavePath would
// break hardlinking silently. This test fails first.
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

	results := p.req.Client.GetFiles([]string{ts[0].Hash})
	require.Len(t, results, 1)
	require.NoError(t, results[0].Err)
	episode, err := episodeFileFromFiles(results[0].Files, ts[0].SavePath)
	if err != nil {
		t.Fatalf("getEpisodeFile: %v", err)
	}
	if mock.gotHash != "abc123" {
		t.Errorf("GetFiles called with hash %q, want abc123", mock.gotHash)
	}
	gotSource := episode.Path()
	const wantSource = "/data/torrents/tv/Some.Show.S01.1080p/Some.Show.S01E01.1080p.mkv"
	if gotSource != wantSource {
		t.Errorf("hardlink source = %q, want %q", gotSource, wantSource)
	}
}

func TestGetEpisodeFileSelectsFirstValidEpisode(t *testing.T) {
	mock := &mockTorrentClient{
		files: []torrentclient.File{
			{Name: "Some.Show.S01/poster.jpg", Size: 50},                  // not a video
			{Name: "Some.Show.S01/Some.Show.S01E01.mkv", Size: 1_000_000}, // first valid
			{Name: "Some.Show.S01/Some.Show.S01E02.mkv", Size: 1_100_000},
		},
	}
	episode, err := episodeFileFromFiles(mock.files, "")
	if err != nil {
		t.Fatalf("getEpisodeFile: %v", err)
	}
	if episode.Path() != "Some.Show.S01/Some.Show.S01E01.mkv" {
		t.Errorf("episode path = %q, want Some.Show.S01/Some.Show.S01E01.mkv", episode.Path())
	}
}

func TestGetEpisodeFileErrorsWhenNoValidEpisode(t *testing.T) {
	mock := &mockTorrentClient{
		files: []torrentclient.File{
			{Name: "Some.Show.S01/poster.jpg", Size: 50},
			{Name: "Some.Show.S01/readme.txt", Size: 10},
		},
	}
	if _, err := episodeFileFromFiles(mock.files, ""); err == nil {
		t.Fatal("expected error when no valid episode file is present, got nil")
	}
}

func TestGetEpisodeFilesKeepsSuccessfulBulkResults(t *testing.T) {
	mock := &mockTorrentClient{
		filesByHash: map[string][]torrentclient.File{
			"one": {{Name: "Show.S01E01.1080p.WEB-DL-GROUP.mkv", Size: 100}},
		},
		fileErrByHash: map[string]error{"two": errors.New("boom")},
	}
	p := newTestProcessor(mock)
	candidates := []entry{
		{torrent: torrentclient.Torrent{Hash: "one", Name: "Show.S01E01", SavePath: "/one"}},
		{torrent: torrentclient.Torrent{Hash: "two", Name: "Show.S01E02", SavePath: "/two"}},
	}

	episodes := p.getEpisodeFiles(candidates)
	require.Len(t, episodes, 1)
	require.Equal(t, "/one/Show.S01E01.1080p.WEB-DL-GROUP.mkv", episodes[0].Path())
	require.Equal(t, 1, mock.fileBatchCalls)
	require.Equal(t, 2, mock.fileCalls)
	require.Equal(t, []string{"one", "two"}, mock.gotHashes)
}

func resetProcessorGlobals() {
	clientMap = xsync.NewMapOf[string, cachedTorrentClient]()
	entryMap = xsync.NewMapOf[string, *entryCache]()
	planMap = xsync.NewMapOf[importPlanCacheKey, cachedImportPlan]()
}

func TestClientConfigsEqualCoversEveryField(t *testing.T) {
	base := domain.Client{
		Type:     "qbittorrent",
		Host:     "localhost",
		Port:     8080,
		Username: "user",
		Password: "password",
		APIKey:   "api-key",
		Import: domain.ImportPolicy{
			SavePath:      "/save",
			Tags:          []string{"one", "two"},
			Category:      "category",
			DownloadPath:  "/download",
			ContentLayout: "subfolder",
		},
	}
	require.True(t, clientConfigsEqual(base, cloneClientConfig(base)))

	tests := map[string]func(*domain.Client){
		"type":           func(client *domain.Client) { client.Type = "transmission" },
		"host":           func(client *domain.Client) { client.Host = "other" },
		"port":           func(client *domain.Client) { client.Port++ },
		"username":       func(client *domain.Client) { client.Username = "other" },
		"password":       func(client *domain.Client) { client.Password = "other" },
		"api key":        func(client *domain.Client) { client.APIKey = "other" },
		"save path":      func(client *domain.Client) { client.Import.SavePath = "/other" },
		"tags":           func(client *domain.Client) { client.Import.Tags[0] = "other" },
		"category":       func(client *domain.Client) { client.Import.Category = "other" },
		"download path":  func(client *domain.Client) { client.Import.DownloadPath = "/other" },
		"content layout": func(client *domain.Client) { client.Import.ContentLayout = "original" },
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			changed := cloneClientConfig(base)
			mutate(&changed)
			require.False(t, clientConfigsEqual(base, changed))
		})
	}

	withoutTags := cloneClientConfig(base)
	withoutTags.Import.Tags = nil
	emptyTags := cloneClientConfig(base)
	emptyTags.Import.Tags = []string{}
	require.True(t, clientConfigsEqual(withoutTags, emptyTags), "nil and empty tags have the same policy")

	allocations := testing.AllocsPerRun(1000, func() {
		clientConfigsEqualSink = clientConfigsEqual(base, base)
	})
	require.Zero(t, allocations, "config equality is on the request path")
}

func TestTorrentEntryCacheIsScopedToClientAndFuzzyConfig(t *testing.T) {
	resetProcessorGlobals()
	mock := &mockTorrentClient{torrents: []torrentclient.Torrent{{Name: "Series.S01E01.1080p.WEB-DL-GRP"}}}
	p := newTestProcessor(mock)
	client := &domain.Client{Type: "qbittorrent", Import: domain.ImportPolicy{Category: "one"}}

	_, err := p.getAllTorrents("default", client, domain.FuzzyMatching{})
	require.NoError(t, err)
	cached, ok := entryMap.Load("default")
	require.True(t, ok)
	cached.expiresAt = time.Now().Add(time.Minute)
	_, err = p.getAllTorrents("default", client, domain.FuzzyMatching{})
	require.NoError(t, err)
	require.Equal(t, 1, mock.torrentCalls, "unchanged config should reuse cached entries")

	changedClient := cloneClientConfig(*client)
	changedClient.Import.Category = "two"
	_, err = p.getAllTorrents("default", &changedClient, domain.FuzzyMatching{})
	require.NoError(t, err)
	require.Equal(t, 2, mock.torrentCalls, "client config change should refresh cached entries")

	_, err = p.getAllTorrents("default", &changedClient, domain.FuzzyMatching{SkipYearCompare: true})
	require.NoError(t, err)
	require.Equal(t, 3, mock.torrentCalls, "fuzzy config change should refresh cached entries")
}

func newImportProcessor() *processor {
	cfg := staticConfig{config: domain.Config{
		Clients: map[string]*domain.Client{
			"default": {Type: "qbittorrent", Import: domain.ImportPolicy{Category: "tv-hd"}},
		},
	}}
	return newProcessor(logger.New(&domain.Config{LogLevel: "ERROR", Version: "test"}), cfg, nil)
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
	torrentBytes, err := torrents.TorrentFromRls("Series.S01.1080p.WEB-DL.H.264-RlsGrp", 1)
	require.NoError(t, err)

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
	p.req = &request{
		Name:       "Series.S01.1080p.WEB-DL.H.264-RlsGrp",
		Torrent:    []byte(base64.StdEncoding.EncodeToString(torrentBytes)),
		Client:     mock,
		ClientName: "default",
	}

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

func TestParseTorrentRejectsArchivePackWithSampleVideos(t *testing.T) {
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

	mock := &mockTorrentClient{
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
		Client:     mock,
		ClientName: "default",
	}

	statusCode, err := p.parseTorrent()
	require.EqualError(t, err, domain.StatusFailedMatchToTorrentEps.String())
	require.Equal(t, domain.StatusFailedMatchToTorrentEps, statusCode)
	require.False(t, mock.importCalled, "rejected archive pack must not be imported")
	require.NoFileExists(t, filepath.Join(importDir, releaseName, episodeName))
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
