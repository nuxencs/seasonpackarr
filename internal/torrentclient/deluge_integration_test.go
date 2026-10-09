// Copyright (c) 2023 - 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

//go:build integration

package torrentclient

import (
	"context"
	"os"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/nuxencs/seasonpackarr/internal/domain"

	"github.com/autobrr/go-deluge"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// delugeTestAPI holds the RPC calls for setup and teardown that the adapter
// does not use.
type delugeTestAPI interface {
	Close() error
	DaemonVersion(context.Context) (string, error)
	EnablePlugin(context.Context, string) error
	GetEnabledPlugins(context.Context) ([]string, error)
	GetListenPort(context.Context) (uint16, error)
	RemoveTorrent(context.Context, string, bool) (bool, error)
}

type delugeTestLabelAPI interface {
	GetTorrentLabel(string) (string, error)
}

func TestDelugeDaemon_ImportsCompletePack(t *testing.T) {
	importDir := requireDaemon(t, envDelugeType)
	c := newDelugeDaemonClient(t, importDir)
	pack := writeCompletePack(t, importDir, packName(t, os.Getenv(envDelugeType)))

	importDelugePack(t, c, pack.importRequest(importDir, true))

	status := waitDelugeChecked(t, c, pack.hashes.Legacy)
	assert.InDelta(t, 100, status.Progress, 0.01, "complete pack progress")
	assert.Equal(t, status.TotalSize, status.TotalDone, "complete pack bytes")
	requireListedPack(t, c, pack, importDir)
	requireFileReadLoad(t, c, pack.hashes.Legacy)
	requireDelugeLabel(t, c, pack.hashes.Legacy, "seasonpackarr")
}

func TestDelugeDaemon_ResumesPartialPack(t *testing.T) {
	importDir := requireDaemon(t, envDelugeType)
	c := newDelugeDaemonClient(t, importDir)
	pack := writePartialPack(t, importDir, packName(t, os.Getenv(envDelugeType)), 1)

	importDelugePack(t, c, pack.importRequest(importDir, false))

	status := waitDelugeChecked(t, c, pack.hashes.Legacy)
	assertPartialProgress(t, float64(status.Progress)/100, 1)
	assert.Positive(t, status.TotalDone, "the check must find the present episode")
	assert.Less(t, status.TotalDone, status.TotalSize, "a partial pack must not read as complete")
	requireDelugeLabel(t, c, pack.hashes.Legacy, "seasonpackarr")
}

// TestDelugeDaemon_DownloadsMissingEpisodes checks the core promise against a
// real download: the client gets only the missing episode from the seeder, and
// the reused episodes stay hardlinks of their source files.
func TestDelugeDaemon_DownloadsMissingEpisodes(t *testing.T) {
	importDir := requireDaemon(t, envDelugeType)
	s := newSeeder(t)
	c := newDelugeDaemonClient(t, importDir)

	tests := []struct {
		name    string
		missing int
	}{
		{name: "first episode missing", missing: 1},
		{name: "last episode missing", missing: packEpisodes},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			name := packName(t, os.Getenv(envDelugeType))
			seeded := s.seed(t, name)
			pack := writeLinkedPack(t, importDir, name, tt.missing)
			require.Equal(t, seeded.hashes, pack.hashes, "the seeder and the import must have the same torrent")

			importDelugePack(t, c, pack.importRequest(importDir, false))
			status := waitDelugeChecked(t, c, pack.hashes.Legacy)
			assertPartialProgress(t, float64(status.Progress)/100, packEpisodes-1)

			c.mu.Lock()
			port, err := delugeDaemonAPI(t, c).GetListenPort(t.Context())
			c.mu.Unlock()
			require.NoError(t, err)
			peer := newPeerAddress(t, envOrDefault(envDelugeHost, "127.0.0.1"), int(port))
			status, complete := waitSeededDownload(t, s, pack.hashes.Legacy, peer, readDelugeTorrent(t, c, pack.hashes.Legacy),
				func(status *deluge.TorrentStatus) bool {
					// libtorrent adds payload to all_time_download once per second,
					// so the count can lag behind the progress.
					return status.Progress >= 100 && status.IsFinished && status.AllTimeDownload >= packEpisodeSize
				})
			require.True(t, complete, "download did not complete: state=%s progress=%.2f", status.State, status.Progress)
			assertDownloadedPack(t, importDir, pack, tt.missing, status.AllTimeDownload)
		})
	}
}

