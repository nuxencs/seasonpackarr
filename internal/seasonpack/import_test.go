// Copyright (c) 2023 - 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package seasonpack

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/nuxencs/seasonpackarr/internal/domain"
	"github.com/nuxencs/seasonpackarr/internal/inventory"
	"github.com/nuxencs/seasonpackarr/internal/torrentclient"
	"github.com/nuxencs/seasonpackarr/internal/torrentclient/torrentclienttest"
	"github.com/nuxencs/seasonpackarr/internal/torrents"

	"github.com/autobrr/go-torrent/bencode"
	"github.com/autobrr/go-torrent/metainfo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestImport_ReusesAcceptedPlanWithoutClientReads(t *testing.T) {
	pack := newPack(t, "PlanReuse", 2, 2)
	f := newFixture(pack.client)
	cfg := newSmartConfig(0.75)

	// Each request has its own handle, as in the webhook handlers.
	outcome := f.processor.Match(t.Context(), f.client(t, "default", cfg), pack.name, TorrentBytes(pack.torrent))
	require.NoError(t, outcome.Err)
	require.Equal(t, domain.StatusSuccessfulMatch, outcome.Status)
	require.Equal(t, 1, pack.client.TorrentCalls)
	require.Equal(t, 2, pack.client.FileCalls)

	outcome = f.processor.Import(t.Context(), f.client(t, "default", cfg), pack.name, pack.torrent)
	require.NoError(t, outcome.Err)
	require.Equal(t, domain.StatusSuccessfulHardlink, outcome.Status)
	require.Equal(t, &Coverage{Reusable: 2, Total: 2}, outcome.Coverage)
	require.Equal(t, 1, pack.client.TorrentCalls, "import must reuse the accepted inventory")
	require.Equal(t, 2, pack.client.FileCalls, "import must reuse the accepted import plan")
	require.FileExists(t, pack.target(1))
	require.FileExists(t, pack.target(2))
}

// TestImport_ImportsAndPassesResolvedRoot verifies that the import resolves
// the import root, hardlinks the matched episodes under it, and hands the
// torrent, the info hash, and the resolved root to the client import.
func TestImport_ImportsAndPassesResolvedRoot(t *testing.T) {
	pack := newPack(t, "ParseImport", 2, 2)
	infoHashes, err := torrents.InfoHashes(pack.torrent)
	require.NoError(t, err)
	f := newFixture(pack.client)

	outcome := f.processor.Import(t.Context(), f.defaultClient(t), pack.name, pack.torrent)
	require.NoError(t, outcome.Err)
	require.Equal(t, domain.StatusSuccessfulHardlink, outcome.Status)

	require.True(t, pack.client.ImportCalled)
	require.Equal(t, infoHashes.Legacy, pack.client.ImportReq.LegacyHash)
	require.Equal(t, infoHashes.V2, pack.client.ImportReq.V2Hash)
	require.Equal(t, infoHashes.HasV1, pack.client.ImportReq.HasV1)
	require.Equal(t, pack.importDir, pack.client.ImportReq.SavePath)
	require.Equal(t, pack.torrent, pack.client.ImportReq.TorrentBytes)
	require.True(t, pack.client.ImportReq.DataComplete, "every torrent file is hardlinked with the expected size")

	require.FileExists(t, pack.target(1))
	require.FileExists(t, pack.target(2))
}

// TestImport_ReportsPartialPackData guards the client hash-check decision: a
// missing torrent file must never let a client skip its check.
func TestImport_ReportsPartialPackData(t *testing.T) {
	pack := newPack(t, "PartialImport", 2, 1)
	f := newFixture(pack.client)

	outcome := f.processor.Import(t.Context(), f.defaultClient(t), pack.name, pack.torrent)

	require.NoError(t, outcome.Err)
	require.Equal(t, domain.StatusSuccessfulHardlink, outcome.Status)
	require.Equal(t, &Coverage{Reusable: 1, Total: 2}, outcome.Coverage)
	require.True(t, pack.client.ImportCalled)
	require.False(t, pack.client.ImportReq.DataComplete)
}

