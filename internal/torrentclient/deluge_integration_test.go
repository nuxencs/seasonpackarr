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
	RemoveTorrent(context.Context, string, bool) (bool, error)
}

type delugeTestLabelAPI interface {
	GetTorrentLabel(string) (string, error)
}

func TestDelugeDaemon_ImportsCompletePack(t *testing.T) {
	importDir := requireDaemon(t, envDelugeType)
	c := newDelugeDaemonClient(t, importDir)
	pack := writeCompletePack(t, importDir)

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
	pack := writePartialPack(t, importDir, 1)

	importDelugePack(t, c, pack.importRequest(importDir, false))

	status := waitDelugeChecked(t, c, pack.hashes.Legacy)
	assertPartialProgress(t, float64(status.Progress)/100, 1)
	requireDelugeLabel(t, c, pack.hashes.Legacy, "seasonpackarr")
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

	ctx, cancel := context.WithTimeout(t.Context(), delugeTimeout)
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
		c.mu.Lock()
		defer c.mu.Unlock()
		found, err := c.c.TorrentsStatus(ctx, deluge.StateUnspecified, []string{req.LegacyHash})
		if !assert.NoError(t, err, "find torrent for removal") || len(found) == 0 {
			return
		}
		_, err = raw.RemoveTorrent(ctx, req.LegacyHash, false)
		assert.NoError(t, err, "remove torrent")
	})
	report, err := c.Import(t.Context(), req)
	require.NoError(t, err)
	return report
}

// waitDelugeChecked waits for Deluge to leave the paused and checking states
// after the adapter resumes the torrent.
func waitDelugeChecked(t *testing.T, c *delugeClient, hash string) *deluge.TorrentStatus {
	t.Helper()
	status, checked := waitFor(t, func() *deluge.TorrentStatus {
		c.mu.Lock()
		defer c.mu.Unlock()
		status, err := c.c.TorrentStatus(t.Context(), hash)
		require.NoError(t, err)
		require.NotNil(t, status, "torrent %s is missing", hash)
		return status
	}, func(status *deluge.TorrentStatus) bool {
		state := deluge.TorrentState(status.State)
		return state != deluge.StatePaused && state != deluge.StateChecking
	})
	t.Logf("Deluge state=%s progress=%.2f", status.State, status.Progress)
	require.True(t, checked, "torrent did not leave the paused and checking states")
	require.NotEqual(t, deluge.StateError, deluge.TorrentState(status.State), "torrent entered the error state")
	return status
}

func requireDelugeLabel(t *testing.T, c *delugeClient, hash, want string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), delugeTimeout)
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