// newDelugeDaemonClient connects with SEASONPACKARR_TEST_DELUGE_TYPE, checks
// that the daemon major version matches it, and enables the Label plugin.
func newDelugeDaemonClient(t *testing.T, importDir string) *delugeClient {
	t.Helper()
	clientType := os.Getenv(envDelugeType)
	port, err := strconv.Atoi(envOrDefault(envDelugePort, "58846"))
	require.NoError(t, err, "parse %s", envDelugePort)

	c, err := newDelugeClient(t.Context(), &domain.Client{
		Type:     clientType,
		Host:     envOrDefault(envDelugeHost, "127.0.0.1"),
		Port:     port,
		Username: envOrDefault(envDelugeUser, "seasonpackarr"),
		Password: envOrDefault(envDelugePass, "integration"),
		Import: domain.ImportPolicy{
			SavePath: importDir,
			// Deluge stores labels in lowercase; the adapter must convert it.
			Tags: []string{"SeasonPackArr"},
		},
	})
	require.NoError(t, err, "connect to %s", clientType)
	c.pollInterval = 25 * time.Millisecond

	raw := delugeDaemonAPI(t, c)
	t.Cleanup(func() {
		assert.NoError(t, raw.Close(), "close Deluge client")
	})

	ctx, cancel := context.WithTimeout(t.Context(), daemonTimeout)
	defer cancel()
	version, err := raw.DaemonVersion(ctx)
	require.NoError(t, err)
	wantMajor := strings.TrimPrefix(clientType, "deluge-v") + "."
	require.True(t, strings.HasPrefix(version, wantMajor), "client type %s connected to Deluge %s", clientType, version)
	t.Logf("connected with %s to Deluge %s", clientType, version)

	require.NoError(t, raw.EnablePlugin(ctx, "Label"))
	enabled, err := raw.GetEnabledPlugins(ctx)
	require.NoError(t, err)
	require.True(t, slices.Contains(enabled, "Label"), "Label plugin is not enabled: %v", enabled)
	return c
}

func delugeDaemonAPI(t *testing.T, c *delugeClient) delugeTestAPI {
	t.Helper()
	raw, ok := c.c.(delugeTestAPI)
	require.True(t, ok, "adapter does not wrap a full RPC client")
	return raw
}

// importDelugePack imports through the adapter and removes the torrent, not
// its data, at cleanup.
func importDelugePack(t *testing.T, c *delugeClient, req ImportRequest) ImportReport {
	t.Helper()
	raw := delugeDaemonAPI(t, c)
	t.Cleanup(func() {
		ctx, cancel := cleanupContext(t)
		defer cancel()
		// The session list, not a status read: Deluge 1.3 returns an empty status
		// for a removed hash, which go-deluge cannot decode.
		present := func() (bool, error) {
			c.mu.Lock()
			defer c.mu.Unlock()
			ids, err := c.c.SessionState(ctx)
			return slices.ContainsFunc(ids, func(id string) bool { return strings.EqualFold(id, req.LegacyHash) }), err
		}
		if found, err := present(); !assert.NoError(t, err, "find torrent for removal") || !found {
			return
		}
		c.mu.Lock()
		_, err := raw.RemoveTorrent(ctx, req.LegacyHash, false)
		c.mu.Unlock()
		assert.NoError(t, err, "remove torrent")
		assertRemoved(t, req.LegacyHash, present)
	})
	report, err := c.Import(t.Context(), req)
	require.NoError(t, err)
	return report
}

// waitDelugeChecked waits for Deluge to leave the paused and checking states
// after the adapter resumes the torrent.
func waitDelugeChecked(t *testing.T, c *delugeClient, hash string) *deluge.TorrentStatus {
	t.Helper()
	status, checked := waitFor(t.Context(), readDelugeTorrent(t, c, hash), func(status *deluge.TorrentStatus) bool {
		state := deluge.TorrentState(status.State)
		return state != deluge.StatePaused && state != deluge.StateChecking
	})
	t.Logf("Deluge state=%s progress=%.2f", status.State, status.Progress)
	require.True(t, checked, "torrent did not leave the paused and checking states")
	require.NotEqual(t, deluge.StateError, deluge.TorrentState(status.State), "torrent entered the error state")
	return status
}

// readDelugeTorrent returns a waitFor read function that stops the test when
// the torrent is missing.
func readDelugeTorrent(t *testing.T, c *delugeClient, hash string) func() *deluge.TorrentStatus {
	return func() *deluge.TorrentStatus {
		c.mu.Lock()
		defer c.mu.Unlock()
		status, err := c.c.TorrentStatus(t.Context(), hash)
		require.NoError(t, err)
		require.NotNil(t, status, "torrent %s is missing", hash)
		return status
	}
}

func requireDelugeLabel(t *testing.T, c *delugeClient, hash, want string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), daemonTimeout)
	defer cancel()
	c.mu.Lock()
	defer c.mu.Unlock()
	plugin, err := c.label(ctx)
	require.NoError(t, err)
	reader, ok := plugin.(delugeTestLabelAPI)
	require.True(t, ok, "Label plugin does not expose label reads")
	got, err := reader.GetTorrentLabel(hash)
	require.NoError(t, err)
	require.Equal(t, want, got)
}

func envOrDefault(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
