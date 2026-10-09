// Copyright (c) 2023 - 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

//go:build integration

package torrentclient

import (
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"github.com/nuxencs/seasonpackarr/internal/domain"

	"github.com/hekmon/transmissionrpc/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTransmissionDaemon_ImportsCompletePack(t *testing.T) {
	importDir := requireDaemon(t, envTransmissionHost)
	c := newTransmissionDaemonClient(t, domain.ImportPolicy{SavePath: importDir, Tags: []string{"seasonpackarr"}})
	pack := writeCompletePack(t, importDir, packName(t))

	importTransmissionPack(t, c, pack.importRequest(importDir, true))

	tr := waitTransmissionChecked(t, c, pack.hashes.Legacy)
	assertTransmissionStarted(t, tr)
	requireListedPack(t, c, pack, importDir)
	requireFileReadLoad(t, c, pack.hashes.Legacy)
}

func TestTransmissionDaemon_ResumesPartialPack(t *testing.T) {
	importDir := requireDaemon(t, envTransmissionHost)
	c := newTransmissionDaemonClient(t, domain.ImportPolicy{SavePath: importDir})
	pack := writePartialPack(t, importDir, packName(t), 1)

	importTransmissionPack(t, c, pack.importRequest(importDir, false))

	tr := waitTransmissionChecked(t, c, pack.hashes.Legacy)
	assertTransmissionStarted(t, tr)
	assertPartialProgress(t, transmissionPercentDone(tr), 1)
}

// TestTransmissionDaemon_ResumesPartialPackWithIncompleteDir keeps only the last
// episode. With the first file missing, Transmission keeps the torrent's folder
// in the incomplete folder, but it must still find the hardlinked episode in
// the download folder, check it, and start.
func TestTransmissionDaemon_ResumesPartialPackWithIncompleteDir(t *testing.T) {
	importDir := requireDaemon(t, envTransmissionHost)
	c := newTransmissionDaemonClient(t, domain.ImportPolicy{SavePath: importDir})
	raw := transmissionDaemonAPI(t, c)

	original, err := raw.SessionArgumentsGetAll(t.Context())
	require.NoError(t, err)
	t.Cleanup(func() {
		ctx, cancel := cleanupContext(t)
		defer cancel()
		assert.NoError(t, raw.SessionArgumentsSet(ctx, transmissionrpc.SessionArguments{
			IncompleteDirEnabled: original.IncompleteDirEnabled,
			IncompleteDir:        original.IncompleteDir,
		}), "restore session")
	})
	incompleteDir := filepath.Join(importDir, "incomplete")
	require.NoError(t, os.MkdirAll(incompleteDir, 0o755))
	require.NoError(t, raw.SessionArgumentsSet(t.Context(), transmissionrpc.SessionArguments{
		IncompleteDirEnabled: new(true),
		IncompleteDir:        new(incompleteDir),
	}))
	pack := writePartialPack(t, importDir, packName(t), packEpisodes)

	importTransmissionPack(t, c, pack.importRequest(importDir, false))

	tr := waitTransmissionChecked(t, c, pack.hashes.Legacy)
	assertTransmissionStarted(t, tr)
	assertPartialProgress(t, transmissionPercentDone(tr), 1)
}

// TestTransmissionDaemon_DownloadsMissingEpisodes checks the core promise
// against a real download: the client gets only the missing episode from the
// seeder, and the reused episodes stay hardlinks of their source files.
func TestTransmissionDaemon_DownloadsMissingEpisodes(t *testing.T) {
	importDir := requireDaemon(t, envTransmissionHost)
	s := newSeeder(t)
	c := newTransmissionDaemonClient(t, domain.ImportPolicy{SavePath: importDir})
	host, err := url.Parse(os.Getenv(envTransmissionHost))
	require.NoError(t, err)

	tests := []struct {
		name    string
		missing int
	}{
		{name: "first episode missing", missing: 1},
		{name: "last episode missing", missing: packEpisodes},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			name := packName(t)
			seeded := s.seed(t, name)
			pack := writeLinkedPack(t, importDir, name, tt.missing)
			require.Equal(t, seeded.hashes, pack.hashes, "the seeder and the import must have the same torrent")

			importTransmissionPack(t, c, pack.importRequest(importDir, false))
			tr := waitTransmissionChecked(t, c, pack.hashes.Legacy)
			assertTransmissionStarted(t, tr)
			assertPartialProgress(t, transmissionPercentDone(tr), packEpisodes-1)

			session, err := transmissionDaemonAPI(t, c).SessionArgumentsGetAll(t.Context())
			require.NoError(t, err)
			require.NotNil(t, session.PeerPort, "session has no peer-port")
			peer := newPeerAddress(t, host.Hostname(), int(*session.PeerPort))
			tr, complete := waitSeededDownload(t, s, pack.hashes.Legacy, peer, readTransmissionTorrent(t, c, pack.hashes.Legacy),
				func(tr transmissionrpc.Torrent) bool {
					// Transmission downloads into <file>.part and renames it when
					// the file completes. Seeding comes after the rename.
					return transmissionPercentDone(tr) >= 1 && tr.Status != nil && *tr.Status == transmissionrpc.TorrentStatusSeed &&
						transmissionDownloaded(tr) >= packEpisodeSize
				})
			require.True(t, complete, "download did not complete: status=%s percentDone=%.2f error=%q",
				tr.Status, transmissionPercentDone(tr), derefString(tr.ErrorString))
			assertDownloadedPack(t, importDir, pack, tt.missing, transmissionDownloaded(tr))
		})
	}
}

