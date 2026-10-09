// Copyright (c) 2023 - 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package http

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"encoding/xml"
	"fmt"
	stdhttp "net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/nuxencs/seasonpackarr/internal/domain"
	"github.com/nuxencs/seasonpackarr/internal/logger"
	"github.com/nuxencs/seasonpackarr/internal/prowlarr"
	"github.com/nuxencs/seasonpackarr/internal/state"
	"github.com/nuxencs/seasonpackarr/internal/torrentclient"
	"github.com/nuxencs/seasonpackarr/internal/torrents"

	"github.com/puzpuzpuz/xsync/v3"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const processorTestToken = "processor-test-token"

type fakeNotificationSender struct{}

func (fakeNotificationSender) Name() string { return "noop" }

func (fakeNotificationSender) Send(context.Context, domain.StatusCode, domain.NotificationPayload) error {
	return nil
}

type fakeConfig struct {
	mu     sync.RWMutex
	config domain.Config
}

func (c *fakeConfig) Snapshot() domain.Config {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.config
}

func (c *fakeConfig) Store(config domain.Config) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.config = config
}

type processorHTTPFixture struct {
	search        *searchRunner
	statePath     string
	handler       stdhttp.Handler
	torrentClient *fakeTorrentClient
	config        *fakeConfig
	releaseName   string
	torrent       []byte
	sourceDir     string
	importDir     string
}

// fixtureOptions sets the season pack and the client inventory that a fixture
// starts with.
type fixtureOptions struct {
	packEpisodes   int           // episodes in the season pack torrent
	clientEpisodes int           // episodes from E01 that the client already holds
	threshold      float32       // smart-mode threshold; 0 accepts any coverage
	log            logger.Logger // nil logs errors only
}

func newProcessorHTTPFixture(t *testing.T, opts fixtureOptions) processorHTTPFixture {
	t.Helper()
	resetProcessorGlobals()

	tempDir := t.TempDir()
	sourceDir := filepath.Join(tempDir, "source")
	importDir := filepath.Join(tempDir, "import")
	require.NoError(t, os.MkdirAll(sourceDir, 0o755))
	require.NoError(t, os.MkdirAll(importDir, 0o755))

	releaseName := "Lifecycle.S01.1080p.WEB-DL.H.264-RlsGrp"
	torrentBytes, err := torrents.TorrentFromRls(releaseName, opts.packEpisodes)
	require.NoError(t, err)

	torrentClient := &fakeTorrentClient{
		filesByHash: make(map[string][]torrentclient.File),
		importRoot:  importDir,
	}
	for episode := 1; episode <= opts.clientEpisodes; episode++ {
		episodeRelease := fmt.Sprintf("Lifecycle.S01E%02d.1080p.WEB-DL.H.264-RlsGrp", episode)
		episodeFile := episodeRelease + ".mkv"
		hash := fmt.Sprintf("ep%d", episode)
		writeEpisode(t, filepath.Join(sourceDir, episodeFile))
		torrentClient.torrents = append(torrentClient.torrents, torrentclient.Torrent{
			Name:     episodeRelease,
			Hash:     hash,
			SavePath: sourceDir,
		})
		torrentClient.filesByHash[hash] = []torrentclient.File{{Name: episodeFile, Size: 1}}
	}

	clientCfg := &domain.Client{
		Type: "qbittorrent",
		Import: domain.ImportPolicy{
			SavePath: importDir,
			Category: "tv-hd",
		},
	}
	cfg := &fakeConfig{config: domain.Config{
		Clients:            map[string]*domain.Client{"default": clientCfg},
		SmartMode:          true,
		SmartModeThreshold: opts.threshold,
		APIToken:           processorTestToken,
	}}
	clientMap.Store("default", cachedTorrentClient{config: cloneClientConfig(*clientCfg), client: torrentClient})

	statePath := filepath.Join(tempDir, "seasonpackarr.db")
	store, err := state.Open(t.Context(), statePath, zerolog.Nop())
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })
	log := opts.log
	if log == nil {
		log = logger.New(&domain.Config{LogLevel: "ERROR", Version: "test"})
	}
	server := NewServer(
		log,
		cfg,
		fakeNotificationSender{},
		store,
	)
	return processorHTTPFixture{
		search:        server.search,
		statePath:     statePath,
		handler:       server.Handler(),
		torrentClient: torrentClient,
		config:        cfg,
		releaseName:   releaseName,
		torrent:       torrentBytes,
		sourceDir:     sourceDir,
		importDir:     importDir,
	}
}

