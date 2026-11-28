// Copyright (c) 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package seasonpack

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nuxencs/seasonpackarr/internal/domain"
	"github.com/nuxencs/seasonpackarr/internal/placement"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestImport_LinksSourceFolderFiles(t *testing.T) {
	pack := newPack(t, "FolderImport", 2, 0)
	first := pack.writeFolderFile(t, 1)
	second := pack.writeFolderFile(t, 2)
	cfg := withSourceFolders(newConfig(), pack.folder())
	f := newFixture(pack.client)
	f.scanSourceFolders(t, cfg)

	outcome := f.processor.Match(t.Context(), f.client(t, "default", cfg), pack.name, TorrentBytes(pack.torrent))
	require.NoError(t, outcome.Err)
	require.Equal(t, &Coverage{Reusable: 2, Total: 2}, outcome.Coverage)
	entries, err := os.ReadDir(pack.importDir)
	require.NoError(t, err)
	require.Empty(t, entries, "the match changes nothing on disk")

	outcome = f.processor.Import(t.Context(), f.client(t, "default", cfg), pack.name, pack.torrent)

	require.NoError(t, outcome.Err)
	assert.Equal(t, domain.StatusSuccessfulHardlink, outcome.Status)
	assert.Equal(t, &Coverage{Reusable: 2, Total: 2}, outcome.Coverage)
	requireSameFile(t, first, pack.target(1))
	requireSameFile(t, second, pack.target(2))
	assert.True(t, pack.client.ImportReq.DataComplete)
	assert.Zero(t, pack.client.FileBatchCalls, "a client with no torrent candidate needs no file-detail read")
	linked := 0
	for _, event := range f.log.Events(t) {
		if message, _ := event["message"].(string); strings.HasPrefix(message, "created or reused hardlink") {
			assert.Equal(t, "source_folder", event["source_kind"])
			linked++
		}
	}
	assert.Equal(t, 2, linked)
}

// The client has E01 as a torrent, and the source folder has E02. Smart mode at 100 percent accepts the pack only
// because it counts the file.
func TestImport_CountsTorrentAndSourceFolderEpisodes(t *testing.T) {
	pack := newPack(t, "FolderMixed", 2, 1)
	file := pack.writeFolderFile(t, 2)
	cfg := withSourceFolders(newSmartConfig(1), pack.folder())
	f := newFixture(pack.client)
	f.scanSourceFolders(t, cfg)

	outcome := f.processor.Import(t.Context(), f.client(t, "default", cfg), pack.name, pack.torrent)

	require.NoError(t, outcome.Err)
	assert.Equal(t, &Coverage{Reusable: 2, Total: 2}, outcome.Coverage)
	requireSameFile(t, pack.source(1), pack.target(1))
	requireSameFile(t, file, pack.target(2))
}

// A torrent and a file of a source folder supply E01. The coverage counts the episode once, and the torrent wins.
func TestImport_TorrentWinsOverSourceFolderFile(t *testing.T) {
	pack := newPack(t, "FolderWins", 1, 1)
	file := pack.writeFolderFile(t, 1)
	cfg := withSourceFolders(newConfig(), pack.folder())
	f := newFixture(pack.client)
	f.scanSourceFolders(t, cfg)

	outcome := f.processor.Import(t.Context(), f.client(t, "default", cfg), pack.name, pack.torrent)

	require.NoError(t, outcome.Err)
	assert.Equal(t, &Coverage{Reusable: 1, Total: 1}, outcome.Coverage)
	requireSameFile(t, pack.source(1), pack.target(1))
	targetInfo, err := os.Stat(pack.target(1))
	require.NoError(t, err)
	fileInfo, err := os.Stat(file)
	require.NoError(t, err)
	assert.False(t, os.SameFile(targetInfo, fileInfo), "the torrent source must win over the file")
}

// Each file is of the season and the release variant of the pack by its name, but cannot be linked. The pack has one
// episode, so a link would give an accepted import.
func TestImport_DoesNotLinkUnfitSourceFolderFile(t *testing.T) {
	tests := map[string]struct {
		name string
		data []byte
	}{
		"another size":       {name: "FolderUnfit.S01E01.1080p.WEB-DL.H.264-RlsGrp.mkv", data: []byte("00")},
		"a sample":           {name: "FolderUnfit.S01E01.1080p.WEB-DL.H.264-RlsGrp.sample.mkv", data: []byte("0")},
		"another container":  {name: "FolderUnfit.S01E01.1080p.WEB-DL.H.264-RlsGrp.mp4", data: []byte("0")},
		"no parsable name":   {name: "a1b2c3d4e5f6.mkv", data: []byte("0")},
		"no episode in name": {name: "FolderUnfit - Pilot - WEBDL-1080p.mkv", data: []byte("0")},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			pack := newPack(t, "FolderUnfit", 1, 0)
			path := pack.writeFolderFileNamed(t, tt.name)
			require.NoError(t, os.WriteFile(path, tt.data, 0o644))
			cfg := withSourceFolders(newConfig(), pack.folder())
			f := newFixture(pack.client)
			f.scanSourceFolders(t, cfg)

			outcome := f.processor.Import(t.Context(), f.client(t, "default", cfg), pack.name, pack.torrent)

			require.Error(t, outcome.Err)
			assert.Contains(t, []domain.StatusCode{domain.StatusNoMatches, domain.StatusFailedMatchToTorrentEps}, outcome.Status)
			assert.NoFileExists(t, pack.target(1))
			assert.Zero(t, pack.client.ImportCalls)
		})
	}
}

