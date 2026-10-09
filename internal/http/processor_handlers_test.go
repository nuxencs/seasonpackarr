// Copyright (c) 2023 - 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package http

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	stdhttp "net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nuxencs/seasonpackarr/internal/domain"
	"github.com/nuxencs/seasonpackarr/internal/logger"
	"github.com/nuxencs/seasonpackarr/internal/logger/loggertest"
	"github.com/nuxencs/seasonpackarr/internal/torrentclient"
	"github.com/nuxencs/seasonpackarr/internal/torrents"

	"github.com/autobrr/go-torrent/bencode"
	"github.com/autobrr/go-torrent/metainfo"
	"github.com/gin-gonic/gin"
	"github.com/puzpuzpuz/xsync/v3"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"
)

func TestCandidateEndpoint_LogsStructuredCompatibilityMismatch(t *testing.T) {
	previousLevel := zerolog.GlobalLevel()
	zerolog.SetGlobalLevel(zerolog.TraceLevel)
	t.Cleanup(func() { zerolog.SetGlobalLevel(previousLevel) })

	log := loggertest.New()
	f := newProcessorHTTPFixtureWithLogger(t, 2, 2, 0.5, log)
	f.torrentClient.torrents[0].Name = "Lifecycle.S01E01.1080p.BluRay.H.264-RlsGrp"
	cfg := f.config.Snapshot()
	cfg.FuzzyMatching.SimplifyWebCompare = true
	f.config.Store(cfg)

	response := f.postJSON(t, "/api/candidate", map[string]any{
		"name":       f.releaseName,
		"clientname": "default",
	})
	require.Equal(t, domain.StatusSuccessfulMatch.Code(), response.Code)

	logs := log.Events(t)
	mismatch := logs.Require(t, "client release is not compatible")
	require.Equal(t, "compatibility_mismatch", mismatch["reason"])
	require.Equal(t, "source", mismatch["field"])
	require.Equal(t, "WEB-DL", mismatch["want"])
	require.Equal(t, "BluRay", mismatch["got"])
	require.Equal(t, "Lifecycle.S01E01.1080p.BluRay.H.264-RlsGrp", mismatch["client_release"])
}

func TestCandidateEndpoint_LogsEachMatchingClientReleaseAtDebug(t *testing.T) {
	previousLevel := zerolog.GlobalLevel()
	zerolog.SetGlobalLevel(zerolog.TraceLevel)
	t.Cleanup(func() { zerolog.SetGlobalLevel(previousLevel) })

	log := loggertest.New()
	f := newProcessorHTTPFixtureWithLogger(t, 2, 2, 0.5, log)

	response := f.postJSON(t, "/api/candidate", map[string]any{
		"name":       f.releaseName,
		"clientname": "default",
	})
	require.Equal(t, domain.StatusSuccessfulMatch.Code(), response.Code)

	logs := log.Events(t)
	for episode := 1; episode <= 2; episode++ {
		clientRelease := fmt.Sprintf("Lifecycle.S01E%02d.1080p.WEB-DL.H.264-RlsGrp", episode)
		matched := logs.RequireField(t, "client release passed candidate gate", "client_release", clientRelease)
		require.Equal(t, "debug", matched["level"])
	}
}

func TestMatchEndpoint_LogsUnmatchedTorrentEpisode(t *testing.T) {
	previousLevel := zerolog.GlobalLevel()
	zerolog.SetGlobalLevel(zerolog.TraceLevel)
	t.Cleanup(func() { zerolog.SetGlobalLevel(previousLevel) })

	log := loggertest.New()
	f := newProcessorHTTPFixtureWithLogger(t, 2, 1, 0.5, log)

	response := f.postJSON(t, "/api/match", f.packPayload())
	require.Equal(t, domain.StatusSuccessfulMatch.Code(), response.Code)

	logs := log.Events(t)
	unmatched := logs.Require(t, "torrent episode is not reusable")
	require.Equal(t, "Lifecycle.S01E02.1080p.WEB-DL.H.264-RlsGrp.mkv", filepath.Base(unmatched["torrent_path"].(string)))
	require.Equal(t, "source_episode_not_found", unmatched["reason"])

	summary := logs.Require(t, "season pack import plan built")
	require.Equal(t, float64(1), summary["reusable_episodes"])
	require.Equal(t, float64(2), summary["total_episodes"])
	require.Equal(t, float64(1), summary["unmatched_episodes"])
	require.Equal(t, float64(50), summary["coverage_percent"])
	require.Equal(t, float64(50), summary["threshold_percent"])
}