func (f processorHTTPFixture) postJSON(t *testing.T, path string, payload any) *httptest.ResponseRecorder {
	t.Helper()
	return f.postRaw(t, path, encodeJSON(t, payload), processorTestToken)
}

func encodeJSON(t *testing.T, payload any) []byte {
	t.Helper()
	body, err := json.Marshal(payload)
	require.NoError(t, err)
	return body
}

// postRaw makes no assertions, so a goroutine can call it. postJSON cannot.
func (f processorHTTPFixture) postRaw(t *testing.T, path string, body []byte, token string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequestWithContext(t.Context(), stdhttp.MethodPost, path, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("X-API-Token", token)
	}
	res := httptest.NewRecorder()
	f.handler.ServeHTTP(res, req)
	return res
}

func (f processorHTTPFixture) packPayload() map[string]any {
	return map[string]any{
		"name":       f.releaseName,
		"clientname": "default",
		"torrent":    base64.StdEncoding.EncodeToString(f.torrent),
	}
}

// fakeTorrentClient is a configurable in-memory torrentclient.TorrentClient for
// exercising the processor without a live torrent client.
type fakeTorrentClient struct {
	torrents       []torrentclient.Torrent
	torrentsErr    error
	getTorrents    func(context.Context) ([]torrentclient.Torrent, error)
	torrentCalls   int
	files          []torrentclient.File            // returned for any hash when filesByHash is nil
	filesByHash    map[string][]torrentclient.File // per-hash file lists
	filesErr       error
	fileErrByHash  map[string]error
	gotHash        string
	gotHashes      []string
	fileCalls      int
	fileBatchCalls int
	afterGetFiles  func()

	importRoot          string
	importRootErr       error
	flatImport          bool
	onImportDestination func()
	importErr           error
	importReport        torrentclient.ImportReport
	importCalled        bool
	importCalls         int
	importReq           torrentclient.ImportRequest
	importCtxErr        error
}

func (f *fakeTorrentClient) GetTorrents(ctx context.Context) ([]torrentclient.Torrent, error) {
	f.torrentCalls++
	if f.getTorrents != nil {
		return f.getTorrents(ctx)
	}
	return f.torrents, f.torrentsErr
}

func (f *fakeTorrentClient) GetFiles(_ context.Context, hashes []string) []torrentclient.FileResult {
	f.fileBatchCalls++
	f.fileCalls += len(hashes)
	f.gotHashes = append([]string(nil), hashes...)
	if len(hashes) > 0 {
		f.gotHash = hashes[0]
	}

	results := make([]torrentclient.FileResult, len(hashes))
	for index, hash := range hashes {
		results[index].Hash = hash
		if f.filesErr != nil {
			results[index].Err = f.filesErr
			continue
		}
		if err := f.fileErrByHash[hash]; err != nil {
			results[index].Err = err
			continue
		}
		if f.filesByHash != nil {
			results[index].Files = f.filesByHash[hash]
			continue
		}
		results[index].Files = f.files
	}
	if f.afterGetFiles != nil {
		f.afterGetFiles()
	}
	return results
}

func (f *fakeTorrentClient) ImportDestination(context.Context) (torrentclient.ImportDestination, error) {
	if f.onImportDestination != nil {
		f.onImportDestination()
	}
	if f.importRootErr != nil {
		return torrentclient.ImportDestination{}, f.importRootErr
	}
	if f.flatImport {
		return torrentclient.NewFlatImportDestination(f.importRoot), nil
	}
	return torrentclient.NewRootedImportDestination(f.importRoot), nil
}

func (f *fakeTorrentClient) Import(ctx context.Context, req torrentclient.ImportRequest) (torrentclient.ImportReport, error) {
	f.importCalled = true
	f.importCalls++
	f.importReq = req
	f.importCtxErr = ctx.Err()
	return f.importReport, f.importErr
}

func newTestProcessor(client torrentclient.TorrentClient) *processor {
	return &processor{req: &request{Client: client}}
}

func resetProcessorGlobals() {
	clientMap = xsync.NewMapOf[string, cachedTorrentClient]()
	entryMap = xsync.NewMapOf[string, *entryCache]()
	planMap = xsync.NewMapOf[importPlanCacheKey, cachedImportPlan]()
}