// The source folder is the import root, so the files of the pack folder are at their own hardlink targets. The import
// keeps each file as it is and imports the pack.
func TestImport_KeepsSourceFolderFileAtItsOwnHardlinkTarget(t *testing.T) {
	pack := newPack(t, "FolderOwnTarget", 2, 0)
	for episode := 1; episode <= 2; episode++ {
		require.NoError(t, os.MkdirAll(filepath.Dir(pack.target(episode)), 0o755))
		require.NoError(t, os.WriteFile(pack.target(episode), []byte("0"), 0o644))
	}
	cfg := withSourceFolders(newConfig(), pack.importDir)
	f := newFixture(pack.client)
	f.scanSourceFolders(t, cfg)
	before, err := os.Stat(pack.target(1))
	require.NoError(t, err)

	outcome := f.processor.Import(t.Context(), f.client(t, "default", cfg), pack.name, pack.torrent)

	require.NoError(t, outcome.Err)
	assert.Equal(t, domain.StatusSuccessfulHardlink, outcome.Status)
	assert.Equal(t, &Coverage{Reusable: 2, Total: 2}, outcome.Coverage)
	after, err := os.Stat(pack.target(1))
	require.NoError(t, err)
	assert.True(t, os.SameFile(before, after), "the file must stay the same file")
	contents, err := os.ReadFile(pack.target(1))
	require.NoError(t, err)
	assert.Equal(t, []byte("0"), contents)
	assert.True(t, pack.client.ImportReq.DataComplete)
}

// A file of a source folder moved after the match. The import discards the inventory and builds the plan once again,
// and the stat call drops the file. The discard keeps the scan results, so E01 still links. The refresh starts no
// scan, so the file at its new path does not count.
func TestImport_RefreshDropsMovedSourceFolderFileWithoutScan(t *testing.T) {
	pack := newPack(t, "FolderMoved", 2, 0)
	pack.writeFolderFile(t, 1)
	moved := pack.writeFolderFile(t, 2)
	cfg := withSourceFolders(newConfig(), pack.folder())
	f := newFixture(pack.client)
	f.scanSourceFolders(t, cfg)
	outcome := f.processor.Match(t.Context(), f.client(t, "default", cfg), pack.name, TorrentBytes(pack.torrent))
	require.NoError(t, outcome.Err)
	require.Equal(t, &Coverage{Reusable: 2, Total: 2}, outcome.Coverage)
	newPath := filepath.Join(pack.folder(), "Season 1", pack.episodeFile(2))
	require.NoError(t, os.MkdirAll(filepath.Dir(newPath), 0o755))
	require.NoError(t, os.Rename(moved, newPath))

	outcome = f.processor.Import(t.Context(), f.client(t, "default", cfg), pack.name, pack.torrent)

	require.NoError(t, outcome.Err)
	assert.Equal(t, &Coverage{Reusable: 1, Total: 2}, outcome.Coverage)
	f.log.Events(t).Require(t, "import plan refreshed after missing hardlink source")
	assert.FileExists(t, pack.target(1))
	assert.NoFileExists(t, pack.target(2))
	assert.False(t, pack.client.ImportReq.DataComplete)
}