func TestMatchEndpoint_LogsReasonWhenNoEpisodeIsReusable(t *testing.T) {
	previousLevel := zerolog.GlobalLevel()
	zerolog.SetGlobalLevel(zerolog.TraceLevel)
	t.Cleanup(func() { zerolog.SetGlobalLevel(previousLevel) })

	log := loggertest.New()
	f := newProcessorHTTPFixtureWithLogger(t, 1, 1, 0, log)
	f.torrentClient.filesByHash["ep1"][0].Size = 2

	response := f.postJSON(t, "/api/match", f.packPayload())
	require.Equal(t, domain.StatusFailedMatchToTorrentEps.Code(), response.Code)

	logs := log.Events(t)
	unmatched := logs.Require(t, "torrent episode is not reusable")
	require.Equal(t, "compatibility_mismatch", unmatched["reason"])
	require.Equal(t, []any{map[string]any{
		"field": "size",
		"want":  float64(1),
		"got":   float64(2),
	}}, unmatched["mismatches"])
	rejection := logs.Require(t, "season pack rejected")
	require.Equal(t, "info", rejection["level"])
	require.Equal(t, "could not match episodes to files in pack", rejection["error"])
}

func TestMatchEndpoint_LogsBelowThresholdAsExpectedRejection(t *testing.T) {
	previousLevel := zerolog.GlobalLevel()
	zerolog.SetGlobalLevel(zerolog.TraceLevel)
	t.Cleanup(func() { zerolog.SetGlobalLevel(previousLevel) })

	log := loggertest.New()
	f := newProcessorHTTPFixtureWithLogger(t, 2, 1, 0.75, log)

	response := f.postJSON(t, "/api/match", f.packPayload())
	require.Equal(t, domain.StatusBelowThreshold.Code(), response.Code)

	logs := log.Events(t)
	rejection := logs.Require(t, "season pack rejected")
	require.Equal(t, "info", rejection["level"])
	require.Equal(t, "number of matches below threshold", rejection["error"])
}

func TestImportEndpoint_LogsClientImportStageTiming(t *testing.T) {
	previousLevel := zerolog.GlobalLevel()
	zerolog.SetGlobalLevel(zerolog.TraceLevel)
	t.Cleanup(func() { zerolog.SetGlobalLevel(previousLevel) })

	log := loggertest.New()
	f := newProcessorHTTPFixtureWithLogger(t, 2, 2, 0.75, log)
	f.torrentClient.importReport = torrentclient.ImportReport{Stages: []torrentclient.ImportStageReport{
		{Stage: torrentclient.ImportStageConfig, Duration: 2 * time.Millisecond},
		{Stage: torrentclient.ImportStageAdd, Duration: 12 * time.Millisecond},
		{Stage: torrentclient.ImportStageFind, Duration: 800 * time.Millisecond},
		{Stage: torrentclient.ImportStageRecheck, Duration: 35 * time.Second},
		{Stage: torrentclient.ImportStageResume, Duration: 3 * time.Millisecond},
	}}

	matchResponse := f.postJSON(t, "/api/match", f.packPayload())
	require.Equal(t, domain.StatusSuccessfulMatch.Code(), matchResponse.Code)
	log.Reset()

	importResponse := f.postJSON(t, "/api/import", f.packPayload())
	require.Equal(t, domain.StatusSuccessfulHardlink.Code(), importResponse.Code)

	logs := log.Events(t)
	plan := logs.Require(t, "import plan resolved")
	require.Equal(t, "cache", plan["plan_source"])
	require.Contains(t, plan, "duration_ms")

	destination := logs.Require(t, "import destination resolved")
	require.Equal(t, "info", destination["level"])
	require.Equal(t, true, destination["successful"])
	require.Contains(t, destination, "duration_ms")

	hardlinks := logs.Require(t, "hardlink stage completed")
	require.Equal(t, float64(2), hardlinks["linked_episodes"])
	require.Contains(t, hardlinks, "duration_ms")
	for _, event := range logs {
		message, _ := event["message"].(string)
		require.False(t, strings.HasPrefix(message, "hardlinked "), "legacy hardlink summary must be absent")
	}

	recheck := logs.RequireField(t, "torrent client import stage finished", "stage", "recheck")
	require.Equal(t, float64(35_000), recheck["duration_ms"])

	completed := logs.Require(t, "season pack import completed")
	require.Contains(t, completed, "total_duration_ms")
}

