// Copyright (c) 2023 - 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package http

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	stdhttp "net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/autobrr/go-torrent/bencode"
	"github.com/autobrr/go-torrent/metainfo"
	"github.com/nuxencs/seasonpackarr/internal/domain"
	"github.com/nuxencs/seasonpackarr/internal/logger"
	"github.com/nuxencs/seasonpackarr/internal/torrentclient"
	"github.com/nuxencs/seasonpackarr/internal/torrents"

	"github.com/puzpuzpuz/xsync/v3"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"
)

const processorTestToken = "processor-test-token"

type noopNotificationSender struct{}

func (noopNotificationSender) Name() string { return "noop" }

func (noopNotificationSender) Send(domain.StatusCode, domain.NotificationPayload) error { return nil }

type mutableProcessorConfig struct {
	mu     sync.RWMutex
	config domain.Config
}

func (c *mutableProcessorConfig) Snapshot() domain.Config {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.config
}

func (c *mutableProcessorConfig) Store(config domain.Config) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.config = config
}

type processorHTTPFixture struct {
	handler     stdhttp.Handler
	mock        *mockTorrentClient
	config      *mutableProcessorConfig
	releaseName string
	torrent     []byte
	sourceDir   string
	importDir   string
}

func newProcessorHTTPFixture(t *testing.T, torrentEpisodes, clientEpisodes int, threshold float32) processorHTTPFixture {
	return newProcessorHTTPFixtureWithLogger(t, torrentEpisodes, clientEpisodes, threshold,
		logger.New(&domain.Config{LogLevel: "ERROR", Version: "test"}))
}

func newProcessorHTTPFixtureWithLogger(
	t *testing.T,
	torrentEpisodes, clientEpisodes int,
	threshold float32,
	log logger.Logger,
) processorHTTPFixture {
	t.Helper()
	resetProcessorGlobals()

	tempDir := t.TempDir()
	sourceDir := filepath.Join(tempDir, "source")
	importDir := filepath.Join(tempDir, "import")
	require.NoError(t, os.MkdirAll(sourceDir, 0o755))
	require.NoError(t, os.MkdirAll(importDir, 0o755))

	releaseName := "Lifecycle.S01.1080p.WEB-DL.H.264-RlsGrp"
	torrentBytes, err := torrents.TorrentFromRls(releaseName, torrentEpisodes)
	require.NoError(t, err)

	mock := &mockTorrentClient{
		filesByHash: make(map[string][]torrentclient.File),
		importRoot:  importDir,
	}
	for episode := 1; episode <= clientEpisodes; episode++ {
		episodeRelease := fmt.Sprintf("Lifecycle.S01E%02d.1080p.WEB-DL.H.264-RlsGrp", episode)
		episodeFile := episodeRelease + ".mkv"
		hash := fmt.Sprintf("ep%d", episode)
		writeEpisode(t, filepath.Join(sourceDir, episodeFile))
		mock.torrents = append(mock.torrents, torrentclient.Torrent{
			Name:     episodeRelease,
			Hash:     hash,
			SavePath: sourceDir,
		})
		mock.filesByHash[hash] = []torrentclient.File{{Name: episodeFile, Size: 1}}
	}

	clientCfg := &domain.Client{
		Type: "qbittorrent",
		Import: domain.ImportPolicy{
			SavePath: importDir,
			Category: "tv-hd",
		},
	}
	cfg := &mutableProcessorConfig{config: domain.Config{
		Clients:            map[string]*domain.Client{"default": clientCfg},
		SmartMode:          true,
		SmartModeThreshold: threshold,
		APIToken:           processorTestToken,
	}}
	clientMap.Store("default", cachedTorrentClient{config: cloneClientConfig(*clientCfg), client: mock})

	server := NewServer(
		log,
		cfg,
		noopNotificationSender{},
	)
	return processorHTTPFixture{
		handler:     server.Handler(),
		mock:        mock,
		config:      cfg,
		releaseName: releaseName,
		torrent:     torrentBytes,
		sourceDir:   sourceDir,
		importDir:   importDir,
	}
}

type capturedLogger struct {
	log zerolog.Logger
}

func newCapturedLogger(output *bytes.Buffer) logger.Logger {
	return &capturedLogger{log: zerolog.New(output)}
}