// After the scan, a tool replaced the season folder with a symlink (issue #426). The files keep their names and sizes
// at the place of the symlink. A file is a hardlink source only when its path resolves below the source folder with
// the rule of os.Root: a relative symlink that stays below the folder is allowed, and each absolute symlink is refused.
// Without a plan of a match, the plan build of the import drops a refused file. With the plan of a match, the link
// refuses it.
func TestImport_LinksSourceFolderFileOnlyBelowTheFolder(t *testing.T) {
	tests := map[string]struct {
		// move returns the new place of the season folder and the target of the symlink.
		move   func(pack packFixture, outside string) (dir, linkTarget string)
		linked bool
	}{
		"absolute symlink out of the folder": {
			move: func(_ packFixture, outside string) (string, string) {
				return outside, outside
			},
		},
		"relative symlink out of the folder": {
			move: func(pack packFixture, outside string) (string, string) {
				rel, err := filepath.Rel(pack.folder(), outside)
				if err != nil {
					panic(err)
				}
				return outside, rel
			},
		},
		"absolute symlink below the folder": {
			move: func(pack packFixture, _ string) (string, string) {
				dir := filepath.Join(pack.folder(), "moved")
				return dir, dir
			},
		},
		"relative symlink below the folder": {
			move: func(pack packFixture, _ string) (string, string) {
				return filepath.Join(pack.folder(), "moved"), "moved"
			},
			linked: true,
		},
	}
	for name, tt := range tests {
		for _, cached := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s, cached plan %t", name, cached), func(t *testing.T) {
				pack := newPack(t, "FolderEscape", 1, 0)
				pack.writeFolderFile(t, 1)
				cfg := withSourceFolders(newConfig(), pack.folder())
				f := newFixture(pack.client)
				f.scanSourceFolders(t, cfg)
				if cached {
					outcome := f.processor.Match(t.Context(), f.client(t, "default", cfg), pack.name, TorrentBytes(pack.torrent))
					require.NoError(t, outcome.Err)
				}
				dir, linkTarget := tt.move(pack, filepath.Join(filepath.Dir(pack.sourceDir), "outside", "Season 01"))
				pack.moveSeasonFolder(t, dir, linkTarget)
				moved := filepath.Join(dir, pack.episodeFile(1))

				outcome := f.processor.Import(t.Context(), f.client(t, "default", cfg), pack.name, pack.torrent)

				if tt.linked {
					require.NoError(t, outcome.Err)
					assert.Equal(t, domain.StatusSuccessfulHardlink, outcome.Status)
					requireSameFile(t, moved, pack.target(1))
					return
				}
				require.Error(t, outcome.Err)
				assert.Zero(t, pack.client.ImportCalls)
				assert.NoFileExists(t, pack.target(1))
				requireNoHardlinkBelow(t, pack.importDir, moved)
				if cached {
					// A refused source is a plain failed link. It starts no plan refresh.
					assert.Equal(t, domain.StatusFailedHardlink, outcome.Status)
					failed := f.log.Events(t).Require(t, "error creating hardlink: "+filepath.Join(pack.seasonFolder(), pack.episodeFile(1)))
					assert.Contains(t, failed["error"], placement.ErrSourceOutsideSourceFolder.Error())
					for _, event := range f.log.Events(t) {
						assert.NotEqual(t, "import plan refreshed after missing hardlink source", event["message"])
					}
					return
				}
				assert.Equal(t, domain.StatusFailedMatchToTorrentEps, outcome.Status)
				dropped := f.log.Events(t).Require(t, "client episode file is not reusable")
				assert.Equal(t, "outside_source_folder", dropped["reason"])
				assert.Equal(t, "source_folder", dropped["source_kind"])
			})
		}
	}
}

// The scan follows a source folder that is a symlink, so its files stay hardlink sources.
func TestImport_LinksFileOfSymlinkedSourceFolder(t *testing.T) {
	pack := newPack(t, "FolderSymlinked", 1, 0)
	file := pack.writeFolderFile(t, 1)
	link := filepath.Join(filepath.Dir(pack.folder()), "source-folder-link")
	require.NoError(t, os.Symlink(pack.folder(), link))
	cfg := withSourceFolders(newConfig(), link)
	f := newFixture(pack.client)
	f.scanSourceFolders(t, cfg)

	outcome := f.processor.Import(t.Context(), f.client(t, "default", cfg), pack.name, pack.torrent)

	require.NoError(t, outcome.Err)
	assert.Equal(t, domain.StatusSuccessfulHardlink, outcome.Status)
	requireSameFile(t, file, pack.target(1))
}

// The client has a source folder below another. A file is checked against the folder whose scan listed it first, in
// the order of the config, and no second folder is tried. After the scan, a relative symlink replaced the season
// folder. It stays below the outer folder and leaves the inner one.
func TestImport_ChecksFileAgainstFirstSourceFolderThatListsIt(t *testing.T) {
	for _, innerFirst := range []bool{false, true} {
		t.Run(fmt.Sprintf("inner folder first %t", innerFirst), func(t *testing.T) {
			pack := newPack(t, "FolderNested", 1, 0)
			pack.writeFolderFile(t, 1)
			outer := filepath.Dir(pack.folder())
			folders := []string{outer, pack.folder()}
			if innerFirst {
				folders = []string{pack.folder(), outer}
			}
			cfg := withSourceFolders(newConfig(), folders...)
			f := newFixture(pack.client)
			f.scanSourceFolders(t, cfg)
			moved := filepath.Join(outer, "moved")
			pack.moveSeasonFolder(t, moved, filepath.Join("..", "moved"))

			outcome := f.processor.Import(t.Context(), f.client(t, "default", cfg), pack.name, pack.torrent)

			if innerFirst {
				require.Error(t, outcome.Err)
				assert.NoFileExists(t, pack.target(1))
				dropped := f.log.Events(t).Require(t, "client episode file is not reusable")
				assert.Equal(t, "outside_source_folder", dropped["reason"])
				return
			}
			require.NoError(t, outcome.Err)
			requireSameFile(t, filepath.Join(moved, pack.episodeFile(1)), pack.target(1))
		})
	}
}