func TestImportEndpoint_LogsTotalTimingOnImportFailure(t *testing.T) {
	previousLevel := zerolog.GlobalLevel()
	zerolog.SetGlobalLevel(zerolog.TraceLevel)
	t.Cleanup(func() { zerolog.SetGlobalLevel(previousLevel) })

	log := loggertest.New()
	f := newProcessorHTTPFixtureWithLogger(t, 1, 1, 0, log)
	f.torrentClient.importReport = torrentclient.ImportReport{Stages: []torrentclient.ImportStageReport{
		{Stage: torrentclient.ImportStageConfig, Duration: time.Millisecond},
		{Stage: torrentclient.ImportStageAdd, Duration: 12 * time.Millisecond},
	}}
	f.torrentClient.importErr = &torrentclient.ImportError{
		Stage: torrentclient.ImportStageAdd,
		Err:   errors.New("client rejected torrent"),
	}

	matchResponse := f.postJSON(t, "/api/match", f.packPayload())
	require.Equal(t, domain.StatusSuccessfulMatch.Code(), matchResponse.Code)
	log.Reset()

	importResponse := f.postJSON(t, "/api/import", f.packPayload())
	require.Equal(t, domain.StatusAddTorrentError.Code(), importResponse.Code)

	logs := log.Events(t)
	completed := logs.Require(t, "season pack import completed")
	require.Equal(t, false, completed["successful"])
	require.Equal(t, float64(domain.StatusAddTorrentError), completed["status_code"])
	require.Contains(t, completed, "total_duration_ms")
}

func TestImportEndpoint_LogsFailedDestinationResolutionTiming(t *testing.T) {
	previousLevel := zerolog.GlobalLevel()
	zerolog.SetGlobalLevel(zerolog.TraceLevel)
	t.Cleanup(func() { zerolog.SetGlobalLevel(previousLevel) })

	log := loggertest.New()
	f := newProcessorHTTPFixtureWithLogger(t, 1, 1, 0, log)

	matchResponse := f.postJSON(t, "/api/match", f.packPayload())
	require.Equal(t, domain.StatusSuccessfulMatch.Code(), matchResponse.Code)
	f.torrentClient.importRootErr = &torrentclient.ImportError{
		Stage: torrentclient.ImportStageConfig,
		Err:   errors.New("destination unavailable"),
	}
	log.Reset()

	importResponse := f.postJSON(t, "/api/import", f.packPayload())
	require.Equal(t, domain.StatusImportConfigError.Code(), importResponse.Code)

	logs := log.Events(t)
	destination := logs.Require(t, "import destination resolved")
	require.Equal(t, "info", destination["level"])
	require.Equal(t, false, destination["successful"])
	require.Contains(t, destination, "duration_ms")
	require.Equal(t, "destination unavailable", destination["error"])
}

func TestImportEndpoint_LogsFailedPlanRebuildTiming(t *testing.T) {
	previousLevel := zerolog.GlobalLevel()
	zerolog.SetGlobalLevel(zerolog.TraceLevel)
	t.Cleanup(func() { zerolog.SetGlobalLevel(previousLevel) })

	log := loggertest.New()
	f := newProcessorHTTPFixtureWithLogger(t, 1, 1, 0, log)
	f.torrentClient.filesByHash["ep1"][0].Size = 2

	importResponse := f.postJSON(t, "/api/import", f.packPayload())
	require.Equal(t, domain.StatusFailedMatchToTorrentEps.Code(), importResponse.Code)

	logs := log.Events(t)
	plan := logs.Require(t, "import plan resolved")
	require.Equal(t, "rebuilt", plan["plan_source"])
	require.Equal(t, false, plan["successful"])
	require.Contains(t, plan, "duration_ms")

	completed := logs.Require(t, "season pack import completed")
	require.Equal(t, false, completed["successful"])
	require.Contains(t, completed, "total_duration_ms")
}

func TestEndpoints_ImportReusesAcceptedPlan(t *testing.T) {
	f := newProcessorHTTPFixture(t, 2, 2, 0.75)

	candidate := f.postJSON(t, "/api/candidate", map[string]any{
		"name":       f.releaseName,
		"clientname": "default",
	})
	require.Equal(t, domain.StatusSuccessfulMatch.Code(), candidate.Code)
	require.Equal(t, 1, f.torrentClient.torrentCalls)
	require.Zero(t, f.torrentClient.fileCalls)
	require.Zero(t, f.torrentClient.importCalls)

	matchResponse := f.postJSON(t, "/api/match", f.packPayload())
	require.Equal(t, domain.StatusSuccessfulMatch.Code(), matchResponse.Code)
	require.Equal(t, 1, f.torrentClient.torrentCalls, "match must reuse the candidate inventory")
	require.Equal(t, 2, f.torrentClient.fileCalls)
	require.Zero(t, f.torrentClient.importCalls)
	require.NoDirExists(t, filepath.Join(f.importDir, f.releaseName))

	importResponse := f.postJSON(t, "/api/import", f.packPayload())
	require.Equal(t, domain.StatusSuccessfulHardlink.Code(), importResponse.Code)
	require.Equal(t, 1, f.torrentClient.torrentCalls, "import must not reread the inventory on a plan hit")
	require.Equal(t, 2, f.torrentClient.fileCalls, "import must not reread file details on a plan hit")
	require.Equal(t, 1, f.torrentClient.importCalls)
	for episode := 1; episode <= 2; episode++ {
		episodeFile := fmt.Sprintf("Lifecycle.S01E%02d.1080p.WEB-DL.H.264-RlsGrp.mkv", episode)
		require.FileExists(t, filepath.Join(f.importDir, f.releaseName, episodeFile))
	}
}