// TestImport_ClientImportIgnoresCallerCancel guards the production failure
// where autobrr's request timeout cancelled a started client import and left
// the torrent stopped.
func TestImport_ClientImportIgnoresCallerCancel(t *testing.T) {
	pack := newPack(t, "CancelImport", 1, 1)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	pack.client.OnImportDestination = cancel
	f := newFixture(pack.client)

	_ = f.processor.Import(ctx, f.defaultClient(t), pack.name, pack.torrent)

	require.True(t, pack.client.ImportCalled)
	require.NoError(t, pack.client.ImportCtxErr, "a caller disconnect must not cancel the client import")
}

func TestImport_RejectsArchivePackWithSampleVideos(t *testing.T) {
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
			{Name: "ArchivePack.S01E01.1080p.WEB.h264-RlsGrp", Hash: "ep1"},
		},
		SavePath: sourceDir,
		FilesByHash: map[string][]torrentclient.File{
			"ep1": {{Name: episodeName, Size: 1}},
		},
		ImportRoot: importDir,
	}
	f := newFixture(torrentClient)

	outcome := f.processor.Import(t.Context(), f.defaultClient(t), releaseName, torrent.Bytes())

	require.EqualError(t, outcome.Err, domain.StatusFailedMatchToTorrentEps.String())
	require.Equal(t, domain.StatusFailedMatchToTorrentEps, outcome.Status)
	require.False(t, torrentClient.ImportCalled, "rejected archive pack must not be imported")
	require.NoFileExists(t, filepath.Join(importDir, releaseName, episodeName))
}

func TestImport_UsesFlatImportDestination(t *testing.T) {
	pack := newPack(t, "FlatImport", 1, 1)
	pack.client.FlatImport = true
	f := newFixture(pack.client)

	outcome := f.processor.Import(t.Context(), f.defaultClient(t), pack.name, pack.torrent)

	require.NoError(t, outcome.Err)
	require.Equal(t, domain.StatusSuccessfulHardlink, outcome.Status)
	require.FileExists(t, filepath.Join(pack.importDir, pack.episodeFile(1)))
	require.NoFileExists(t, pack.target(1))
}

// TestImport_RefusesRelativeImportRoot: a relative import root must not make
// seasonpackarr link below its own working directory (issue #351).
func TestImport_RefusesRelativeImportRoot(t *testing.T) {
	pack := newPack(t, "RelativeImport", 1, 1)
	workDir := filepath.Join(filepath.Dir(pack.sourceDir), "work")
	require.NoError(t, os.MkdirAll(workDir, 0o755))
	t.Chdir(workDir)
	pack.client.ImportRoot = "import"
	f := newFixture(pack.client)

	outcome := f.processor.Import(t.Context(), f.defaultClient(t), pack.name, pack.torrent)

	require.EqualError(t, outcome.Err, domain.StatusFailedHardlink.String())
	require.Equal(t, domain.StatusFailedHardlink, outcome.Status)
	require.False(t, pack.client.ImportCalled, "the client must not get a relative save path")
	entries, err := os.ReadDir(workDir)
	require.NoError(t, err)
	require.Empty(t, entries, "nothing must appear below the working directory")
}

func TestImport_RetryReusesExistingHardlinks(t *testing.T) {
	pack := newPack(t, "RetryImport", 1, 1)
	f := newFixture(pack.client)

	for attempt := range 2 {
		pack.client.ImportCalled = false

		outcome := f.processor.Import(t.Context(), f.defaultClient(t), pack.name, pack.torrent)

		require.NoError(t, outcome.Err, "attempt %d", attempt+1)
		require.Equal(t, domain.StatusSuccessfulHardlink, outcome.Status)
		require.True(t, pack.client.ImportCalled, "attempt %d must reach client import", attempt+1)
	}
}