func newTransmissionDaemonClient(t *testing.T, policy domain.ImportPolicy) *transmissionClient {
	t.Helper()
	c, err := newTransmissionClient(t.Context(), &domain.Client{
		Host:     os.Getenv(envTransmissionHost),
		Username: os.Getenv(envTransmissionUser),
		Password: os.Getenv(envTransmissionPass),
		Import:   policy,
	})
	require.NoError(t, err)
	return c
}

// transmissionDaemonAPI returns the full RPC client for setup and teardown
// calls that the adapter does not use.
func transmissionDaemonAPI(t *testing.T, c *transmissionClient) *transmissionrpc.Client {
	t.Helper()
	raw, ok := c.c.(*transmissionrpc.Client)
	require.True(t, ok, "adapter does not wrap an RPC client")
	return raw
}

// importTransmissionPack imports through the adapter and removes the torrent,
// not its data, at cleanup.
func importTransmissionPack(t *testing.T, c *transmissionClient, req ImportRequest) ImportReport {
	t.Helper()
	raw := transmissionDaemonAPI(t, c)
	t.Cleanup(func() {
		ctx, cancel := cleanupContext(t)
		defer cancel()
		lookup := func() ([]transmissionrpc.Torrent, error) {
			return raw.TorrentGetHashes(ctx, []string{"id"}, []string{req.LegacyHash})
		}
		found, err := lookup()
		if !assert.NoError(t, err, "find torrent for removal") || len(found) == 0 || found[0].ID == nil {
			return
		}
		assert.NoError(t, raw.TorrentRemove(ctx, transmissionrpc.TorrentRemovePayload{IDs: []int64{*found[0].ID}}), "remove torrent")
		assertRemoved(t, req.LegacyHash, func() (bool, error) {
			found, err := lookup()
			return len(found) > 0, err
		})
	})
	report, err := c.Import(t.Context(), req)
	require.NoError(t, err)
	return report
}

// waitTransmissionChecked waits for the add-time check to finish and the
// torrent to start or report an error. The adapter returns before the check,
// so assertions on progress must wait. Transmission starts the torrent in a
// second session step after the check, and a read between the two sees stopped.
func waitTransmissionChecked(t *testing.T, c *transmissionClient, hash string) transmissionrpc.Torrent {
	t.Helper()
	checking := func(tr transmissionrpc.Torrent) bool {
		return tr.Status == nil || *tr.Status == transmissionrpc.TorrentStatusCheckWait || *tr.Status == transmissionrpc.TorrentStatusCheck
	}
	// A torrent that stays stopped ends the wait at the timeout, and
	// assertTransmissionStarted reports it.
	tr, _ := waitFor(t.Context(), readTransmissionTorrent(t, c, hash), func(tr transmissionrpc.Torrent) bool {
		return !checking(tr) && (*tr.Status != transmissionrpc.TorrentStatusStopped || derefString(tr.ErrorString) != "")
	})
	require.False(t, checking(tr), "Transmission did not finish its check")
	t.Logf("Transmission status=%s percentDone=%.2f error=%q", *tr.Status, transmissionPercentDone(tr), derefString(tr.ErrorString))
	return tr
}

// readTransmissionTorrent returns a reader of the torrent hash for waitFor. It
// stops the test when the daemon does not hold the torrent.
func readTransmissionTorrent(t *testing.T, c *transmissionClient, hash string) func() transmissionrpc.Torrent {
	return func() transmissionrpc.Torrent {
		fields := []string{"status", "percentDone", "errorString", "downloadedEver"}
		found, err := c.c.TorrentGetHashes(t.Context(), fields, []string{hash})
		require.NoError(t, err)
		require.Len(t, found, 1, "torrent %s is missing", hash)
		return found[0]
	}
}

func assertTransmissionStarted(t *testing.T, tr transmissionrpc.Torrent) {
	t.Helper()
	assert.Empty(t, derefString(tr.ErrorString), "Transmission reported an error after import")
	assert.NotEqual(t, transmissionrpc.TorrentStatusStopped, *tr.Status, "import left the torrent stopped")
}

func transmissionPercentDone(tr transmissionrpc.Torrent) float64 {
	if tr.PercentDone == nil {
		return 0
	}
	return *tr.PercentDone
}

// transmissionDownloaded is the payload byte count of the torrent. Transmission
// counts corrupt data separately, in corruptEver.
func transmissionDownloaded(tr transmissionrpc.Torrent) int64 {
	if tr.DownloadedEver == nil {
		return 0
	}
	return *tr.DownloadedEver
}