func (l *capturedLogger) Log() *zerolog.Event          { return l.log.Log() }
func (l *capturedLogger) Fatal() *zerolog.Event        { return l.log.Error() }
func (l *capturedLogger) Err(err error) *zerolog.Event { return l.log.Err(err) }
func (l *capturedLogger) Error() *zerolog.Event        { return l.log.Error() }
func (l *capturedLogger) Warn() *zerolog.Event         { return l.log.Warn() }
func (l *capturedLogger) Info() *zerolog.Event         { return l.log.Info() }
func (l *capturedLogger) Trace() *zerolog.Event        { return l.log.Trace() }
func (l *capturedLogger) Debug() *zerolog.Event        { return l.log.Debug() }
func (l *capturedLogger) With() zerolog.Context        { return l.log.With() }
func (l *capturedLogger) SetLogLevel(string)           {}

func decodeCapturedLogs(t *testing.T, output *bytes.Buffer) []map[string]any {
	t.Helper()
	lines := bytes.Split(bytes.TrimSpace(output.Bytes()), []byte("\n"))
	logs := make([]map[string]any, 0, len(lines))
	for _, line := range lines {
		if len(line) == 0 {
			continue
		}
		var event map[string]any
		require.NoError(t, json.Unmarshal(line, &event))
		logs = append(logs, event)
	}
	return logs
}

func requireCapturedLog(t *testing.T, logs []map[string]any, message string) map[string]any {
	t.Helper()
	for _, event := range logs {
		if event["message"] == message {
			return event
		}
	}
	t.Fatalf("missing log message %q in %#v", message, logs)
	return nil
}

func requireCapturedLogField(t *testing.T, logs []map[string]any, message, field string, value any) map[string]any {
	t.Helper()
	for _, event := range logs {
		if event["message"] == message && event[field] == value {
			return event
		}
	}
	t.Fatalf("missing log message %q with %s=%v in %#v", message, field, value, logs)
	return nil
}

func (f processorHTTPFixture) postJSON(t *testing.T, path string, payload any) *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(payload)
	require.NoError(t, err)
	return f.postRaw(t, path, body, processorTestToken)
}