// TestImport_SkipsCrossSeedDuplicates ensures a target is hardlinked only once
// even when multiple cross-seeded client torrents match the same episode.
func TestImport_SkipsCrossSeedDuplicates(t *testing.T) {
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
			{Name: "CrossSeed.S01E01.1080p.WEB-DL.H.264-RlsGrp", Hash: "ep1a"},
			{Name: "CrossSeed.S01E01.1080p.WEB-DL.H.264-RlsGrp", Hash: "ep1b"},
			{Name: "CrossSeed.S01E02.1080p.WEB-DL.H.264-RlsGrp", Hash: "ep2a"},
			{Name: "CrossSeed.S01E02.1080p.WEB-DL.H.264-RlsGrp", Hash: "ep2b"},
		},
		SavePathByHash: map[string]string{"ep1a": sourceDir1, "ep1b": sourceDir2, "ep2a": sourceDir1, "ep2b": sourceDir2},
		FilesByHash: map[string][]torrentclient.File{
			"ep1a": {{Name: ep1, Size: 1}},
			"ep1b": {{Name: ep1, Size: 1}},
			"ep2a": {{Name: ep2, Size: 1}},
			"ep2b": {{Name: ep2, Size: 1}},
		},
		ImportRoot: importDir,
	}
	f := newFixture(torrentClient)

	outcome := f.processor.Import(t.Context(), f.defaultClient(t), releaseName, torrentBytes)

	require.NoError(t, outcome.Err)
	require.Equal(t, domain.StatusSuccessfulHardlink, outcome.Status)
	require.Equal(t, &Coverage{Reusable: 2, Total: 2}, outcome.Coverage)
	require.FileExists(t, filepath.Join(importDir, releaseName, ep1))
	require.FileExists(t, filepath.Join(importDir, releaseName, ep2))
	require.True(t, torrentClient.ImportCalled)
}

// The request checks of the import read no client.
func TestImport_ChecksRequestBeforeClientRead(t *testing.T) {
	pack := newPack(t, "Checks", 1, 1)
	tests := []struct {
		name    string
		release string
		torrent []byte
		want    domain.StatusCode
	}{
		{name: "empty torrent bytes", release: pack.name, want: domain.StatusTorrentBytesError},
		{name: "empty torrent bytes and empty release name", want: domain.StatusTorrentBytesError},
		{name: "empty release name", torrent: pack.torrent, want: domain.StatusAnnounceNameError},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture(pack.client)

			outcome := f.processor.Import(t.Context(), f.defaultClient(t), tt.release, tt.torrent)

			assert.Equal(t, tt.want, outcome.Status)
			assert.EqualError(t, outcome.Err, tt.want.String())
			assert.Zero(t, pack.client.TorrentCalls)
			assert.Zero(t, pack.client.ImportCalls)
		})
	}
}

// The plan of a match is valid for 2 minutes.
func TestImport_BuildsPlanAgainAfterTwoMinutes(t *testing.T) {
	tests := []struct {
		name      string
		wait      time.Duration
		fileReads int
	}{
		{name: "uses the plan before it expires", wait: 2*time.Minute - time.Second, fileReads: 1},
		{name: "builds the plan again after it expires", wait: 2*time.Minute + time.Second, fileReads: 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				pack := newPack(t, "Expired", 1, 1)
				f := newFixture(pack.client)
				outcome := f.processor.Match(t.Context(), f.defaultClient(t), pack.name, TorrentBytes(pack.torrent))
				require.NoError(t, outcome.Err)
				require.Equal(t, 1, pack.client.FileBatchCalls)

				time.Sleep(tt.wait)
				outcome = f.processor.Import(t.Context(), f.defaultClient(t), pack.name, pack.torrent)

				require.NoError(t, outcome.Err)
				assert.Equal(t, tt.fileReads, pack.client.FileBatchCalls)
				assert.Equal(t, 1, pack.client.ImportCalls)
			})
		})
	}
}

// A plan is valid only for the release name, the client config with its source
// folders, and the fuzzy matching settings of its match.
func TestImport_BuildsPlanAgainWhenItsScopeChanged(t *testing.T) {
	tests := map[string]func(release *string, cfg *domain.Config){
		// Both names parse to the same release, so the candidate check passes.
		"release name": func(release *string, _ *domain.Config) {
			*release = "Scope.S01.1080p.WEB-DL.H264-RlsGrp"
		},
		"client config": func(_ *string, cfg *domain.Config) {
			changed := cfg.Clients["default"].Clone()
			changed.Import.Category = "changed"
			cfg.Clients = map[string]*domain.Client{"default": &changed}
		},
		"fuzzy matching settings": func(_ *string, cfg *domain.Config) {
			cfg.FuzzyMatching.SkipYearCompare = true
		},
		"source folders": func(_ *string, cfg *domain.Config) {
			*cfg = withSourceFolders(*cfg, "/srv/tv")
		},
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			pack := newPack(t, "Scope", 1, 1)
			f := newFixture(pack.client)
			release, cfg := pack.name, newConfig()
			outcome := f.processor.Match(t.Context(), f.client(t, "default", cfg), release, TorrentBytes(pack.torrent))
			require.NoError(t, outcome.Err)
			require.Equal(t, 1, pack.client.FileBatchCalls)

			mutate(&release, &cfg)
			outcome = f.processor.Import(t.Context(), f.client(t, "default", cfg), release, pack.torrent)

			require.NoError(t, outcome.Err)
			assert.Equal(t, 2, pack.client.FileBatchCalls, "the import must not use the plan of the match")
			assert.Equal(t, 1, pack.client.ImportCalls)
		})
	}
}