func TestImportEndpoint_RefreshesPlanWhenSourceMoves(t *testing.T) {
	previousLevel := zerolog.GlobalLevel()
	zerolog.SetGlobalLevel(zerolog.TraceLevel)
	t.Cleanup(func() { zerolog.SetGlobalLevel(previousLevel) })

	log := loggertest.New()
	f := newProcessorHTTPFixtureWithLogger(t, 2, 2, 1, log)

	matchResponse := f.postJSON(t, "/api/match", f.packPayload())
	require.Equal(t, domain.StatusSuccessfulMatch.Code(), matchResponse.Code)

	episodeFile := "Lifecycle.S01E02.1080p.WEB-DL.H.264-RlsGrp.mkv"
	movedDir := filepath.Join(filepath.Dir(f.sourceDir), "source-completed")
	require.NoError(t, os.MkdirAll(movedDir, 0o755))
	require.NoError(t, os.Rename(
		filepath.Join(f.sourceDir, episodeFile),
		filepath.Join(movedDir, episodeFile),
	))
	f.torrentClient.torrents[1].SavePath = movedDir

	importResponse := f.postJSON(t, "/api/import", f.packPayload())
	require.Equal(t, domain.StatusSuccessfulHardlink.Code(), importResponse.Code, importResponse.Body.String())
	require.Equal(t, 2, f.torrentClient.torrentCalls, "import must refresh the stale inventory once")
	require.Equal(t, 4, f.torrentClient.fileCalls, "import must rebuild the stale plan once")
	for episode := 1; episode <= 2; episode++ {
		fileName := fmt.Sprintf("Lifecycle.S01E%02d.1080p.WEB-DL.H.264-RlsGrp.mkv", episode)
		require.FileExists(t, filepath.Join(f.importDir, f.releaseName, fileName))
	}
	require.True(t, f.torrentClient.importCalled)

	logs := log.Events(t)
	missing := logs.RequireField(t, "hardlink source is missing",
		"source",
		filepath.Join(f.sourceDir, episodeFile),
	)
	require.Equal(t, "warn", missing["level"])
	refresh := logs.Require(t, "import plan refreshed after missing hardlink source")
	require.Equal(t, true, refresh["successful"])
	hardlinks := logs.Require(t, "hardlink stage completed")
	require.Equal(t, float64(2), hardlinks["linked_episodes"])
}

func TestImportEndpoint_RefreshesAtMostOnceWhenSourceMovesAgain(t *testing.T) {
	previousLevel := zerolog.GlobalLevel()
	zerolog.SetGlobalLevel(zerolog.TraceLevel)
	t.Cleanup(func() { zerolog.SetGlobalLevel(previousLevel) })

	log := loggertest.New()
	f := newProcessorHTTPFixtureWithLogger(t, 2, 2, 1, log)

	matchResponse := f.postJSON(t, "/api/match", f.packPayload())
	require.Equal(t, domain.StatusSuccessfulMatch.Code(), matchResponse.Code)

	episodeFile := "Lifecycle.S01E02.1080p.WEB-DL.H.264-RlsGrp.mkv"
	movedDir := filepath.Join(filepath.Dir(f.sourceDir), "source-completed")
	movedAgainDir := filepath.Join(filepath.Dir(f.sourceDir), "source-moved-again")
	require.NoError(t, os.MkdirAll(movedDir, 0o755))
	require.NoError(t, os.MkdirAll(movedAgainDir, 0o755))
	require.NoError(t, os.Rename(
		filepath.Join(f.sourceDir, episodeFile),
		filepath.Join(movedDir, episodeFile),
	))
	f.torrentClient.torrents[1].SavePath = movedDir
	f.torrentClient.afterGetFiles = func() {
		require.NoError(t, os.Rename(
			filepath.Join(movedDir, episodeFile),
			filepath.Join(movedAgainDir, episodeFile),
		))
		f.torrentClient.afterGetFiles = nil
	}

	importResponse := f.postJSON(t, "/api/import", f.packPayload())
	require.Equal(t, domain.StatusBelowThreshold.Code(), importResponse.Code, importResponse.Body.String())
	require.Equal(t, 2, f.torrentClient.torrentCalls, "import must refresh the inventory only once")
	require.Equal(t, 4, f.torrentClient.fileCalls, "import must rebuild the plan only once")
	require.Zero(t, f.torrentClient.importCalls)

	refreshCount := 0
	missingCount := 0
	for _, event := range log.Events(t) {
		switch event["message"] {
		case "import plan refreshed after missing hardlink source":
			refreshCount++
		case "hardlink source is missing":
			missingCount++
		}
	}
	require.Equal(t, 1, refreshCount)
	require.Equal(t, 2, missingCount)
}