func newImportProcessor() *processor {
	cfg := &fakeConfig{config: domain.Config{
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

type fakeSearchClient struct {
	*fakeTorrentClient
	importing chan struct{}
	resume    chan struct{}
}

func (f *fakeSearchClient) Import(ctx context.Context, req torrentclient.ImportRequest) (torrentclient.ImportReport, error) {
	if f.importing != nil {
		close(f.importing)
		select {
		case <-f.resume:
		case <-ctx.Done():
			return torrentclient.ImportReport{}, ctx.Err()
		}
	}
	report, err := f.fakeTorrentClient.Import(ctx, req)
	if err == nil {
		info, parseErr := torrents.Info(req.TorrentBytes)
		if parseErr != nil {
			return report, parseErr
		}
		f.torrents = append(f.torrents, torrentclient.Torrent{Name: info.BestName(), Hash: req.LegacyHash, SavePath: req.SavePath})
	}
	return report, err
}

type searchFixture struct {
	processorHTTPFixture
	queries        []string
	discoveryCalls int
	downloads      int
	titles         []string
	failFirst      bool
	beforeSearch   func()
	respond        func(stdhttp.ResponseWriter, *stdhttp.Request) bool
	pages          bool
	pageSize       int
	torrentByTitle map[string][]byte
}

func newSearchFixture(t *testing.T, opts fixtureOptions) *searchFixture {
	t.Helper()
	f := &searchFixture{processorHTTPFixture: newProcessorHTTPFixture(t, opts)}
	f.titles = []string{f.releaseName}
	server := httptest.NewServer(stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		if r.Header.Get("X-Api-Key") != "prowlarr-test-key" {
			w.WriteHeader(401)
			return
		}
		if f.respond != nil && f.respond(w, r) {
			return
		}
		if r.URL.Path == "/api/v1/indexer" {
			f.discoveryCalls++
			fmt.Fprintf(w, `[{"id":1,"priority":1,"enable":true,"protocol":"torrent","supportsSearch":true,"supportsRss":true,"supportsPagination":%t,"capabilities":{"searchParams":["q"],"limitsMax":%d}},{"id":2,"priority":2,"enable":true,"protocol":"torrent","supportsSearch":true,"supportsRss":true,"capabilities":{"searchParams":["q"]}}]`, f.pages, f.pageSize)
			return
		}
		if strings.HasSuffix(r.URL.Path, "/api") {
			f.queries = append(f.queries, r.URL.Query().Get("q"))
			if f.beforeSearch != nil {
				f.beforeSearch()
			}
			if f.failFirst && r.URL.Path == "/1/api" {
				w.WriteHeader(429)
				return
			}
			feed := struct {
				XMLName xml.Name          `xml:"rss"`
				Items   []prowlarr.Result `xml:"channel>item"`
			}{}
			id := strings.Split(r.URL.Path, "/")[1]
			for i, title := range f.titles {
				if f.pages {
					offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
					limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
					if i < offset || i >= offset+limit {
						continue
					}
				}
				feed.Items = append(feed.Items, prowlarr.Result{Title: title, GUID: fmt.Sprintf("%s-%s", id, title), Link: fmt.Sprintf("http://%s/%s/download?link=%d&file=t", r.Host, id, i)})
			}
			assert.NoError(t, xml.NewEncoder(w).Encode(feed))
			return
		}
		if strings.HasSuffix(r.URL.Path, "/download") {
			f.downloads++
			i, _ := strconv.Atoi(r.URL.Query().Get("link"))
			if data := f.torrentByTitle[f.titles[i]]; data != nil {
				w.Write(data)
			} else {
				w.Write(f.torrent)
			}
			return
		}
		w.WriteHeader(404)
	}))
	t.Cleanup(server.Close)
	cfg := f.config.Snapshot()
	cfg.Search = domain.Search{ProwlarrURL: server.URL, APIKey: "prowlarr-test-key", RSSInterval: "0s", RequestInterval: "0s"}
	f.config.Store(cfg)
	clientMap.Store("default", cachedTorrentClient{config: *cfg.Clients["default"], client: &fakeSearchClient{fakeTorrentClient: f.torrentClient}})
	return f
}

func (f *searchFixture) runExact(t *testing.T, dryRun bool) searchReport {
	t.Helper()
	response := f.postJSON(t, "/api/search", map[string]any{"dryRun": dryRun, "verify": dryRun})
	require.Equal(t, 200, response.Code, response.Body.String())
	var report searchReport
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &report))
	return report
}

func (f *searchFixture) restart(t *testing.T) {
	t.Helper()
	require.NoError(t, f.search.state.Close())
	store, err := state.Open(t.Context(), f.statePath, zerolog.Nop())
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })
	server := NewServer(f.search.log, f.config, fakeNotificationSender{}, store)
	f.search, f.handler = server.search, server.Handler()
}