// Another import can add the season pack between the match and the import.
func TestImport_ChecksCandidateAgain(t *testing.T) {
	pack := newPack(t, "Again", 1, 1)
	f := newFixture(pack.client)
	outcome := f.processor.Match(t.Context(), f.defaultClient(t), pack.name, TorrentBytes(pack.torrent))
	require.NoError(t, outcome.Err)

	pack.client.Torrents = append(pack.client.Torrents, torrentclient.Torrent{Name: pack.name, Hash: "pack"})
	f.store.DiscardClient("default")
	outcome = f.processor.Import(t.Context(), f.defaultClient(t), pack.name, pack.torrent)

	require.Equal(t, domain.StatusAlreadyInClient, outcome.Status)
	require.EqualError(t, outcome.Err, domain.StatusAlreadyInClient.String())
	require.Equal(t, Rejected, outcome.Class())
	require.Zero(t, pack.client.ImportCalls)
	require.NoFileExists(t, pack.target(1))
}

// The client moved an episode between the candidate check and the match. The
// plan of the match holds the new path, so the import links each episode with
// no refresh.
func TestImport_LinksEpisodeMovedBeforeMatch(t *testing.T) {
	pack := newPack(t, "MovedBefore", 2, 2)
	f := newFixture(pack.client)
	client := f.defaultClient(t)
	require.NoError(t, f.processor.Candidate(t.Context(), client, pack.name).Err)
	pack.moveEpisode(t, 2)
	outcome := f.processor.Match(t.Context(), client, pack.name, TorrentBytes(pack.torrent))
	require.NoError(t, outcome.Err)
	require.Equal(t, &Coverage{Reusable: 2, Total: 2}, outcome.Coverage)

	outcome = f.processor.Import(t.Context(), client, pack.name, pack.torrent)

	require.NoError(t, outcome.Err)
	require.Equal(t, domain.StatusSuccessfulHardlink, outcome.Status)
	assert.Equal(t, &Coverage{Reusable: 2, Total: 2}, outcome.Coverage)
	assert.FileExists(t, pack.target(1))
	assert.FileExists(t, pack.target(2))
	assert.True(t, pack.client.ImportReq.DataComplete, "the client import must get complete pack data")
	assert.Equal(t, 1, pack.client.TorrentCalls, "the import must need no inventory refresh")
	assert.Equal(t, 1, pack.client.FileBatchCalls, "the import must use the plan of the match")
}

// The client moved an episode after the match. The import reads the client
// again and builds the plan again, one time.
func TestImport_RefreshesPlanOnceAfterMissingSource(t *testing.T) {
	pack := newPack(t, "Moved", 2, 2)
	f := newFixture(pack.client)
	cfg := newSmartConfig(1)
	outcome := f.processor.Match(t.Context(), f.client(t, "default", cfg), pack.name, TorrentBytes(pack.torrent))
	require.NoError(t, outcome.Err)

	pack.moveEpisode(t, 2)
	outcome = f.processor.Import(t.Context(), f.client(t, "default", cfg), pack.name, pack.torrent)

	require.NoError(t, outcome.Err)
	require.Equal(t, domain.StatusSuccessfulHardlink, outcome.Status)
	require.Equal(t, 2, pack.client.TorrentCalls, "import must refresh the stale inventory once")
	require.Equal(t, 2, pack.client.FileBatchCalls, "import must rebuild the stale plan once")
	require.FileExists(t, pack.target(1))
	require.FileExists(t, pack.target(2))
	require.Equal(t, 1, pack.client.ImportCalls)
}