func TestImportEndpoint_RejectsRefreshedPlanBelowThreshold(t *testing.T) {
	f := newProcessorHTTPFixture(t, 2, 2, 1)

	matchResponse := f.postJSON(t, "/api/match", f.packPayload())
	require.Equal(t, domain.StatusSuccessfulMatch.Code(), matchResponse.Code)

	episodeFile := "Lifecycle.S01E02.1080p.WEB-DL.H.264-RlsGrp.mkv"
	require.NoError(t, os.Rename(
		filepath.Join(f.sourceDir, episodeFile),
		filepath.Join(filepath.Dir(f.sourceDir), episodeFile),
	))
	f.torrentClient.torrents = f.torrentClient.torrents[:1]

	importResponse := f.postJSON(t, "/api/import", f.packPayload())
	require.Equal(t, domain.StatusBelowThreshold.Code(), importResponse.Code, importResponse.Body.String())
	require.Equal(t, 2, f.torrentClient.torrentCalls)
	require.Equal(t, 3, f.torrentClient.fileCalls)
	require.Zero(t, f.torrentClient.importCalls)
	require.FileExists(t, filepath.Join(
		f.importDir,
		f.releaseName,
		"Lifecycle.S01E01.1080p.WEB-DL.H.264-RlsGrp.mkv",
	))
}

func TestEndpoints_AcceptMP4EpisodesAndIgnoreExtraVideos(t *testing.T) {
	f := newProcessorHTTPFixture(t, 1, 1, 1)
	episodeRelease := "Lifecycle.S01E01.1080p.WEB-DL.H.264-RlsGrp"
	episodeFile := episodeRelease + ".mp4"
	writeEpisode(t, filepath.Join(f.sourceDir, episodeFile))
	f.torrentClient.filesByHash["ep1"] = []torrentclient.File{{Name: episodeFile, Size: 1}}

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

	res := f.postJSON(t, "/api/match", f.packPayload())
	require.Equal(t, domain.StatusSuccessfulMatch.Code(), res.Code)
	require.Equal(t, 1, f.torrentClient.fileCalls)
	require.Zero(t, f.torrentClient.importCalls)

	res = f.postJSON(t, "/api/import", f.packPayload())
	require.Equal(t, domain.StatusSuccessfulHardlink.Code(), res.Code)
	require.Equal(t, 1, f.torrentClient.importCalls)
	require.FileExists(t, filepath.Join(f.importDir, f.releaseName, episodeFile))
	require.NoFileExists(t, filepath.Join(f.importDir, f.releaseName, "Extras", "Interview.1080p.WEB-DL.mp4"))
}

func TestEndpoints_FailuresDoNotMutateClientOrFilesystem(t *testing.T) {
	t.Run("authentication", func(t *testing.T) {
		for _, path := range []string{"/api/candidate", "/api/match", "/api/import"} {
			t.Run(path, func(t *testing.T) {
				f := newProcessorHTTPFixture(t, 1, 1, 0)
				body, err := json.Marshal(f.packPayload())
				require.NoError(t, err)
				for _, token := range []string{"", "wrong-token"} {
					res := f.postRaw(t, path, body, token)
					require.Equal(t, stdhttp.StatusUnauthorized, res.Code)
				}
				require.Zero(t, f.torrentClient.torrentCalls)
				require.Zero(t, f.torrentClient.fileCalls)
				require.Zero(t, f.torrentClient.importCalls)
				entries, err := os.ReadDir(f.importDir)
				require.NoError(t, err)
				require.Empty(t, entries)
			})
		}
	})

	t.Run("invalid JSON", func(t *testing.T) {
		f := newProcessorHTTPFixture(t, 1, 1, 0)
		res := f.postRaw(t, "/api/match", []byte(`{"name":`), processorTestToken)
		require.Equal(t, domain.StatusDecodingError.Code(), res.Code)
		require.Zero(t, f.torrentClient.torrentCalls)
		require.Zero(t, f.torrentClient.importCalls)
	})

	t.Run("unknown client", func(t *testing.T) {
		f := newProcessorHTTPFixture(t, 1, 1, 0)
		payload := f.packPayload()
		payload["clientname"] = "missing"
		res := f.postJSON(t, "/api/match", payload)
		require.Equal(t, domain.StatusClientNotFound.Code(), res.Code)
		require.Zero(t, f.torrentClient.torrentCalls)
		require.Zero(t, f.torrentClient.importCalls)
	})

	t.Run("match missing torrent bytes", func(t *testing.T) {
		f := newProcessorHTTPFixture(t, 1, 1, 0)
		res := f.postJSON(t, "/api/match", map[string]any{"name": f.releaseName, "clientname": "default"})
		require.Equal(t, domain.StatusTorrentBytesError.Code(), res.Code)
		require.Zero(t, f.torrentClient.torrentCalls)
		require.Zero(t, f.torrentClient.importCalls)
	})

	t.Run("import missing torrent bytes", func(t *testing.T) {
		f := newProcessorHTTPFixture(t, 1, 1, 0)
		res := f.postJSON(t, "/api/import", map[string]any{"name": f.releaseName, "clientname": "default"})
		require.Equal(t, domain.StatusTorrentBytesError.Code(), res.Code)
		require.Zero(t, f.torrentClient.torrentCalls)
		require.Zero(t, f.torrentClient.importCalls)
	})

	t.Run("invalid base64", func(t *testing.T) {
		f := newProcessorHTTPFixture(t, 1, 1, 0)
		payload := f.packPayload()
		payload["torrent"] = "%%%"
		res := f.postJSON(t, "/api/match", payload)
		require.Equal(t, domain.StatusDecodeTorrentBytesError.Code(), res.Code)
		require.Equal(t, 1, f.torrentClient.torrentCalls)
		require.Zero(t, f.torrentClient.fileCalls)
		require.Zero(t, f.torrentClient.importCalls)
	})

	t.Run("malformed torrent metadata", func(t *testing.T) {
		f := newProcessorHTTPFixture(t, 1, 1, 0)
		payload := f.packPayload()
		payload["torrent"] = base64.StdEncoding.EncodeToString([]byte("not-a-torrent"))
		res := f.postJSON(t, "/api/match", payload)
		require.Equal(t, domain.StatusParseTorrentInfoError.Code(), res.Code)
		require.Equal(t, 1, f.torrentClient.torrentCalls)
		require.Zero(t, f.torrentClient.fileCalls)
		require.Zero(t, f.torrentClient.importCalls)
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

		res := f.postJSON(t, "/api/match", payload)
		require.Equal(t, domain.StatusGetEpisodesError.Code(), res.Code)
		require.Zero(t, f.torrentClient.fileCalls)
		require.Zero(t, f.torrentClient.importCalls)
	})

	t.Run("below threshold", func(t *testing.T) {
		f := newProcessorHTTPFixture(t, 4, 2, 0.75)
		res := f.postJSON(t, "/api/match", f.packPayload())
		require.Equal(t, domain.StatusBelowThreshold.Code(), res.Code)
		require.Zero(t, f.torrentClient.importCalls)
		require.NoDirExists(t, filepath.Join(f.importDir, f.releaseName))
	})
}