func (f processorHTTPFixture) postRaw(t *testing.T, path string, body []byte, token string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(stdhttp.MethodPost, path, bytes.NewReader(body))
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

func TestAuthenticatedCandidateLogsStructuredCompatibilityMismatch(t *testing.T) {
	previousLevel := zerolog.GlobalLevel()
	zerolog.SetGlobalLevel(zerolog.TraceLevel)
	t.Cleanup(func() { zerolog.SetGlobalLevel(previousLevel) })

	var output bytes.Buffer
	f := newProcessorHTTPFixtureWithLogger(t, 2, 2, 0.5, newCapturedLogger(&output))
	f.mock.torrents[0].Name = "Lifecycle.S01E01.720p.WEB-DL.H.264-RlsGrp"

	response := f.postJSON(t, "/api/candidate", map[string]any{
		"name":       f.releaseName,
		"clientname": "default",
	})
	require.Equal(t, domain.StatusSuccessfulMatch.Code(), response.Code)

	logs := decodeCapturedLogs(t, &output)
	mismatch := requireCapturedLog(t, logs, "client release is not compatible")
	require.Equal(t, "compatibility_mismatch", mismatch["reason"])
	require.Equal(t, "resolution", mismatch["field"])
	require.Equal(t, "1080p", mismatch["want"])
	require.Equal(t, "720p", mismatch["got"])
	require.Equal(t, "Lifecycle.S01E01.720p.WEB-DL.H.264-RlsGrp", mismatch["client_release"])
}

func TestAuthenticatedPackLogsUnmatchedTorrentEpisode(t *testing.T) {
	previousLevel := zerolog.GlobalLevel()
	zerolog.SetGlobalLevel(zerolog.TraceLevel)
	t.Cleanup(func() { zerolog.SetGlobalLevel(previousLevel) })

	var output bytes.Buffer
	f := newProcessorHTTPFixtureWithLogger(t, 2, 1, 0.5, newCapturedLogger(&output))

	response := f.postJSON(t, "/api/pack", f.packPayload())
	require.Equal(t, domain.StatusSuccessfulMatch.Code(), response.Code)

	logs := decodeCapturedLogs(t, &output)
	unmatched := requireCapturedLog(t, logs, "torrent episode is not reusable")
	require.Equal(t, "Lifecycle.S01E02.1080p.WEB-DL.H.264-RlsGrp.mkv", filepath.Base(unmatched["torrent_path"].(string)))
	require.Equal(t, "source_episode_not_found", unmatched["reason"])

	summary := requireCapturedLog(t, logs, "season pack import plan built")
	require.Equal(t, float64(1), summary["reusable_episodes"])
	require.Equal(t, float64(2), summary["total_episodes"])
	require.Equal(t, float64(1), summary["unmatched_episodes"])
	require.Equal(t, float64(50), summary["coverage_percent"])
	require.Equal(t, float64(50), summary["threshold_percent"])
}

func TestAuthenticatedPackLogsReasonWhenNoEpisodeIsReusable(t *testing.T) {
	previousLevel := zerolog.GlobalLevel()
	zerolog.SetGlobalLevel(zerolog.TraceLevel)
	t.Cleanup(func() { zerolog.SetGlobalLevel(previousLevel) })

	var output bytes.Buffer
	f := newProcessorHTTPFixtureWithLogger(t, 1, 1, 0, newCapturedLogger(&output))
	f.mock.filesByHash["ep1"][0].Size = 2

	response := f.postJSON(t, "/api/pack", f.packPayload())
	require.Equal(t, domain.StatusFailedMatchToTorrentEps.Code(), response.Code)

	logs := decodeCapturedLogs(t, &output)
	unmatched := requireCapturedLog(t, logs, "torrent episode is not reusable")
	require.Equal(t, "compatibility_mismatch", unmatched["reason"])
	require.Equal(t, []any{map[string]any{
		"field": "size",
		"want":  float64(1),
		"got":   float64(2),
	}}, unmatched["mismatches"])
}

func TestAuthenticatedParseLogsClientImportStageTiming(t *testing.T) {
	previousLevel := zerolog.GlobalLevel()
	zerolog.SetGlobalLevel(zerolog.TraceLevel)
	t.Cleanup(func() { zerolog.SetGlobalLevel(previousLevel) })

	var output bytes.Buffer
	f := newProcessorHTTPFixtureWithLogger(t, 2, 2, 0.75, newCapturedLogger(&output))
	f.mock.importReport = torrentclient.ImportReport{Stages: []torrentclient.ImportStageReport{
		{Stage: torrentclient.ImportStageConfig, Duration: 2 * time.Millisecond},
		{Stage: torrentclient.ImportStageAdd, Duration: 12 * time.Millisecond},
		{Stage: torrentclient.ImportStageFind, Duration: 800 * time.Millisecond},
		{Stage: torrentclient.ImportStageRecheck, Duration: 35 * time.Second},
		{Stage: torrentclient.ImportStageResume, Duration: 3 * time.Millisecond},
	}}

	pack := f.postJSON(t, "/api/pack", f.packPayload())
	require.Equal(t, domain.StatusSuccessfulMatch.Code(), pack.Code)
	output.Reset()

	parse := f.postJSON(t, "/api/parse", f.packPayload())
	require.Equal(t, domain.StatusSuccessfulHardlink.Code(), parse.Code)

	logs := decodeCapturedLogs(t, &output)
	plan := requireCapturedLog(t, logs, "import plan resolved")
	require.Equal(t, "cache", plan["plan_source"])
	require.Contains(t, plan, "duration_ms")

	hardlinks := requireCapturedLog(t, logs, "hardlink stage completed")
	require.Equal(t, float64(2), hardlinks["linked_episodes"])
	require.Contains(t, hardlinks, "duration_ms")

	recheck := requireCapturedLogField(t, logs, "torrent client import stage finished", "stage", "recheck")
	require.Equal(t, float64(35_000), recheck["duration_ms"])

	completed := requireCapturedLog(t, logs, "season pack import completed")
	require.Contains(t, completed, "total_duration_ms")
}

func TestAuthenticatedParseLogsTotalTimingOnImportFailure(t *testing.T) {
	previousLevel := zerolog.GlobalLevel()
	zerolog.SetGlobalLevel(zerolog.TraceLevel)
	t.Cleanup(func() { zerolog.SetGlobalLevel(previousLevel) })

	var output bytes.Buffer
	f := newProcessorHTTPFixtureWithLogger(t, 1, 1, 0, newCapturedLogger(&output))
	f.mock.importReport = torrentclient.ImportReport{Stages: []torrentclient.ImportStageReport{
		{Stage: torrentclient.ImportStageConfig, Duration: time.Millisecond},
		{Stage: torrentclient.ImportStageAdd, Duration: 12 * time.Millisecond},
	}}
	f.mock.importErr = &torrentclient.ImportError{
		Stage: torrentclient.ImportStageAdd,
		Err:   errors.New("client rejected torrent"),
	}

	pack := f.postJSON(t, "/api/pack", f.packPayload())
	require.Equal(t, domain.StatusSuccessfulMatch.Code(), pack.Code)
	output.Reset()

	parse := f.postJSON(t, "/api/parse", f.packPayload())
	require.Equal(t, domain.StatusAddTorrentError.Code(), parse.Code)

	logs := decodeCapturedLogs(t, &output)
	completed := requireCapturedLog(t, logs, "season pack import completed")
	require.Equal(t, false, completed["successful"])
	require.Equal(t, float64(domain.StatusAddTorrentError), completed["status_code"])
	require.Contains(t, completed, "total_duration_ms")
}

func TestAuthenticatedParseLogsFailedPlanRebuildTiming(t *testing.T) {
	previousLevel := zerolog.GlobalLevel()
	zerolog.SetGlobalLevel(zerolog.TraceLevel)
	t.Cleanup(func() { zerolog.SetGlobalLevel(previousLevel) })

	var output bytes.Buffer
	f := newProcessorHTTPFixtureWithLogger(t, 1, 1, 0, newCapturedLogger(&output))
	f.mock.filesByHash["ep1"][0].Size = 2

	parse := f.postJSON(t, "/api/parse", f.packPayload())
	require.Equal(t, domain.StatusFailedMatchToTorrentEps.Code(), parse.Code)

	logs := decodeCapturedLogs(t, &output)
	plan := requireCapturedLog(t, logs, "import plan resolved")
	require.Equal(t, "rebuilt", plan["plan_source"])
	require.Equal(t, false, plan["successful"])
	require.Contains(t, plan, "duration_ms")

	completed := requireCapturedLog(t, logs, "season pack import completed")
	require.Equal(t, false, completed["successful"])
	require.Contains(t, completed, "total_duration_ms")
}

func TestAuthenticatedHTTPLifecycleReusesAcceptedPlan(t *testing.T) {
	f := newProcessorHTTPFixture(t, 2, 2, 0.75)

	candidate := f.postJSON(t, "/api/candidate", map[string]any{
		"name":       f.releaseName,
		"clientname": "default",
	})
	require.Equal(t, domain.StatusSuccessfulMatch.Code(), candidate.Code)
	require.Equal(t, 1, f.mock.torrentCalls)
	require.Zero(t, f.mock.fileCalls)
	require.Zero(t, f.mock.importCalls)

	pack := f.postJSON(t, "/api/pack", f.packPayload())
	require.Equal(t, domain.StatusSuccessfulMatch.Code(), pack.Code)
	require.Equal(t, 1, f.mock.torrentCalls, "pack must reuse the candidate inventory")
	require.Equal(t, 2, f.mock.fileCalls)
	require.Zero(t, f.mock.importCalls)
	require.NoDirExists(t, filepath.Join(f.importDir, f.releaseName))

	parse := f.postJSON(t, "/api/parse", f.packPayload())
	require.Equal(t, domain.StatusSuccessfulHardlink.Code(), parse.Code)
	require.Equal(t, 1, f.mock.torrentCalls, "parse must not reread the inventory on a plan hit")
	require.Equal(t, 2, f.mock.fileCalls, "parse must not reread file details on a plan hit")
	require.Equal(t, 1, f.mock.importCalls)
	for episode := 1; episode <= 2; episode++ {
		episodeFile := fmt.Sprintf("Lifecycle.S01E%02d.1080p.WEB-DL.H.264-RlsGrp.mkv", episode)
		require.FileExists(t, filepath.Join(f.importDir, f.releaseName, episodeFile))
	}
}

func TestPackCoverageAcceptsMp4EpisodesAndIgnoresExtraVideos(t *testing.T) {
	f := newProcessorHTTPFixture(t, 1, 1, 1)
	episodeRelease := "Lifecycle.S01E01.1080p.WEB-DL.H.264-RlsGrp"
	episodeFile := episodeRelease + ".mp4"
	writeEpisode(t, filepath.Join(f.sourceDir, episodeFile))
	f.mock.filesByHash["ep1"] = []torrentclient.File{{Name: episodeFile, Size: 1}}

	infoBytes, err := bencode.Marshal(metainfo.Info{
		Name:        f.releaseName,
		PieceLength: 256 * 1024,
		Files: []metainfo.FileInfo{
			{Path: []string{episodeFile}, Length: 1},
			{Path: []string{"Extras", "Interview.1080p.WEB-DL.mp4"}, Length: 1},
			{Path: []string{"Sample", episodeRelease + "-SAMPLE.mp4"}, Length: 1},
		},
	})
	require.NoError(t, err)
	var torrent bytes.Buffer
	require.NoError(t, (&metainfo.MetaInfo{InfoBytes: infoBytes}).Write(&torrent))
	f.torrent = torrent.Bytes()

	res := f.postJSON(t, "/api/pack", f.packPayload())
	require.Equal(t, domain.StatusSuccessfulMatch.Code(), res.Code)
	require.Equal(t, 1, f.mock.fileCalls)
	require.Zero(t, f.mock.importCalls)

	res = f.postJSON(t, "/api/parse", f.packPayload())
	require.Equal(t, domain.StatusSuccessfulHardlink.Code(), res.Code)
	require.Equal(t, 1, f.mock.importCalls)
	require.FileExists(t, filepath.Join(f.importDir, f.releaseName, episodeFile))
	require.NoFileExists(t, filepath.Join(f.importDir, f.releaseName, "Extras", "Interview.1080p.WEB-DL.mp4"))
}

func TestHTTPFailureContractsDoNotMutateClientOrFilesystem(t *testing.T) {
	t.Run("authentication", func(t *testing.T) {
		f := newProcessorHTTPFixture(t, 1, 1, 0)
		res := f.postRaw(t, "/api/candidate", []byte(`{"name":"Lifecycle.S01.1080p.WEB-DL.H.264-RlsGrp"}`), "")
		require.Equal(t, stdhttp.StatusUnauthorized, res.Code)
		require.Zero(t, f.mock.torrentCalls)
		require.Zero(t, f.mock.importCalls)
	})

	t.Run("invalid JSON", func(t *testing.T) {
		f := newProcessorHTTPFixture(t, 1, 1, 0)
		res := f.postRaw(t, "/api/pack", []byte(`{"name":`), processorTestToken)
		require.Equal(t, domain.StatusDecodingError.Code(), res.Code)
		require.Zero(t, f.mock.torrentCalls)
		require.Zero(t, f.mock.importCalls)
	})

	t.Run("unknown client", func(t *testing.T) {
		f := newProcessorHTTPFixture(t, 1, 1, 0)
		payload := f.packPayload()
		payload["clientname"] = "missing"
		res := f.postJSON(t, "/api/pack", payload)
		require.Equal(t, domain.StatusClientNotFound.Code(), res.Code)
		require.Zero(t, f.mock.torrentCalls)
		require.Zero(t, f.mock.importCalls)
	})

	t.Run("pack missing torrent bytes", func(t *testing.T) {
		f := newProcessorHTTPFixture(t, 1, 1, 0)
		res := f.postJSON(t, "/api/pack", map[string]any{"name": f.releaseName, "clientname": "default"})
		require.Equal(t, domain.StatusTorrentBytesError.Code(), res.Code)
		require.Zero(t, f.mock.torrentCalls)
		require.Zero(t, f.mock.importCalls)
	})

	t.Run("parse missing torrent bytes", func(t *testing.T) {
		f := newProcessorHTTPFixture(t, 1, 1, 0)
		res := f.postJSON(t, "/api/parse", map[string]any{"name": f.releaseName, "clientname": "default"})
		require.Equal(t, domain.StatusTorrentBytesError.Code(), res.Code)
		require.Zero(t, f.mock.torrentCalls)
		require.Zero(t, f.mock.importCalls)
	})

	t.Run("invalid base64", func(t *testing.T) {
		f := newProcessorHTTPFixture(t, 1, 1, 0)
		payload := f.packPayload()
		payload["torrent"] = "%%%"
		res := f.postJSON(t, "/api/pack", payload)
		require.Equal(t, domain.StatusDecodeTorrentBytesError.Code(), res.Code)
		require.Equal(t, 1, f.mock.torrentCalls)
		require.Zero(t, f.mock.fileCalls)
		require.Zero(t, f.mock.importCalls)
	})

	t.Run("malformed torrent metadata", func(t *testing.T) {
		f := newProcessorHTTPFixture(t, 1, 1, 0)
		payload := f.packPayload()
		payload["torrent"] = base64.StdEncoding.EncodeToString([]byte("not-a-torrent"))
		res := f.postJSON(t, "/api/pack", payload)
		require.Equal(t, domain.StatusParseTorrentInfoError.Code(), res.Code)
		require.Equal(t, 1, f.mock.torrentCalls)
		require.Zero(t, f.mock.fileCalls)
		require.Zero(t, f.mock.importCalls)
	})

	t.Run("torrent without episode files", func(t *testing.T) {
		f := newProcessorHTTPFixture(t, 1, 1, 0)
		infoBytes, err := bencode.Marshal(metainfo.Info{
			Name:        f.releaseName,
			PieceLength: 256 * 1024,
			Files:       []metainfo.FileInfo{{Path: []string{"readme.nfo"}, Length: 1}},
		})
		require.NoError(t, err)
		var torrent bytes.Buffer
		require.NoError(t, (&metainfo.MetaInfo{InfoBytes: infoBytes}).Write(&torrent))
		payload := f.packPayload()
		payload["torrent"] = base64.StdEncoding.EncodeToString(torrent.Bytes())

		res := f.postJSON(t, "/api/pack", payload)
		require.Equal(t, domain.StatusGetEpisodesError.Code(), res.Code)
		require.Zero(t, f.mock.fileCalls)
		require.Zero(t, f.mock.importCalls)
	})

	t.Run("below threshold", func(t *testing.T) {
		f := newProcessorHTTPFixture(t, 4, 2, 0.75)
		res := f.postJSON(t, "/api/pack", f.packPayload())
		require.Equal(t, domain.StatusBelowThreshold.Code(), res.Code)
		require.Zero(t, f.mock.importCalls)
		require.NoDirExists(t, filepath.Join(f.importDir, f.releaseName))
	})
}

func TestParseRebuildsUnavailableOrInvalidPlansThroughHTTP(t *testing.T) {
	t.Run("missing after restart", func(t *testing.T) {
		f := newProcessorHTTPFixture(t, 1, 1, 0)
		require.Equal(t, domain.StatusSuccessfulMatch.Code(), f.postJSON(t, "/api/pack", f.packPayload()).Code)

		planMap = xsync.NewMapOf[importPlanCacheKey, cachedImportPlan]()
		entryMap = xsync.NewMapOf[string, *entryCache]()
		res := f.postJSON(t, "/api/parse", f.packPayload())

		require.Equal(t, domain.StatusSuccessfulHardlink.Code(), res.Code)
		require.Equal(t, 2, f.mock.torrentCalls, "restart must rebuild the client inventory")
		require.Equal(t, 2, f.mock.fileCalls, "restart must rebuild the exact plan")
		require.Equal(t, 1, f.mock.importCalls)
	})

	t.Run("expired", func(t *testing.T) {
		f := newProcessorHTTPFixture(t, 1, 1, 0)
		require.Equal(t, domain.StatusSuccessfulMatch.Code(), f.postJSON(t, "/api/pack", f.packPayload()).Code)

		hashes, err := torrents.InfoHashes(f.torrent)
		require.NoError(t, err)
		key := importPlanKey("default", hashes)
		cached, ok := planMap.Load(key)
		require.True(t, ok)
		cached.expiresAt = time.Now().Add(-time.Second)
		planMap.Store(key, cached)

		res := f.postJSON(t, "/api/parse", f.packPayload())
		require.Equal(t, domain.StatusSuccessfulHardlink.Code(), res.Code)
		require.Equal(t, 1, f.mock.torrentCalls, "an expired plan can still reuse the inventory cache")
		require.Equal(t, 2, f.mock.fileCalls, "an expired plan must rebuild file mappings")
		require.Equal(t, 1, f.mock.importCalls)
	})

	t.Run("release name changed", func(t *testing.T) {
		f := newProcessorHTTPFixture(t, 1, 1, 0)
		require.Equal(t, domain.StatusSuccessfulMatch.Code(), f.postJSON(t, "/api/pack", f.packPayload()).Code)

		payload := f.packPayload()
		payload["name"] = "Different.S01.1080p.WEB-DL.H.264-RlsGrp"
		res := f.postJSON(t, "/api/parse", payload)

		require.Equal(t, domain.StatusNoMatches.Code(), res.Code)
		require.Zero(t, f.mock.importCalls, "a plan accepted for another release must not import")
	})

	t.Run("torrent identity changed", func(t *testing.T) {
		f := newProcessorHTTPFixture(t, 1, 1, 0)
		require.Equal(t, domain.StatusSuccessfulMatch.Code(), f.postJSON(t, "/api/pack", f.packPayload()).Code)
		changedTorrent, err := torrents.TorrentFromRls(f.releaseName, 2)
		require.NoError(t, err)

		payload := f.packPayload()
		payload["torrent"] = base64.StdEncoding.EncodeToString(changedTorrent)
		res := f.postJSON(t, "/api/parse", payload)

		require.Equal(t, domain.StatusSuccessfulHardlink.Code(), res.Code)
		require.Equal(t, 2, f.mock.fileCalls, "a different info hash must rebuild the exact plan")
		require.Equal(t, 1, f.mock.importCalls)
	})

	t.Run("fuzzy matching changed", func(t *testing.T) {
		f := newProcessorHTTPFixture(t, 1, 1, 0)
		require.Equal(t, domain.StatusSuccessfulMatch.Code(), f.postJSON(t, "/api/pack", f.packPayload()).Code)

		changed := f.config.Snapshot()
		changed.FuzzyMatching.SkipYearCompare = true
		f.config.Store(changed)
		res := f.postJSON(t, "/api/parse", f.packPayload())

		require.Equal(t, domain.StatusSuccessfulHardlink.Code(), res.Code)
		require.Equal(t, 2, f.mock.torrentCalls, "matching changes must refresh the inventory index")
		require.Equal(t, 2, f.mock.fileCalls, "matching changes must rebuild the exact plan")
		require.Equal(t, 1, f.mock.importCalls)
	})

	t.Run("client configuration changed", func(t *testing.T) {
		f := newProcessorHTTPFixture(t, 1, 1, 0)
		require.Equal(t, domain.StatusSuccessfulMatch.Code(), f.postJSON(t, "/api/pack", f.packPayload()).Code)

		changed := f.config.Snapshot()
		changedClient := cloneClientConfig(*changed.Clients["default"])
		changedClient.Import.Category = "changed"
		changed.Clients = map[string]*domain.Client{"default": &changedClient}
		f.config.Store(changed)
		clientMap.Store("default", cachedTorrentClient{config: cloneClientConfig(changedClient), client: f.mock})

		res := f.postJSON(t, "/api/parse", f.packPayload())
		require.Equal(t, domain.StatusSuccessfulHardlink.Code(), res.Code)
		require.Equal(t, 2, f.mock.torrentCalls, "client changes must refresh the inventory")
		require.Equal(t, 2, f.mock.fileCalls, "client changes must rebuild the exact plan")
		require.Equal(t, 1, f.mock.importCalls)
	})
}

func TestParseMutationFailuresRemainSafeAndRetryable(t *testing.T) {
	t.Run("import failure retains plan for retry", func(t *testing.T) {
		f := newProcessorHTTPFixture(t, 1, 1, 0)
		require.Equal(t, domain.StatusSuccessfulMatch.Code(), f.postJSON(t, "/api/pack", f.packPayload()).Code)
		f.mock.importErr = errors.New("import failed")

		first := f.postJSON(t, "/api/parse", f.packPayload())
		require.Equal(t, domain.StatusAddTorrentError.Code(), first.Code)
		require.Equal(t, 1, f.mock.importCalls)
		require.FileExists(t, filepath.Join(f.importDir, f.releaseName, "Lifecycle.S01E01.1080p.WEB-DL.H.264-RlsGrp.mkv"))

		f.mock.importErr = nil
		second := f.postJSON(t, "/api/parse", f.packPayload())
		require.Equal(t, domain.StatusSuccessfulHardlink.Code(), second.Code)
		require.Equal(t, 1, f.mock.torrentCalls, "retry must retain the accepted inventory")
		require.Equal(t, 1, f.mock.fileCalls, "retry must retain the accepted plan")
		require.Equal(t, 2, f.mock.importCalls)
	})

	t.Run("one conflicting target does not block safe links", func(t *testing.T) {
		f := newProcessorHTTPFixture(t, 2, 2, 0)
		require.Equal(t, domain.StatusSuccessfulMatch.Code(), f.postJSON(t, "/api/pack", f.packPayload()).Code)

		targetDir := filepath.Join(f.importDir, f.releaseName)
		require.NoError(t, os.MkdirAll(targetDir, 0o755))
		conflictingTarget := filepath.Join(targetDir, "Lifecycle.S01E01.1080p.WEB-DL.H.264-RlsGrp.mkv")
		require.NoError(t, os.WriteFile(conflictingTarget, []byte("conflict"), 0o644))

		res := f.postJSON(t, "/api/parse", f.packPayload())
		require.Equal(t, domain.StatusSuccessfulHardlink.Code(), res.Code)
		require.Equal(t, 1, f.mock.importCalls)
		contents, err := os.ReadFile(conflictingTarget)
		require.NoError(t, err)
		require.Equal(t, []byte("conflict"), contents, "an unrelated target must never be overwritten")
		require.FileExists(t, filepath.Join(targetDir, "Lifecycle.S01E02.1080p.WEB-DL.H.264-RlsGrp.mkv"))
	})

	t.Run("smart mode retains safe links when achieved coverage is below threshold", func(t *testing.T) {
		f := newProcessorHTTPFixture(t, 3, 3, 0.75)
		require.Equal(t, domain.StatusSuccessfulMatch.Code(), f.postJSON(t, "/api/pack", f.packPayload()).Code)

		targetDir := filepath.Join(f.importDir, f.releaseName)
		require.NoError(t, os.MkdirAll(targetDir, 0o755))
		existingSource := filepath.Join(f.sourceDir, "Lifecycle.S01E01.1080p.WEB-DL.H.264-RlsGrp.mkv")
		existingTarget := filepath.Join(targetDir, "Lifecycle.S01E01.1080p.WEB-DL.H.264-RlsGrp.mkv")
		require.NoError(t, os.Link(existingSource, existingTarget))
		conflictingTarget := filepath.Join(targetDir, "Lifecycle.S01E02.1080p.WEB-DL.H.264-RlsGrp.mkv")
		require.NoError(t, os.WriteFile(conflictingTarget, []byte("conflict"), 0o644))

		res := f.postJSON(t, "/api/parse", f.packPayload())
		require.Equal(t, domain.StatusBelowThreshold.Code(), res.Code)
		require.Zero(t, f.mock.importCalls)
		existingSourceInfo, err := os.Stat(existingSource)
		require.NoError(t, err)
		existingTargetInfo, err := os.Stat(existingTarget)
		require.NoError(t, err)
		require.True(t, os.SameFile(existingSourceInfo, existingTargetInfo), "a pre-existing valid link must remain")
		contents, err := os.ReadFile(conflictingTarget)
		require.NoError(t, err)
		require.Equal(t, []byte("conflict"), contents, "an unrelated target must never be overwritten")
		require.FileExists(t, filepath.Join(targetDir, "Lifecycle.S01E03.1080p.WEB-DL.H.264-RlsGrp.mkv"), "a successful link must remain safe for retry")
	})

	t.Run("all conflicting targets prevent import", func(t *testing.T) {
		f := newProcessorHTTPFixture(t, 1, 1, 0)
		require.Equal(t, domain.StatusSuccessfulMatch.Code(), f.postJSON(t, "/api/pack", f.packPayload()).Code)

		targetDir := filepath.Join(f.importDir, f.releaseName)
		require.NoError(t, os.MkdirAll(targetDir, 0o755))
		conflictingTarget := filepath.Join(targetDir, "Lifecycle.S01E01.1080p.WEB-DL.H.264-RlsGrp.mkv")
		require.NoError(t, os.WriteFile(conflictingTarget, []byte("conflict"), 0o644))

		res := f.postJSON(t, "/api/parse", f.packPayload())
		require.Equal(t, domain.StatusFailedHardlink.Code(), res.Code)
		require.Zero(t, f.mock.importCalls)
		contents, err := os.ReadFile(conflictingTarget)
		require.NoError(t, err)
		require.Equal(t, []byte("conflict"), contents)
	})
}
