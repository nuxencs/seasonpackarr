// Copyright (c) 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package seasonpack

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/nuxencs/seasonpackarr/internal/domain"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMatch_RejectsBelowThresholdWithoutSourceFolder(t *testing.T) {
	pack := newPack(t, "FolderNone", 2, 1)
	pack.writeFolderFile(t, 2)
	f := newFixture(pack.client)

	outcome := f.processor.Match(t.Context(), f.client(t, "default", newSmartConfig(1)), pack.name, TorrentBytes(pack.torrent))

	assert.Equal(t, domain.StatusBelowThreshold, outcome.Status)
	assert.Equal(t, &Coverage{Reusable: 1, Total: 2}, outcome.Coverage, "a folder that the client does not name adds nothing")
}

// A file removed or replaced after the scan is dropped at the match with its log line. The coverage rule then decides.
// A symlink is dropped also when its target has the size of the file in the torrent, because a hardlink of a symlink
// links the symlink itself on Linux.
func TestMatch_DropsSourceFolderFileChangedAfterScan(t *testing.T) {
	remove := func(t *testing.T, path string) {
		require.NoError(t, os.Remove(path))
	}
	replaceWithSymlink := func(t *testing.T, path string) {
		target := filepath.Join(t.TempDir(), filepath.Base(path))
		writeEpisode(t, target)
		remove(t, path)
		require.NoError(t, os.Symlink(target, path))
	}
	tests := map[string]struct {
		cfg      domain.Config
		change   func(t *testing.T, path string)
		status   domain.StatusCode
		accepted bool
		reason   string
	}{
		"removed, accepted with no threshold": {
			cfg: newConfig(), change: remove, status: domain.StatusSuccessfulMatch, accepted: true, reason: "missing",
		},
		"removed, rejected below threshold": {
			cfg: newSmartConfig(1), change: remove, status: domain.StatusBelowThreshold, reason: "missing",
		},
		"replaced by a symlink, accepted with no threshold": {
			cfg: newConfig(), change: replaceWithSymlink, status: domain.StatusSuccessfulMatch, accepted: true,
			reason: "not_regular_file",
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			pack := newPack(t, "FolderChanged", 2, 0)
			pack.writeFolderFile(t, 1)
			changed := pack.writeFolderFile(t, 2)
			cfg := withSourceFolders(tt.cfg, pack.folder())
			f := newFixture(pack.client)
			f.scanSourceFolders(t, cfg)
			tt.change(t, changed)

			outcome := f.processor.Match(t.Context(), f.client(t, "default", cfg), pack.name, TorrentFetch(func(context.Context) ([]byte, error) {
				return pack.torrent, nil
			}))

			assert.Equal(t, tt.status, outcome.Status)
			assert.Equal(t, tt.accepted, outcome.Err == nil)
			assert.Equal(t, &Coverage{Reusable: 1, Total: 2}, outcome.Coverage)
			dropped := f.log.Events(t).RequireField(t, "client episode file is not reusable", "source", changed)
			assert.Equal(t, tt.reason, dropped["reason"])
			assert.Equal(t, "source_folder", dropped["source_kind"])
		})
	}
}

// Each file of the candidates was removed after the scan. That is no failed file-detail read, so the discovery match
// stops with 445 and not with 468, and it does not fetch the torrent.
func TestMatch_FetchStopsWithNoSourceWhenSourceFolderFilesAreGone(t *testing.T) {
	pack := newPack(t, "FolderGone", 1, 0)
	path := pack.writeFolderFile(t, 1)
	cfg := withSourceFolders(newConfig(), pack.folder())
	f := newFixture(pack.client)
	f.scanSourceFolders(t, cfg)
	require.NoError(t, os.Remove(path))
	fetched := false

	outcome := f.processor.Match(t.Context(), f.client(t, "default", cfg), pack.name, TorrentFetch(func(context.Context) ([]byte, error) {
		fetched = true
		return pack.torrent, nil
	}))

	assert.ErrorIs(t, outcome.Err, ErrNoSourceOnDisk)
	assert.Equal(t, domain.StatusFailedMatchToTorrentEps, outcome.Status)
	assert.False(t, fetched)
}

// A file of a source folder is a source on disk for the discovery match.
func TestMatch_FetchesTorrentForSourceFolderFile(t *testing.T) {
	pack := newPack(t, "FolderFetch", 1, 0)
	pack.writeFolderFile(t, 1)
	cfg := withSourceFolders(newConfig(), pack.folder())
	f := newFixture(pack.client)
	f.scanSourceFolders(t, cfg)

	outcome := f.processor.Match(t.Context(), f.client(t, "default", cfg), pack.name, TorrentFetch(func(context.Context) ([]byte, error) {
		return pack.torrent, nil
	}))

	require.NoError(t, outcome.Err)
	assert.Equal(t, &Coverage{Reusable: 1, Total: 1}, outcome.Coverage)
	assert.Zero(t, pack.client.FileBatchCalls, "a client with no torrent candidate needs no file-detail read")
}