// One hardlink target holds another file, so the import links fewer episodes
// than the plan has.
func TestImport_StopsBeforeClientImportWhenAchievedCoverageIsBelowThreshold(t *testing.T) {
	pack := newPack(t, "Achieved", 3, 3)
	f := newFixture(pack.client)
	require.NoError(t, os.MkdirAll(filepath.Dir(pack.target(2)), 0o755))
	require.NoError(t, os.WriteFile(pack.target(2), []byte("conflict"), 0o644))

	outcome := f.processor.Import(t.Context(), f.client(t, "default", newSmartConfig(0.75)), pack.name, pack.torrent)

	require.Equal(t, domain.StatusBelowThreshold, outcome.Status)
	require.EqualError(t, outcome.Err, domain.StatusBelowThreshold.String())
	require.Equal(t, &Coverage{Reusable: 2, Total: 3}, outcome.Coverage)
	require.Zero(t, pack.client.ImportCalls)
	contents, err := os.ReadFile(pack.target(2))
	require.NoError(t, err)
	require.Equal(t, []byte("conflict"), contents, "an unrelated target must never be overwritten")
}

// fakeBlockingClient holds each client import until resume is closed.
type fakeBlockingClient struct {
	*torrentclienttest.Client
	started atomic.Int32
	resume  chan struct{}
}

func (f *fakeBlockingClient) Import(ctx context.Context, req torrentclient.ImportRequest) (torrentclient.ImportReport, error) {
	f.started.Add(1)
	<-f.resume
	return f.Client.Import(ctx, req)
}

// Two client names with one endpoint are one client service, so their imports
// must not run at the same time.
func TestImport_RunsOneAtATimeForOneEndpoint(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		pack := newPack(t, "Serial", 1, 1)
		blocking := &fakeBlockingClient{Client: pack.client, resume: make(chan struct{})}
		f := newFixture(blocking)
		cfg := newConfig()
		alias := cfg.Clients["default"].Clone()
		alias.Import.Category = "other"
		cfg.Clients["alias"] = &alias
		clients := []*inventory.Client{f.client(t, "default", cfg), f.client(t, "alias", cfg)}

		outcomes := make(chan Outcome, len(clients))
		for _, client := range clients {
			go func() { outcomes <- f.processor.Import(t.Context(), client, pack.name, pack.torrent) }()
			synctest.Wait()
			assert.Equal(t, int32(1), blocking.started.Load(), "only the first import reaches the client")
		}
		assert.Empty(t, outcomes)

		close(blocking.resume)
		synctest.Wait()

		assert.Equal(t, int32(2), blocking.started.Load())
		for range clients {
			outcome := <-outcomes
			assert.NoError(t, outcome.Err)
		}
	})
}

// The endpoint is the address that the adapter connects to, so an import
// discards the inventory of a client that spells the same address another way,
// and keeps the inventory of a client behind another qui proxy path.
func TestImport_DiscardsInventoryOfEachClientOnTheEndpoint(t *testing.T) {
	tests := map[string]struct {
		importHost, otherHost string
		discarded             bool
	}{
		"same address with another spelling": {importHost: "qbit", otherHost: "http://QBit:8080/", discarded: true},
		"other qui proxy path":               {importHost: "http://qui:8080/proxy/abc", otherHost: "http://qui:8080/proxy/def"},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			pack := newPack(t, "Endpoint", 1, 1)
			f := newFixture(pack.client)
			cfg := newConfig()
			cfg.Clients["default"].Host = tt.importHost
			cfg.Clients["default"].Port = 8080
			cfg.Clients["other"] = &domain.Client{Type: "qbittorrent", Host: tt.otherHost, Import: domain.ImportPolicy{Category: "other"}}
			other := f.client(t, "other", cfg)
			_, err := other.Inventory(t.Context())
			require.NoError(t, err)

			outcome := f.processor.Import(t.Context(), f.client(t, "default", cfg), pack.name, pack.torrent)
			require.NoError(t, outcome.Err)
			before := pack.client.TorrentCalls
			_, err = other.Inventory(t.Context())
			require.NoError(t, err)

			reads := pack.client.TorrentCalls - before
			if tt.discarded {
				assert.Equal(t, 1, reads, "the import must discard the inventory of the other client")
			} else {
				assert.Zero(t, reads, "the import must keep the inventory of the other endpoint")
			}
		})
	}
}