func TestEndpoints_RemovedWebhookRoutesReturnNotFound(t *testing.T) {
	for _, path := range []string{"/api/pack", "/api/parse"} {
		t.Run(path, func(t *testing.T) {
			f := newProcessorHTTPFixture(t, 1, 1, 0)
			res := f.postJSON(t, path, f.packPayload())
			require.Equal(t, stdhttp.StatusNotFound, res.Code)
			require.Empty(t, res.Header().Get("Location"))
			require.Zero(t, f.torrentClient.torrentCalls)
			require.Zero(t, f.torrentClient.fileCalls)
			require.Zero(t, f.torrentClient.importCalls)
			entries, err := os.ReadDir(f.importDir)
			require.NoError(t, err)
			require.Empty(t, entries)
		})
	}
}

func TestImportEndpoint_RebuildsUnavailableOrInvalidPlans(t *testing.T) {
	t.Run("missing after restart", func(t *testing.T) {
		f := newProcessorHTTPFixture(t, 1, 1, 0)
		require.Equal(t, domain.StatusSuccessfulMatch.Code(), f.postJSON(t, "/api/match", f.packPayload()).Code)

		planMap = xsync.NewMapOf[importPlanCacheKey, cachedImportPlan]()
		entryMap = xsync.NewMapOf[string, *entryCache]()
		res := f.postJSON(t, "/api/import", f.packPayload())

		require.Equal(t, domain.StatusSuccessfulHardlink.Code(), res.Code)
		require.Equal(t, 2, f.torrentClient.torrentCalls, "restart must rebuild the client inventory")
		require.Equal(t, 2, f.torrentClient.fileCalls, "restart must rebuild the exact plan")
		require.Equal(t, 1, f.torrentClient.importCalls)
	})

	t.Run("expired", func(t *testing.T) {
		f := newProcessorHTTPFixture(t, 1, 1, 0)
		require.Equal(t, domain.StatusSuccessfulMatch.Code(), f.postJSON(t, "/api/match", f.packPayload()).Code)

		hashes, err := torrents.InfoHashes(f.torrent)
		require.NoError(t, err)
		key := importPlanKey("default", hashes)
		cached, ok := planMap.Load(key)
		require.True(t, ok)
		cached.expiresAt = time.Now().Add(-time.Second)
		planMap.Store(key, cached)

		res := f.postJSON(t, "/api/import", f.packPayload())
		require.Equal(t, domain.StatusSuccessfulHardlink.Code(), res.Code)
		require.Equal(t, 1, f.torrentClient.torrentCalls, "an expired plan can still reuse the inventory cache")
		require.Equal(t, 2, f.torrentClient.fileCalls, "an expired plan must rebuild file mappings")
		require.Equal(t, 1, f.torrentClient.importCalls)
	})

	t.Run("release name changed", func(t *testing.T) {
		f := newProcessorHTTPFixture(t, 1, 1, 0)
		require.Equal(t, domain.StatusSuccessfulMatch.Code(), f.postJSON(t, "/api/match", f.packPayload()).Code)

		payload := f.packPayload()
		payload["name"] = "Different.S01.1080p.WEB-DL.H.264-RlsGrp"
		res := f.postJSON(t, "/api/import", payload)

		require.Equal(t, domain.StatusNoMatches.Code(), res.Code)
		require.Zero(t, f.torrentClient.importCalls, "a plan accepted for another release must not import")
	})

	t.Run("torrent identity changed", func(t *testing.T) {
		f := newProcessorHTTPFixture(t, 1, 1, 0)
		require.Equal(t, domain.StatusSuccessfulMatch.Code(), f.postJSON(t, "/api/match", f.packPayload()).Code)
		changedTorrent, err := torrents.TorrentFromRls(f.releaseName, 2)
		require.NoError(t, err)

		payload := f.packPayload()
		payload["torrent"] = base64.StdEncoding.EncodeToString(changedTorrent)
		res := f.postJSON(t, "/api/import", payload)

		require.Equal(t, domain.StatusSuccessfulHardlink.Code(), res.Code)
		require.Equal(t, 2, f.torrentClient.fileCalls, "a different info hash must rebuild the exact plan")
		require.Equal(t, 1, f.torrentClient.importCalls)
	})

	t.Run("fuzzy matching changed", func(t *testing.T) {
		f := newProcessorHTTPFixture(t, 1, 1, 0)
		require.Equal(t, domain.StatusSuccessfulMatch.Code(), f.postJSON(t, "/api/match", f.packPayload()).Code)

		changed := f.config.Snapshot()
		changed.FuzzyMatching.SkipYearCompare = true
		f.config.Store(changed)
		res := f.postJSON(t, "/api/import", f.packPayload())

		require.Equal(t, domain.StatusSuccessfulHardlink.Code(), res.Code)
		require.Equal(t, 2, f.torrentClient.torrentCalls, "matching changes must refresh the inventory index")
		require.Equal(t, 2, f.torrentClient.fileCalls, "matching changes must rebuild the exact plan")
		require.Equal(t, 1, f.torrentClient.importCalls)
	})

	t.Run("client configuration changed", func(t *testing.T) {
		f := newProcessorHTTPFixture(t, 1, 1, 0)
		require.Equal(t, domain.StatusSuccessfulMatch.Code(), f.postJSON(t, "/api/match", f.packPayload()).Code)

		changed := f.config.Snapshot()
		changedClient := cloneClientConfig(*changed.Clients["default"])
		changedClient.Import.Category = "changed"
		changed.Clients = map[string]*domain.Client{"default": &changedClient}
		f.config.Store(changed)
		clientMap.Store("default", cachedTorrentClient{config: cloneClientConfig(changedClient), client: f.torrentClient})

		res := f.postJSON(t, "/api/import", f.packPayload())
		require.Equal(t, domain.StatusSuccessfulHardlink.Code(), res.Code)
		require.Equal(t, 2, f.torrentClient.torrentCalls, "client changes must refresh the inventory")
		require.Equal(t, 2, f.torrentClient.fileCalls, "client changes must rebuild the exact plan")
		require.Equal(t, 1, f.torrentClient.importCalls)
	})
}

