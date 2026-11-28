// Copyright (c) 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package inventory

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/nuxencs/seasonpackarr/internal/domain"
	"github.com/nuxencs/seasonpackarr/internal/logger/loggertest"
	"github.com/nuxencs/seasonpackarr/internal/torrentclient"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	sourceFolderScanned = "source folder scanned"
	inventoryReady      = "client inventory ready"
	keptEpisode         = "Series.S01E01.1080p.WEB-DL-GRP.mkv"
)

// writeSourceFile writes an empty file and its parent folders below root.
func writeSourceFile(t *testing.T, root, name string) string {
	t.Helper()
	path := filepath.Join(root, name)
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, nil, 0o644))
	return path
}

// readAtStartupWithFolders runs the startup read for clients that each read one torrent.
func readAtStartupWithFolders(t *testing.T, ctx context.Context, clients map[string]*domain.Client) *loggertest.Logger {
	t.Helper()
	adapter := &fakeAdapter{torrents: []torrentclient.Torrent{{Name: "Series.S02E01.1080p.WEB-DL-GRP"}}}
	log := loggertest.New()
	store := New((&fakeConnector{adapter: adapter}).connect, log)

	store.ReadAtStartup(ctx, &fakeProvider{cfg: configWith(clients)})
	return log
}

// countEvents returns the number of events with the message.
func countEvents(events loggertest.Events, message string) int {
	count := 0
	for _, event := range events {
		if event["message"] == message {
			count++
		}
	}
	return count
}

func TestReadAtStartup_ScanFollowsTheWalkRules(t *testing.T) {
	tests := map[string]struct {
		setup func(t *testing.T, root string)
		want  int
	}{
		"episode file": {setup: func(*testing.T, string) {}, want: 1},
		"file deep in the tree": {setup: func(t *testing.T, root string) {
			writeSourceFile(t, root, "Series (2023)/Season 01/extra/deep/Series.S01E02.1080p.WEB-DL-GRP.mkv")
		}, want: 2},
		"extension in upper case": {setup: func(t *testing.T, root string) {
			writeSourceFile(t, root, "Series.S01E02.1080p.WEB-DL-GRP.MKV")
		}, want: 2},
		"mp4 file": {setup: func(t *testing.T, root string) {
			writeSourceFile(t, root, "Series.S01E02.1080p.WEB-DL-GRP.mp4")
		}, want: 2},
		// The scan reads names only, so a file that it cannot open still counts.
		"file without read permission": {setup: func(t *testing.T, root string) {
			require.NoError(t, os.Chmod(writeSourceFile(t, root, "Series.S01E02.1080p.WEB-DL-GRP.mkv"), 0))
		}, want: 2},
		"other extension": {setup: func(t *testing.T, root string) {
			writeSourceFile(t, root, "Series.S01E02.1080p.WEB-DL-GRP.avi")
		}, want: 1},
		"hidden file": {setup: func(t *testing.T, root string) {
			writeSourceFile(t, root, ".Series.S01E02.1080p.WEB-DL-GRP.mkv")
		}, want: 1},
		"hidden folder": {setup: func(t *testing.T, root string) {
			writeSourceFile(t, root, ".trash/Series.S01E02.1080p.WEB-DL-GRP.mkv")
		}, want: 1},
		"symlink to a file": {setup: func(t *testing.T, root string) {
			target := writeSourceFile(t, t.TempDir(), "Series.S01E02.1080p.WEB-DL-GRP.mkv")
			require.NoError(t, os.Symlink(target, filepath.Join(root, "Series.S01E02.1080p.WEB-DL-GRP.mkv")))
		}, want: 1},
		"symlink to a folder": {setup: func(t *testing.T, root string) {
			outside := t.TempDir()
			writeSourceFile(t, outside, "Series.S01E02.1080p.WEB-DL-GRP.mkv")
			require.NoError(t, os.Symlink(outside, filepath.Join(root, "Season 01")))
		}, want: 1},
		"symlink loop": {setup: func(t *testing.T, root string) {
			require.NoError(t, os.Symlink(root, filepath.Join(root, "loop")))
		}, want: 1},
		"folder with a video name": {setup: func(t *testing.T, root string) {
			require.NoError(t, os.Mkdir(filepath.Join(root, "Series.S01E02.1080p.WEB-DL-GRP.mkv"), 0o755))
		}, want: 1},
		"sample": {setup: func(t *testing.T, root string) {
			writeSourceFile(t, root, "Series.S01E02.1080p.WEB-DL-GRP-sample.mkv")
		}, want: 1},
		"obfuscated name": {setup: func(t *testing.T, root string) {
			writeSourceFile(t, root, "a3f9c1e07b2d4e8f9a0b.mkv")
		}, want: 1},
		"no episode": {setup: func(t *testing.T, root string) {
			writeSourceFile(t, root, "Extras/Interview.1080p.WEB-DL.mkv")
		}, want: 1},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			writeSourceFile(t, root, keptEpisode)
			tt.setup(t, root)

			log := readAtStartupWithFolders(t, t.Context(), map[string]*domain.Client{"default": {Host: "qbit", SourceFolders: []string{root}}})

			events := log.Events(t)
			scanned := events.RequireField(t, sourceFolderScanned, "folder", root)
			assert.EqualValues(t, tt.want, scanned["episode_files"])
			assert.Contains(t, scanned, "duration_ms")
			assert.EqualValues(t, tt.want, events.RequireField(t, inventoryReady, "clientname", "default")["source_folder_files"])
		})
	}
}