// A client can add a torrent before it reports a failure, so each import
// attempt makes the next call read the client and build the plan again.
func TestImport_NextCallReadsClientAgainAfterAttempt(t *testing.T) {
	tests := map[string]error{
		"successful import": nil,
		"failed import":     errors.New("import failed"),
	}
	for name, importErr := range tests {
		t.Run(name, func(t *testing.T) {
			pack := newPack(t, "Attempt", 1, 1)
			pack.client.ImportErr = importErr
			f := newFixture(pack.client)
			match := func() Outcome {
				return f.processor.Match(t.Context(), f.defaultClient(t), pack.name, TorrentBytes(pack.torrent))
			}
			require.NoError(t, match().Err)
			require.Equal(t, 1, pack.client.TorrentCalls)
			require.Equal(t, 1, pack.client.FileBatchCalls)

			outcome := f.processor.Import(t.Context(), f.defaultClient(t), pack.name, pack.torrent)
			require.Equal(t, importErr == nil, outcome.Err == nil, "%v", outcome.Err)
			require.Equal(t, 1, pack.client.ImportCalls)
			require.Equal(t, 1, pack.client.FileBatchCalls, "the import uses the plan of the match")

			require.NoError(t, match().Err)
			assert.Equal(t, 2, pack.client.TorrentCalls, "the attempt must drop the inventory")
			pack.client.ImportErr = nil
			outcome = f.processor.Import(t.Context(), f.defaultClient(t), pack.name, pack.torrent)
			require.NoError(t, outcome.Err)
			assert.Equal(t, 2, pack.client.FileBatchCalls, "the second match stores a new plan")
		})
	}
}

// A failed import of a plan keeps no plan: the retry builds it again.
func TestImport_RetryBuildsPlanAgainAfterFailedAttempt(t *testing.T) {
	pack := newPack(t, "Retry", 1, 1)
	pack.client.ImportErr = errors.New("import failed")
	f := newFixture(pack.client)
	require.NoError(t, f.processor.Match(t.Context(), f.defaultClient(t), pack.name, TorrentBytes(pack.torrent)).Err)

	first := f.processor.Import(t.Context(), f.defaultClient(t), pack.name, pack.torrent)
	require.Equal(t, domain.StatusAddTorrentError, first.Status)
	require.Equal(t, Failed, first.Class())
	pack.client.ImportErr = nil
	second := f.processor.Import(t.Context(), f.defaultClient(t), pack.name, pack.torrent)

	require.NoError(t, second.Err)
	assert.Equal(t, 2, pack.client.TorrentCalls, "the retry must check whether the failed import added the pack")
	assert.Equal(t, 2, pack.client.FileBatchCalls, "the retry must build the plan again")
	assert.Equal(t, 2, pack.client.ImportCalls)
}

// A torrent source has no containment rule (issue #426). The save path is no boundary that the operator declared, and
// the client reads and writes the data of the torrent through the same symlink. Thus an episode behind a folder below
// the save path that is a symlink to another place is still reused, for a new plan and for the plan of a match.
func TestImport_LinksTorrentSourceBehindSymlinkedFolder(t *testing.T) {
	for _, cached := range []bool{false, true} {
		t.Run(fmt.Sprintf("cached plan %t", cached), func(t *testing.T) {
			pack := newPack(t, "TorrentSymlinked", 1, 1)
			outside := filepath.Join(filepath.Dir(pack.sourceDir), "outside")
			require.NoError(t, os.MkdirAll(outside, 0o755))
			moved := filepath.Join(outside, pack.episodeFile(1))
			require.NoError(t, os.Rename(pack.source(1), moved))
			require.NoError(t, os.Symlink(outside, filepath.Join(pack.sourceDir, "Season 01")))
			pack.client.FilesByHash["ep1"] = []torrentclient.File{{Name: filepath.Join("Season 01", pack.episodeFile(1)), Size: 1}}
			f := newFixture(pack.client)
			client := f.defaultClient(t)
			if cached {
				require.NoError(t, f.processor.Match(t.Context(), client, pack.name, TorrentBytes(pack.torrent)).Err)
			}

			outcome := f.processor.Import(t.Context(), client, pack.name, pack.torrent)

			require.NoError(t, outcome.Err)
			assert.Equal(t, domain.StatusSuccessfulHardlink, outcome.Status)
			requireSameFile(t, moved, pack.target(1))
		})
	}
}