func TestImportEndpoint_MutationFailuresRemainSafeAndRetryable(t *testing.T) {
	t.Run("import failure refreshes state for retry", func(t *testing.T) {
		f := newProcessorHTTPFixture(t, 1, 1, 0)
		require.Equal(t, domain.StatusSuccessfulMatch.Code(), f.postJSON(t, "/api/match", f.packPayload()).Code)
		f.torrentClient.importErr = errors.New("import failed")

		first := f.postJSON(t, "/api/import", f.packPayload())
		require.Equal(t, domain.StatusAddTorrentError.Code(), first.Code)
		require.Equal(t, 1, f.torrentClient.importCalls)
		require.FileExists(t, filepath.Join(f.importDir, f.releaseName, "Lifecycle.S01E01.1080p.WEB-DL.H.264-RlsGrp.mkv"))

		f.torrentClient.importErr = nil
		second := f.postJSON(t, "/api/import", f.packPayload())
		require.Equal(t, domain.StatusSuccessfulHardlink.Code(), second.Code)
		require.Equal(t, 2, f.torrentClient.torrentCalls, "retry must check whether the failed import added a pack")
		require.Equal(t, 2, f.torrentClient.fileCalls, "retry must rebuild after a client mutation attempt")
		require.Equal(t, 2, f.torrentClient.importCalls)
	})

	t.Run("one conflicting target does not block safe links", func(t *testing.T) {
		f := newProcessorHTTPFixture(t, 2, 2, 0)
		require.Equal(t, domain.StatusSuccessfulMatch.Code(), f.postJSON(t, "/api/match", f.packPayload()).Code)

		targetDir := filepath.Join(f.importDir, f.releaseName)
		require.NoError(t, os.MkdirAll(targetDir, 0o755))
		conflictingTarget := filepath.Join(targetDir, "Lifecycle.S01E01.1080p.WEB-DL.H.264-RlsGrp.mkv")
		require.NoError(t, os.WriteFile(conflictingTarget, []byte("conflict"), 0o644))

		res := f.postJSON(t, "/api/import", f.packPayload())
		require.Equal(t, domain.StatusSuccessfulHardlink.Code(), res.Code)
		require.Equal(t, 1, f.torrentClient.importCalls)
		contents, err := os.ReadFile(conflictingTarget)
		require.NoError(t, err)
		require.Equal(t, []byte("conflict"), contents, "an unrelated target must never be overwritten")
		require.FileExists(t, filepath.Join(targetDir, "Lifecycle.S01E02.1080p.WEB-DL.H.264-RlsGrp.mkv"))
	})

	t.Run("smart mode retains safe links when achieved coverage is below threshold", func(t *testing.T) {
		f := newProcessorHTTPFixture(t, 3, 3, 0.75)
		require.Equal(t, domain.StatusSuccessfulMatch.Code(), f.postJSON(t, "/api/match", f.packPayload()).Code)

		targetDir := filepath.Join(f.importDir, f.releaseName)
		require.NoError(t, os.MkdirAll(targetDir, 0o755))
		existingSource := filepath.Join(f.sourceDir, "Lifecycle.S01E01.1080p.WEB-DL.H.264-RlsGrp.mkv")
		existingTarget := filepath.Join(targetDir, "Lifecycle.S01E01.1080p.WEB-DL.H.264-RlsGrp.mkv")
		require.NoError(t, os.Link(existingSource, existingTarget))
		conflictingTarget := filepath.Join(targetDir, "Lifecycle.S01E02.1080p.WEB-DL.H.264-RlsGrp.mkv")
		require.NoError(t, os.WriteFile(conflictingTarget, []byte("conflict"), 0o644))

		res := f.postJSON(t, "/api/import", f.packPayload())
		require.Equal(t, domain.StatusBelowThreshold.Code(), res.Code)
		require.Zero(t, f.torrentClient.importCalls)
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
		require.Equal(t, domain.StatusSuccessfulMatch.Code(), f.postJSON(t, "/api/match", f.packPayload()).Code)

		targetDir := filepath.Join(f.importDir, f.releaseName)
		require.NoError(t, os.MkdirAll(targetDir, 0o755))
		conflictingTarget := filepath.Join(targetDir, "Lifecycle.S01E01.1080p.WEB-DL.H.264-RlsGrp.mkv")
		require.NoError(t, os.WriteFile(conflictingTarget, []byte("conflict"), 0o644))

		res := f.postJSON(t, "/api/import", f.packPayload())
		require.Equal(t, domain.StatusFailedHardlink.Code(), res.Code)
		require.Zero(t, f.torrentClient.importCalls)
		contents, err := os.ReadFile(conflictingTarget)
		require.NoError(t, err)
		require.Equal(t, []byte("conflict"), contents)
	})
}

