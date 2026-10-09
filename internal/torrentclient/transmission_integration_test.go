// Copyright (c) 2023 - 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

//go:build integration

package torrentclient

import (
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
		assertRemoved(t, ctx, req.LegacyHash, func() (bool, error) {
			found, err := lookup()
			return len(found) > 0, err
		})
	})
	report, err := c.Import(t.Context(), req)
	require.NoError(t, err)
	return report
}

// waitTransmissionChecked waits for the add-time check to finish. The adapter
// returns before it, so assertions on progress must wait.
func waitTransmissionChecked(t *testing.T, c *transmissionClient, hash string) transmissionrpc.Torrent {
	t.Helper()
	tr, checked := waitFor(t.Context(), func() transmissionrpc.Torrent {
		found, err := c.c.TorrentGetHashes(t.Context(), []string{"status", "percentDone", "errorString"}, []string{hash})
		require.NoError(t, err)
		require.Len(t, found, 1, "torrent %s is missing", hash)
		return found[0]
	}, func(tr transmissionrpc.Torrent) bool {
		return tr.Status != nil && *tr.Status != transmissionrpc.TorrentStatusCheckWait && *tr.Status != transmissionrpc.TorrentStatusCheck
	})
	require.True(t, checked, "Transmission did not finish its check")
	t.Logf("Transmission status=%s percentDone=%.2f error=%q", *tr.Status, transmissionPercentDone(tr), derefString(tr.ErrorString))
	return tr
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
