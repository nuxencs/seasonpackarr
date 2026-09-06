// Copyright (c) 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package http

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"fmt"
	stdhttp "net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/nuxencs/seasonpackarr/internal/domain"
	"github.com/nuxencs/seasonpackarr/internal/prowlarr"
	"github.com/nuxencs/seasonpackarr/internal/torrentclient"
	"github.com/nuxencs/seasonpackarr/internal/torrents"
	"github.com/stretchr/testify/require"
)

type recordingSearchClient struct {
	*mockTorrentClient
	importing chan struct{}
	resume    chan struct{}
}

func (m *recordingSearchClient) Import(ctx context.Context, req torrentclient.ImportRequest) (torrentclient.ImportReport, error) {
	if m.importing != nil {
		close(m.importing)
		select {
		case <-m.resume:
		case <-ctx.Done():
			return torrentclient.ImportReport{}, ctx.Err()
		}
	}
	report, err := m.mockTorrentClient.Import(ctx, req)
	if err == nil {
		info, parseErr := torrents.Info(req.TorrentBytes)
		if parseErr != nil {
			return report, parseErr
		}
		m.torrents = append(m.torrents, torrentclient.Torrent{Name: info.BestName(), Hash: req.LegacyHash, SavePath: req.SavePath})
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

func newSearchFixture(t *testing.T, packEpisodes, clientEpisodes int, threshold float32) *searchFixture {
	t.Helper()
	f := &searchFixture{processorHTTPFixture: newProcessorHTTPFixture(t, packEpisodes, clientEpisodes, threshold)}
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
			fmt.Fprintf(w, `[{"id":1,"priority":1,"enable":true,"protocol":"torrent","supportsSearch":true,"supportsPagination":%t,"capabilities":{"searchParams":["q"],"limitsMax":%d}},{"id":2,"priority":2,"enable":true,"protocol":"torrent","supportsSearch":true,"capabilities":{"searchParams":["q"]}}]`, f.pages, f.pageSize)
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
				feed.Items = append(feed.Items, prowlarr.Result{Title: title, GUID: fmt.Sprintf("%s-%d", id, i), Link: fmt.Sprintf("http://%s/%s/download?link=%d&file=t", r.Host, id, i)})
			}
			require.NoError(t, xml.NewEncoder(w).Encode(feed))
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
	cfg.Search = domain.Search{ProwlarrURL: server.URL, APIKey: "prowlarr-test-key", Interval: "0s", RequestInterval: "0s"}
	f.config.Store(cfg)
	clientMap.Store("default", cachedTorrentClient{config: *cfg.Clients["default"], client: &recordingSearchClient{mockTorrentClient: f.mock}})
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
	require.Zero(t, f.mock.importCalls)
	entries, err := os.ReadDir(f.importDir)
	require.NoError(t, err)
	require.Empty(t, entries)
}

func TestSearch_ImportsOnceAndSkipsExistingPackAcrossTrackers(t *testing.T) {
	f := newSearchFixture(t, 2, 2, 0.75)
	first := f.runExact(t, false)
	require.Equal(t, "imported", first.Outcomes[0].Status)
	require.Equal(t, 1, f.mock.importCalls)
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
	require.Equal(t, 1, f.mock.importCalls)
	require.Equal(t, 1, f.downloads)
}

func TestSearch_RespectsSmartMode(t *testing.T) {
	for _, test := range []struct {
		name      string
		enabled   bool
		threshold float32
		want      string
	}{
		{"below threshold", true, 0.75, "rejected"},
		{"at threshold", true, 0.5, "would_import"},
		{"disabled", false, 0.75, "would_import"},
	} {
		t.Run(test.name, func(t *testing.T) {
			f := newSearchFixture(t, 2, 1, test.threshold)
			cfg := f.config.Snapshot()
			cfg.SmartMode = test.enabled
			f.config.Store(cfg)
			report := f.runExact(t, true)
			require.Equal(t, test.want, report.Outcomes[0].Status)
			if test.want == "rejected" {
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
	require.Zero(t, f.mock.fileCalls)
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
	clientMap.Store("default", cachedTorrentClient{config: *cfg.Clients["default"], client: &recordingSearchClient{mockTorrentClient: f.mock, importing: importing, resume: resume}})
	first := make(chan *httptest.ResponseRecorder, 1)
	go func() { first <- f.postJSON(t, "/api/search", map[string]any{"dryRun": false}) }()
	<-importing
	second := make(chan *httptest.ResponseRecorder, 1)
	go func() { second <- f.postJSON(t, "/api/import", f.packPayload()) }()
	close(resume)
	require.Equal(t, 200, (<-first).Code)
	require.Equal(t, domain.StatusAlreadyInClient.Code(), (<-second).Code)
	require.Equal(t, 1, f.mock.importCalls)
}

func TestSearchSchedule_OptInAndReload(t *testing.T) {
	now := time.Now()
	var schedule searchSchedule
	require.False(t, schedule.due(now, "0s"))
	require.False(t, schedule.due(now, "1h"))
	require.False(t, schedule.due(now.Add(59*time.Minute), "1h"))
	require.True(t, schedule.due(now.Add(time.Hour), "1h"))
	require.False(t, schedule.due(now.Add(time.Hour), "2h"))
	require.False(t, schedule.due(now.Add(4*time.Hour), "0s"))
	require.False(t, schedule.due(now.Add(5*time.Hour), "1h"))
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
	f.mock.torrents = append(f.mock.torrents, torrentclient.Torrent{Name: otherEpisode, Hash: "other", SavePath: f.sourceDir})
	f.mock.filesByHash["other"] = []torrentclient.File{{Name: otherEpisode + ".mkv", Size: 1}}
	report := f.runExact(t, false)
	require.Empty(t, report.Failures)
	require.Equal(t, 1, report.Groups)
	require.Equal(t, 2, f.mock.importCalls)
	require.Equal(t, "imported", report.Outcomes[0].Status)
	require.Equal(t, "imported", report.Outcomes[1].Status)
	require.Equal(t, "rejected", report.Outcomes[2].Status)
}

func TestSearch_GroupIdentityAndUnrelatedTorrents(t *testing.T) {
	f := newSearchFixture(t, 1, 1, 0.75)
	for _, name := range []string{
		"Example.2024.S01E01.1080p.WEB-DL.H.264-RlsGrp",
		"Example.2025.S01E01.1080p.WEB-DL.H.264-RlsGrp",
		"Example.2024.S02E01.1080p.WEB-DL.H.264-RlsGrp",
		"Film.2024.1080p.BluRay.x264-RlsGrp",
		"Packed.S01.1080p.WEB-DL.H.264-RlsGrp",
		"readme.txt",
	} {
		f.mock.torrents = append(f.mock.torrents, torrentclient.Torrent{Name: name, Hash: name})
	}
	report := f.runExact(t, true)
	require.Equal(t, 4, report.Groups)
	require.ElementsMatch(t, []string{
		"Example S01", "Example S01", // 2024 group, two indexers
		"Example S01", "Example S01", // 2025 group, two indexers
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
	clientMap.Store("alias", cachedTorrentClient{config: alias, client: f.mock})
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

func TestSearchSchedule_CancellationStopsWorker(t *testing.T) {
	f := newSearchFixture(t, 1, 1, 0.75)
	runner := &searchRunner{cfg: f.config}
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() { runner.schedule(ctx); close(done) }()
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("scheduler did not stop")
	}
	require.Empty(t, f.queries)
}

func TestInventory_InvalidationDuringScanCannotRestoreStaleCache(t *testing.T) {
	resetProcessorGlobals()
	started, proceed := make(chan struct{}), make(chan struct{})
	mock := &mockTorrentClient{getTorrents: func(context.Context) ([]torrentclient.Torrent, error) {
		close(started)
		<-proceed
		return []torrentclient.Torrent{{Name: "Example.S01E01.1080p.WEB-DL.H.264-RlsGrp"}}, nil
	}}
	p := newTestProcessor(mock)
	done := make(chan error, 1)
	go func() {
		_, err := p.getAllTorrents(t.Context(), "default", &domain.Client{}, domain.FuzzyMatching{})
		done <- err
	}()
	<-started
	invalidateClientImports(domain.Client{})
	close(proceed)
	require.NoError(t, <-done)
	_, cached := entryMap.Load("default")
	require.False(t, cached, "an import invalidated the in-flight inventory")
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

// Exercise the shipped CLI against the real authenticated API, Prowlarr HTTP
// fixture, and filesystem. The torrent client's network boundary is controlled.
func TestSearchCLI_PreviewAndImport(t *testing.T) {
	f := newSearchFixture(t, 1, 1, 0.75)
	server := httptest.NewServer(f.handler)
	defer server.Close()
	for _, mode := range []string{"discovery", "verify", "import"} {
		dryRun := mode != "import"
		args := []string{"run", "../..", "search", "--url", server.URL, "--api", processorTestToken}
		if dryRun {
			args = append(args, "--dry-run")
			if mode == "verify" {
				args = append(args, "--verify")
			}
		}
		command := exec.CommandContext(t.Context(), "go", args...)
		output, err := command.CombinedOutput()
		require.NoError(t, err, string(output))
		var report searchReport
		require.NoError(t, json.Unmarshal(output, &report), string(output))
		require.Equal(t, dryRun, report.DryRun)
		if mode == "discovery" {
			require.Equal(t, "candidate", report.Outcomes[0].Status)
			require.Zero(t, f.downloads)
			require.Zero(t, f.mock.fileCalls)
		} else if dryRun {
			require.Equal(t, "would_import", report.Outcomes[0].Status)
			require.Zero(t, f.mock.importCalls)
		} else {
			require.Equal(t, "imported", report.Outcomes[0].Status)
			require.Equal(t, 1, f.mock.importCalls)
			require.Equal(t, 1, report.TorrentCacheHits)
			require.Zero(t, report.TorrentDownloads)
		}
	}
}

func TestSearch_QueryOmitsYearButMatchingRespectsSettings(t *testing.T) {
	for _, skipYear := range []bool{false, true} {
		t.Run(fmt.Sprintf("skip year %t", skipYear), func(t *testing.T) {
			f := newSearchFixture(t, 1, 1, 0.75)
			cfg := f.config.Snapshot()
			cfg.Search.IndexerIDs = []int{1}
			cfg.FuzzyMatching.SkipYearCompare = skipYear
			f.config.Store(cfg)
			f.mock.torrents[0].Name = "Lifecycle.2024.S01E01.1080p.WEB-DL.H.264-RlsGrp"
			f.titles = []string{
				"Lifecycle.2024.S01.1080p.WEB-DL.H.264-RlsGrp",
				"Lifecycle.2023.S01.1080p.WEB-DL.H.264-RlsGrp",
			}
			response := f.postJSON(t, "/api/search", SearchRequest{DryRun: true})
			require.Equal(t, 200, response.Code, response.Body.String())
			var report searchReport
			require.NoError(t, json.Unmarshal(response.Body.Bytes(), &report))
			require.Equal(t, []string{"Lifecycle S01"}, f.queries)
			require.Len(t, report.Outcomes, 2)
			require.Equal(t, "candidate", report.Outcomes[0].Status)
			if skipYear {
				require.Equal(t, "candidate", report.Outcomes[1].Status)
			} else {
				require.Equal(t, "rejected", report.Outcomes[1].Status)
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
	require.Zero(t, f.mock.fileBatchCalls)
	require.Zero(t, f.mock.importCalls)
}

func TestSearch_IndexerAllowlist(t *testing.T) {
	for _, test := range []struct {
		name                       string
		ids                        []int
		wantID, requests, failures int
	}{
		{"selected", []int{2}, 2, 1, 0},
		{"missing", []int{99}, 0, 0, 2},
		{"partial", []int{2, 99}, 2, 1, 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			f := newSearchFixture(t, 1, 1, 0.75)
			cfg := f.config.Snapshot()
			cfg.Search.IndexerIDs = test.ids
			f.config.Store(cfg)
			report := f.runExact(t, true)
			require.Equal(t, test.requests, report.Requests)
			require.Len(t, report.Failures, test.failures)
			require.Len(t, report.Outcomes, test.requests)
			for _, outcome := range report.Outcomes {
				require.Equal(t, test.wantID, outcome.IndexerID)
			}
		})
	}
}

func TestSearch_PreflightStopsBeforeMetadata(t *testing.T) {
	for _, mode := range []string{"missing", "empty", "wrong size", "directory", "client error"} {
		t.Run(mode, func(t *testing.T) {
			f := newSearchFixture(t, 1, 1, 0.75)
			path := filepath.Join(f.sourceDir, f.mock.filesByHash["ep1"][0].Name)
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
				f.mock.filesErr = fmt.Errorf("client failed")
			}
			report := f.runExact(t, true)
			require.Zero(t, f.downloads)
			require.Zero(t, f.mock.importCalls)
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
	path := filepath.Join(f.sourceDir, f.mock.filesByHash["ep2"][0].Name)
	require.NoError(t, os.Rename(path, path+"-moved"))
	first := f.runExact(t, true)
	require.Equal(t, "rejected", first.Outcomes[0].Status)
	require.Equal(t, new(1), first.Outcomes[0].ReusableEpisodes)
	require.Equal(t, 1, first.TorrentDownloads)
	require.Equal(t, 1, f.mock.fileBatchCalls)
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
			f.mock.torrents = append(f.mock.torrents, torrentclient.Torrent{Name: f.releaseName, Hash: "pack"})
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
			require.Zero(t, f.mock.fileBatchCalls)
			require.Zero(t, f.mock.importCalls)
		})
	}
}

func TestSearch_ExistingPackRespectsVariantAndFuzzySettings(t *testing.T) {
	for _, test := range []struct {
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
		t.Run(test.name, func(t *testing.T) {
			f := newSearchFixture(t, 1, 1, 0.75)
			cfg := f.config.Snapshot()
			cfg.FuzzyMatching = test.fuzzy
			f.config.Store(cfg)
			f.mock.torrents = append(f.mock.torrents, torrentclient.Torrent{Name: test.pack, Hash: "pack"})
			response := f.postJSON(t, "/api/search", SearchRequest{DryRun: true})
			require.Equal(t, 200, response.Code, response.Body.String())
			var report searchReport
			require.NoError(t, json.Unmarshal(response.Body.Bytes(), &report))
			if test.covered {
				require.Equal(t, 1, report.CoveredEpisodeTorrents)
				require.Zero(t, report.Requests)
			} else {
				require.Zero(t, report.CoveredEpisodeTorrents)
				require.Equal(t, 2, report.Requests)
			}
			require.Zero(t, f.mock.fileBatchCalls)
			require.Zero(t, f.downloads)
		})
	}
}

func TestSearch_ExistingPackKeepsUncoveredVariantInSameSeason(t *testing.T) {
	f := newSearchFixture(t, 1, 1, 0.75)
	otherPack := "Lifecycle.S01.2160p.WEB-DL.H.265-OtherGrp"
	otherEpisode := "Lifecycle.S01E01.2160p.WEB-DL.H.265-OtherGrp"
	f.titles = append(f.titles, otherPack)
	f.mock.torrents = append(f.mock.torrents,
		torrentclient.Torrent{Name: f.releaseName, Hash: "pack"},
		torrentclient.Torrent{Name: otherEpisode, Hash: "other", SavePath: f.sourceDir},
	)
	writeEpisode(t, filepath.Join(f.sourceDir, otherEpisode+".mkv"))
	f.mock.filesByHash["other"] = []torrentclient.File{{Name: otherEpisode + ".mkv", Size: 1}}
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
	other := &mockTorrentClient{torrents: []torrentclient.Torrent{f.mock.torrents[0]}}
	f.mock.torrents = append(f.mock.torrents, torrentclient.Torrent{Name: f.releaseName, Hash: "pack"})
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
	require.Zero(t, f.mock.fileBatchCalls)
	require.Zero(t, other.fileBatchCalls)
}

func TestSearch_RemovingExistingPackRestoresQuery(t *testing.T) {
	f := newSearchFixture(t, 1, 1, 0.75)
	f.mock.torrents = append(f.mock.torrents, torrentclient.Torrent{Name: f.releaseName, Hash: "pack"})
	first := f.runExact(t, true)
	require.Zero(t, first.Requests)
	f.mock.torrents = f.mock.torrents[:1]
	second := f.runExact(t, true)
	require.Equal(t, 2, second.Requests)
	require.Equal(t, "would_import", second.Outcomes[0].Status)
}