func TestCandidateEndpoint_ReturnsSuccessfulMatchWithoutFileReads(t *testing.T) {
	resetProcessorGlobals()
	gin.SetMode(gin.TestMode)

	clientCfg := &domain.Client{Type: "qbittorrent", Import: domain.ImportPolicy{Category: "tv-hd"}}
	torrentClient := &fakeTorrentClient{
		torrents: []torrentclient.Torrent{
			{Name: "Candidate.S01E01.1080p.WEB-DL.H.264-RlsGrp", Hash: "ep1", SavePath: "/data/tv"},
		},
	}
	clientMap.Store("default", cachedTorrentClient{config: cloneClientConfig(*clientCfg), client: torrentClient})

	cfg := &fakeConfig{config: domain.Config{Clients: map[string]*domain.Client{"default": clientCfg}}}
	router := gin.New()
	newWebhookHandler(logger.New(&domain.Config{LogLevel: "ERROR", Version: "test"}), cfg, nil, newTaskGroup()).Routes(router.Group("/api"))

	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/candidate", bytes.NewBufferString(`{"name":"Candidate.S01.1080p.WEB-DL.H.264-RlsGrp","clientname":"default"}`))
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	require.Equal(t, domain.StatusSuccessfulMatch.Code(), res.Code)
	require.Zero(t, torrentClient.fileCalls)
}