func TestReadAtStartup_ScansASourceFolderThatIsASymlink(t *testing.T) {
	target := t.TempDir()
	writeSourceFile(t, target, keptEpisode)
	root := filepath.Join(t.TempDir(), "tv")
	require.NoError(t, os.Symlink(target, root))

	log := readAtStartupWithFolders(t, t.Context(), map[string]*domain.Client{"default": {Host: "qbit", SourceFolders: []string{root}}})

	assert.EqualValues(t, 1, log.Events(t).RequireField(t, sourceFolderScanned, "folder", root)["episode_files"])
}

func TestReadAtStartup_WarnsAboutAMissingSourceFolder(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing")
	present := t.TempDir()
	writeSourceFile(t, present, keptEpisode)

	log := readAtStartupWithFolders(t, t.Context(), map[string]*domain.Client{"default": {Host: "qbit", SourceFolders: []string{missing, present}}})

	events := log.Events(t)
	warning := events.RequireField(t, "could not read source folder", "folder", missing)
	assert.Equal(t, "warn", warning["level"])
	assert.Equal(t, "default", warning["clientname"])
	assert.Contains(t, warning["error"], "no such file or directory")
	assert.EqualValues(t, 1, events.RequireField(t, inventoryReady, "clientname", "default")["source_folder_files"], "the other folder counts")
	assert.Equal(t, 1, countEvents(events, sourceFolderScanned), "a missing folder has no scan line")
}

func TestReadAtStartup_SkipsASubfolderThatCannotBeRead(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root reads each folder")
	}
	root := t.TempDir()
	writeSourceFile(t, root, keptEpisode)
	locked := filepath.Join(root, "locked")
	writeSourceFile(t, locked, "Series.S01E02.1080p.WEB-DL-GRP.mkv")
	require.NoError(t, os.Chmod(locked, 0))
	t.Cleanup(func() { _ = os.Chmod(locked, 0o755) })

	log := readAtStartupWithFolders(t, t.Context(), map[string]*domain.Client{"default": {Host: "qbit", SourceFolders: []string{root}}})

	events := log.Events(t)
	warning := events.RequireField(t, "could not read a folder in a source folder; skipped it", "path", locked)
	assert.Equal(t, "warn", warning["level"])
	assert.Equal(t, root, warning["folder"])
	assert.EqualValues(t, 1, events.RequireField(t, sourceFolderScanned, "folder", root)["episode_files"], "the rest of the scan counts")
}

func TestReadAtStartup_ScansAFolderOfTwoClientsOneTime(t *testing.T) {
	root := t.TempDir()
	writeSourceFile(t, root, keptEpisode)
	writeSourceFile(t, root, "Series.S01E02.1080p.WEB-DL-GRP.mkv")

	log := readAtStartupWithFolders(t, t.Context(), map[string]*domain.Client{
		"hd":  {Host: "qbit", Import: domain.ImportPolicy{Category: "tv-hd"}, SourceFolders: []string{root}},
		"uhd": {Host: "qbit", Import: domain.ImportPolicy{Category: "tv-uhd"}, SourceFolders: []string{root}},
	})

	events := log.Events(t)
	assert.Equal(t, 1, countEvents(events, sourceFolderScanned), "clients with the same path share one scan")
	assert.EqualValues(t, 2, events.RequireField(t, inventoryReady, "clientname", "hd")["source_folder_files"])
	assert.EqualValues(t, 2, events.RequireField(t, inventoryReady, "clientname", "uhd")["source_folder_files"])
}

func TestReadAtStartup_LogsAsBeforeForAClientWithoutSourceFolders(t *testing.T) {
	root := t.TempDir()
	writeSourceFile(t, root, keptEpisode)

	log := readAtStartupWithFolders(t, t.Context(), map[string]*domain.Client{
		"default": {Host: "qbit"},
		"usenet":  {Host: "qbit", Import: domain.ImportPolicy{Category: "tv-usenet"}, SourceFolders: []string{root}},
	})

	ready := log.Events(t).RequireField(t, inventoryReady, "clientname", "default")
	assert.NotContains(t, ready, "source_folder_files")
	assert.EqualValues(t, 1, ready["torrents"])
}

func TestReadAtStartup_ScansNoSourceFolderAfterTheContextEnds(t *testing.T) {
	root := t.TempDir()
	writeSourceFile(t, root, keptEpisode)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	log := readAtStartupWithFolders(t, ctx, map[string]*domain.Client{"default": {Host: "qbit", SourceFolders: []string{root}}})

	assert.Empty(t, log.Events(t), "a shutdown is not a failure")
}
