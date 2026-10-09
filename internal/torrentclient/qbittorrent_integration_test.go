// Copyright (c) 2023 - 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

//go:build integration

package torrentclient

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/nuxencs/seasonpackarr/internal/domain"

	"github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestQbitDaemon_ImportsCompletePack(t *testing.T) {
	importDir := requireDaemon(t, envQbitHost)
	c := newQbitDaemonClient(t, domain.ImportPolicy{SavePath: importDir, Tags: []string{"seasonpackarr"}})
	pack := writeCompletePack(t, importDir, packName(t))

	importQbitPack(t, c, pack.importRequest(importDir, true))

	tor, active := waitQbitActive(t, c, pack.hashes.Legacy)
	assert.True(t, active, "import left the torrent in state %s, but it must start", tor.State)
	requireListedPack(t, c, pack, importDir)
	requireFileReadLoad(t, c, pack.hashes.Legacy)
}

func TestQbitDaemon_ResumesPartialPack(t *testing.T) {
	importDir := requireDaemon(t, envQbitHost)
	c := newQbitDaemonClient(t, domain.ImportPolicy{SavePath: importDir})
	pack := writePartialPack(t, importDir, packName(t), 1)

	importQbitPack(t, c, pack.importRequest(importDir, false))

	tor, active := waitQbitActive(t, c, pack.hashes.Legacy)
	assert.True(t, active, "qBittorrent did not start the torrent after its check: state=%s", tor.State)
	assertPartialProgress(t, tor.Progress, 1)
}

// TestQbitDaemon_RecoversMisclassifiedCompletePack sends a partial pack as
// complete. The paused skip-check add reports missingFiles, and recheck, stop,
// start must let qBittorrent check and start it. Without stop, the torrent
// stays stopped after the check because the FilesChecked stop condition remains.
func TestQbitDaemon_RecoversMisclassifiedCompletePack(t *testing.T) {
	importDir := requireDaemon(t, envQbitHost)
	c := newQbitDaemonClient(t, domain.ImportPolicy{SavePath: importDir})
	pack := writePartialPack(t, importDir, packName(t), 1)

	report := importQbitPack(t, c, pack.importRequest(importDir, true))
	require.Contains(t, importStageNames(report), ImportStageRecheck, "the missingFiles fallback must run")

	tor, active := waitQbitActive(t, c, pack.hashes.Legacy)
	assert.True(t, active, "torrent not active after the fallback: state=%s", tor.State)
	assertPartialProgress(t, tor.Progress, 1)
}

func TestQbitDaemon_ImportDestinationFollowsPreferences(t *testing.T) {
	importDir := requireDaemon(t, envQbitHost)
	category := fmt.Sprintf("seasonpackarr-integration-%d", time.Now().UnixNano())
	categoryPath := filepath.Join(importDir, "category")
	c := newQbitDaemonClient(t, domain.ImportPolicy{Category: category, ContentLayout: "subfolder"})
	raw := qbitDaemonAPI(t, c)

	original, err := raw.GetAppPreferences()
	require.NoError(t, err)
	t.Cleanup(func() {
		assert.NoError(t, raw.SetPreferences(map[string]any{
			"auto_tmm_enabled":                  original.AutoTmmEnabled,
			"use_category_paths_in_manual_mode": original.UseCategoryPathsInManualMode,
			"save_path":                         original.SavePath,
		}), "restore preferences")
		assert.NoError(t, raw.RemoveCategories([]string{category}), "remove category")
	})
	require.NoError(t, raw.SetPreferences(map[string]any{"save_path": importDir}))
	require.NoError(t, raw.CreateCategory(category, categoryPath))

	tests := []struct {
		name               string
		autoTMM            bool
		manualCategoryPath bool
		want               string
	}{
		{name: "automatic management", autoTMM: true, want: categoryPath},
		{name: "manual global path", want: importDir},
		{name: "manual category path", manualCategoryPath: true, want: categoryPath},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.NoError(t, raw.SetPreferences(map[string]any{
				"auto_tmm_enabled":                  tt.autoTMM,
				"use_category_paths_in_manual_mode": tt.manualCategoryPath,
			}))
			if tt.manualCategoryPath {
				// qBittorrent before 4.5 ignores this preference and uses the
				// default save path, which the manual global path case covers.
				prefs, err := raw.GetAppPreferences()
				require.NoError(t, err)
				if !prefs.UseCategoryPathsInManualMode {
					t.Skip("daemon does not support category paths in manual mode")
				}
			}

			destination, err := c.ImportDestination(t.Context())
			require.NoError(t, err)
			require.Equal(t, normalizePath(tt.want), destination.SavePath())
		})
	}
}

func newQbitDaemonClient(t *testing.T, policy domain.ImportPolicy) *qbitClient {
	t.Helper()
	c, err := newQbitClient(t.Context(), &domain.Client{
		Host:     os.Getenv(envQbitHost),
		Username: os.Getenv(envQbitUser),
		Password: os.Getenv(envQbitPass),
		Import:   policy,
	})
	require.NoError(t, err)
	return c
}

// qbitDaemonAPI returns the full Web API client for setup and teardown calls
// that the adapter does not use.
func qbitDaemonAPI(t *testing.T, c *qbitClient) *qbittorrent.Client {
	t.Helper()
	raw, ok := c.c.(*qbittorrent.Client)
	require.True(t, ok, "adapter does not wrap a Web API client")
	return raw
}

// importQbitPack imports through the adapter and removes the torrent, not its
// data, at cleanup. qBittorrent 5.2 rejects a duplicate add, so a torrent left
// behind fails the next run.
func importQbitPack(t *testing.T, c *qbitClient, req ImportRequest) ImportReport {
	t.Helper()
	raw := qbitDaemonAPI(t, c)
	t.Cleanup(func() {
		ctx, cancel := cleanupContext(t)
		defer cancel()
		assert.NoError(t, raw.DeleteTorrents([]string{req.LegacyHash}, false), "remove torrent")
		assertRemoved(t, ctx, req.LegacyHash, func() (bool, error) {
			found, err := raw.GetTorrents(qbittorrent.TorrentFilterOptions{Hashes: []string{req.LegacyHash}})
			return len(found) > 0, err
		})
	})
	report, err := c.Import(t.Context(), req)
	require.NoError(t, err)
	return report
}

// waitQbitActive waits for qBittorrent to start the torrent after its add-time
// check. It returns the last state so callers can report it.
func waitQbitActive(t *testing.T, c *qbitClient, hash string) (qbittorrent.Torrent, bool) {
	t.Helper()
	tor, active := waitFor(t.Context(), func() qbittorrent.Torrent {
		found, ok, err := c.lookupTorrent(t.Context(), hash)
		require.NoError(t, err)
		require.True(t, ok, "torrent %s is missing", hash)
		return found
	}, func(tor qbittorrent.Torrent) bool {
		return isActiveTorrentState(tor.State)
	})
	t.Logf("qBittorrent state=%s progress=%.2f", tor.State, tor.Progress)
	return tor, active
}
