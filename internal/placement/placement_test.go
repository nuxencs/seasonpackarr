// Copyright (c) 2023 - 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package placement

import (
	"bytes"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/nuxencs/seasonpackarr/internal/torrents"

	"github.com/autobrr/go-torrent/bencode"
	"github.com/autobrr/go-torrent/metainfo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	packName = "Show.S01.1080p.WEB-DL.H.264-RlsGrp"
	episode1 = "Show.S01E01.1080p.WEB-DL.H.264-RlsGrp.mkv"
	episode2 = "Show.S01E02.1080p.WEB-DL.H.264-RlsGrp.mkv"
)

// newInfo parses a torrent with the given metadata, so each test gets its
// torrent info through the parse, as production does.
func newInfo(t *testing.T, raw metainfo.Info) torrents.Info {
	t.Helper()

	raw.PieceLength = 256 * 1024
	infoBytes, err := bencode.Marshal(raw)
	require.NoError(t, err)
	var torrent bytes.Buffer
	require.NoError(t, (&metainfo.MetaInfo{InfoBytes: infoBytes}).Write(&torrent))

	info, err := torrents.ParseInfo(torrent.Bytes())
	require.NoError(t, err)
	return info
}

// newPackInfo is a directory torrent with two episodes of 7 bytes each.
func newPackInfo(t *testing.T) torrents.Info {
	t.Helper()
	return newInfo(t, metainfo.Info{
		Name: packName,
		Files: []metainfo.FileInfo{
			{Path: []string{episode1}, Length: 7},
			{Path: []string{episode2}, Length: 7},
		},
	})
}

// writeSource writes a reusable episode of 7 bytes outside of the import root.
func writeSource(t *testing.T, dir, name string) string {
	t.Helper()
	source := filepath.Join(dir, name)
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(source, []byte("episode"), 0o644))
	return source
}

func requireSameFile(t *testing.T, want, got string) {
	t.Helper()
	wantInfo, err := os.Stat(want)
	require.NoError(t, err)
	gotInfo, err := os.Stat(got)
	require.NoError(t, err)
	require.True(t, os.SameFile(wantInfo, gotInfo), "%s is not a hardlink to %s", got, want)
}

func TestLink_PutsFilesInPackFolder(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "import")
	source1 := writeSource(t, filepath.Join(dir, "library"), "one.mkv")
	source2 := writeSource(t, filepath.Join(dir, "library"), "two.mkv")

	outcomes := New(newPackInfo(t), PackFolderDestination(root)).Link([]PlannedLink{
		{Source: source1, TorrentPath: episode1},
		{Source: source2, TorrentPath: episode2},
	})

	target1 := filepath.Join(root, packName, episode1)
	target2 := filepath.Join(root, packName, episode2)
	require.Equal(t, []Outcome{
		{Status: Linked, Target: target1},
		{Status: Linked, Target: target2},
	}, outcomes)
	requireSameFile(t, source1, target1)
	requireSameFile(t, source2, target2)
}

func TestLink_PutsFilesBelowRootForFlatLayout(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "import")
	source := writeSource(t, filepath.Join(dir, "library"), "one.mkv")

	outcomes := New(newPackInfo(t), FlatDestination(root)).Link([]PlannedLink{{Source: source, TorrentPath: episode1}})

	target := filepath.Join(root, episode1)
	require.Equal(t, []Outcome{{Status: Linked, Target: target}}, outcomes)
	requireSameFile(t, source, target)
	assert.NoDirExists(t, filepath.Join(root, packName))
}

func TestDestination(t *testing.T) {
	assert.Equal(t, PackFolderDestination("/data/tv"), PackFolderDestination("/data/tv"))
	assert.Equal(t, FlatDestination("/data/tv"), FlatDestination("/data/tv"))
	assert.NotEqual(t, PackFolderDestination("/data/tv"), FlatDestination("/data/tv"))
	assert.NotEqual(t, FlatDestination("/data/tv"), FlatDestination("/data/movies"))
	assert.Equal(t, "/data/tv", FlatDestination("/data/tv").Root())
	assert.Equal(t, "/data/tv", PackFolderDestination("/data/tv").Root())
}

func TestLink_ReusesExistingHardlink(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "import")
	source := writeSource(t, filepath.Join(dir, "library"), "one.mkv")
	placed := New(newPackInfo(t), PackFolderDestination(root))
	links := []PlannedLink{{Source: source, TorrentPath: episode1}}
	target := filepath.Join(root, packName, episode1)
	want := []Outcome{{Status: Linked, Target: target}}

	require.Equal(t, want, placed.Link(links))
	require.Equal(t, want, placed.Link(links))

	requireSameFile(t, source, target)
}

func TestLink_KeepsOtherFileAtHardlinkTarget(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "import")
	source := writeSource(t, filepath.Join(dir, "library"), "one.mkv")
	target := filepath.Join(root, packName, episode1)
	require.NoError(t, os.MkdirAll(filepath.Dir(target), 0o755))
	require.NoError(t, os.WriteFile(target, []byte("different"), 0o644))

	outcomes := New(newPackInfo(t), PackFolderDestination(root)).Link([]PlannedLink{{Source: source, TorrentPath: episode1}})

	require.Len(t, outcomes, 1)
	assert.Equal(t, Failed, outcomes[0].Status)
	assert.ErrorIs(t, outcomes[0].Err, fs.ErrExist)
	assert.Empty(t, outcomes[0].Target)
	content, err := os.ReadFile(target)
	require.NoError(t, err)
	assert.Equal(t, "different", string(content))
}

// TestLink_StaysBelowImportRoot: a prepared folder below the import root must
// not redirect a hardlink to another place on disk (issue #290).
func TestLink_StaysBelowImportRoot(t *testing.T) {
	newDirs := func(t *testing.T) (root, outside, source string) {
		t.Helper()
		dir := t.TempDir()
		root = filepath.Join(dir, "import")
		outside = filepath.Join(dir, "outside")
		require.NoError(t, os.Mkdir(root, 0o755))
		require.NoError(t, os.Mkdir(outside, 0o755))
		return root, outside, writeSource(t, filepath.Join(dir, "library"), "one.mkv")
	}
	links := func(source string) []PlannedLink {
		return []PlannedLink{{Source: source, TorrentPath: episode1}}
	}

	t.Run("symlinked pack folder that leaves the root", func(t *testing.T) {
		root, outside, source := newDirs(t)
		require.NoError(t, os.Symlink(outside, filepath.Join(root, packName)))

		outcomes := New(newPackInfo(t), PackFolderDestination(root)).Link(links(source))

		require.Len(t, outcomes, 1)
		assert.Equal(t, Failed, outcomes[0].Status)
		assert.Error(t, outcomes[0].Err)
		assert.NoFileExists(t, filepath.Join(outside, episode1))
	})

	t.Run("symlinked subfolder that leaves the root", func(t *testing.T) {
		root, outside, source := newDirs(t)
		require.NoError(t, os.Mkdir(filepath.Join(root, packName), 0o755))
		require.NoError(t, os.Symlink(outside, filepath.Join(root, packName, "Extras")))
		info := newInfo(t, metainfo.Info{
			Name:  packName,
			Files: []metainfo.FileInfo{{Path: []string{"Extras", episode1}, Length: 7}},
		})

		outcomes := New(info, PackFolderDestination(root)).Link([]PlannedLink{{Source: source, TorrentPath: "Extras/" + episode1}})

		require.Len(t, outcomes, 1)
		assert.Equal(t, Failed, outcomes[0].Status)
		assert.Error(t, outcomes[0].Err)
		assert.NoFileExists(t, filepath.Join(outside, episode1))
	})

	t.Run("relative symlink that stays below the root", func(t *testing.T) {
		root, _, source := newDirs(t)
		require.NoError(t, os.Mkdir(filepath.Join(root, "real"), 0o755))
		require.NoError(t, os.Symlink("real", filepath.Join(root, packName)))

		outcomes := New(newPackInfo(t), PackFolderDestination(root)).Link(links(source))

		require.Equal(t, []Outcome{{Status: Linked, Target: filepath.Join(root, packName, episode1)}}, outcomes)
		requireSameFile(t, source, filepath.Join(root, "real", episode1))
	})

	t.Run("import root that is a symlink", func(t *testing.T) {
		realRoot, _, source := newDirs(t)
		root := filepath.Join(filepath.Dir(realRoot), "link")
		require.NoError(t, os.Symlink(realRoot, root))

		outcomes := New(newPackInfo(t), PackFolderDestination(root)).Link(links(source))

		require.Equal(t, []Outcome{{Status: Linked, Target: filepath.Join(root, packName, episode1)}}, outcomes)
		requireSameFile(t, source, filepath.Join(realRoot, packName, episode1))
	})
}

func TestLink_ReportsMissingSourceAndLinksTheOthers(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "import")
	missing := filepath.Join(dir, "library", "gone.mkv")
	source := writeSource(t, filepath.Join(dir, "library"), "two.mkv")

	outcomes := New(newPackInfo(t), PackFolderDestination(root)).Link([]PlannedLink{
		{Source: missing, TorrentPath: episode1},
		{Source: source, TorrentPath: episode2},
	})

	require.Len(t, outcomes, 2)
	assert.Equal(t, SourceMissing, outcomes[0].Status)
	assert.Empty(t, outcomes[0].Target)
	// The error of the link call keeps its identity for the operator log.
	assert.ErrorIs(t, outcomes[0].Err, fs.ErrNotExist)
	_, ok := errors.AsType[*os.LinkError](outcomes[0].Err)
	assert.True(t, ok)
	assert.NoFileExists(t, filepath.Join(root, packName, episode1))

	target := filepath.Join(root, packName, episode2)
	assert.Equal(t, Outcome{Status: Linked, Target: target}, outcomes[1])
	requireSameFile(t, source, target)
}

// TestLink_RefusesPathThatIsNoFileOfTorrent: each hardlink target is a file of
// the validated torrent, also when a caller gives another path (issue #290).
func TestLink_RefusesPathThatIsNoFileOfTorrent(t *testing.T) {
	tests := map[string]string{
		"other file name":                           "Show.S01E03.1080p.WEB-DL.H.264-RlsGrp.mkv",
		"parent elements":                           "../../outside/" + episode1,
		"new folder below the parent of the root":   "../../created/" + episode1,
		"file of the torrent behind parent element": "Extras/../" + episode1,
		"absolute path":                             "/" + episode1,
		"empty path":                                "",
	}
	for name, torrentPath := range tests {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			root := filepath.Join(dir, "import")
			outside := filepath.Join(dir, "outside")
			require.NoError(t, os.Mkdir(outside, 0o755))
			source := writeSource(t, filepath.Join(dir, "library"), "one.mkv")

			outcomes := New(newPackInfo(t), PackFolderDestination(root)).Link([]PlannedLink{{Source: source, TorrentPath: torrentPath}})

			require.Len(t, outcomes, 1)
			assert.Equal(t, Failed, outcomes[0].Status)
			assert.ErrorContains(t, outcomes[0].Err, "is not a file of torrent")
			assert.Empty(t, outcomes[0].Target)
			assert.NoFileExists(t, filepath.Join(outside, episode1))
			assert.NoDirExists(t, filepath.Join(dir, "created"))
			assert.NoDirExists(t, filepath.Join(root, packName))
		})
	}
}

func TestPlacement_RejectsSingleFileTorrent(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "import")
	source := writeSource(t, filepath.Join(dir, "library"), "one.mkv")
	info := newInfo(t, metainfo.Info{Name: episode1, Length: 7})

	for name, destination := range map[string]Destination{
		"pack folder": PackFolderDestination(root),
		"flat":        FlatDestination(root),
	} {
		t.Run(name, func(t *testing.T) {
			placed := New(info, destination)

			outcomes := placed.Link([]PlannedLink{{Source: source, TorrentPath: episode1}})

			require.Len(t, outcomes, 1)
			assert.Equal(t, Failed, outcomes[0].Status)
			assert.ErrorContains(t, outcomes[0].Err, "not a directory torrent")
			assert.NoDirExists(t, root)

			// The one file is at the place where a client puts it, with its size.
			require.NoError(t, os.MkdirAll(root, 0o755))
			require.NoError(t, os.Link(source, filepath.Join(root, episode1)))
			assert.False(t, placed.Complete())
			require.NoError(t, os.RemoveAll(root))
		})
	}
}

func TestLink_GivesEachLinkTheErrorOfImportRoot(t *testing.T) {
	dir := t.TempDir()
	// A file where the parent folder of the import root must be.
	blocker := filepath.Join(dir, "blocker")
	require.NoError(t, os.WriteFile(blocker, nil, 0o644))
	source := writeSource(t, filepath.Join(dir, "library"), "one.mkv")
	missing := filepath.Join(dir, "library", "gone.mkv")

	outcomes := New(newPackInfo(t), PackFolderDestination(filepath.Join(blocker, "import"))).Link([]PlannedLink{
		{Source: source, TorrentPath: episode1},
		{Source: missing, TorrentPath: episode2},
	})

	require.Len(t, outcomes, 2)
	require.Error(t, outcomes[0].Err)
	assert.Equal(t, Outcome{Err: outcomes[0].Err}, outcomes[0])
	assert.Equal(t, outcomes[0], outcomes[1], "a failure of the import root is no fact about one source")
}

func TestComplete(t *testing.T) {
	// newPack writes both episodes of the pack with their size of 7 bytes.
	newPack := func(t *testing.T) (root, packDir string) {
		t.Helper()
		root = filepath.Join(t.TempDir(), "import")
		packDir = filepath.Join(root, packName)
		writeSource(t, packDir, episode1)
		writeSource(t, packDir, episode2)
		return root, packDir
	}

	t.Run("all files present", func(t *testing.T) {
		root, _ := newPack(t)

		assert.True(t, New(newPackInfo(t), PackFolderDestination(root)).Complete())
	})

	t.Run("all files present in flat layout", func(t *testing.T) {
		_, packDir := newPack(t)

		assert.True(t, New(newPackInfo(t), FlatDestination(packDir)).Complete())
		assert.False(t, New(newPackInfo(t), PackFolderDestination(packDir)).Complete())
	})

	t.Run("one file missing", func(t *testing.T) {
		root, packDir := newPack(t)
		require.NoError(t, os.Remove(filepath.Join(packDir, episode2)))

		assert.False(t, New(newPackInfo(t), PackFolderDestination(root)).Complete())
	})

	t.Run("one file with another size", func(t *testing.T) {
		root, packDir := newPack(t)
		require.NoError(t, os.WriteFile(filepath.Join(packDir, episode2), []byte("short"), 0o644))

		assert.False(t, New(newPackInfo(t), PackFolderDestination(root)).Complete())
	})

	t.Run("padding file that is not on disk", func(t *testing.T) {
		root, _ := newPack(t)
		info := newInfo(t, metainfo.Info{
			Name: packName,
			Files: []metainfo.FileInfo{
				{Path: []string{episode1}, Length: 7},
				{Path: []string{".pad", "1017"}, Length: 1017, Attr: "p"},
				{Path: []string{episode2}, Length: 7},
			},
		})

		assert.True(t, New(info, PackFolderDestination(root)).Complete())
	})

	t.Run("hardlink target that is not a regular file", func(t *testing.T) {
		root, packDir := newPack(t)
		require.NoError(t, os.Remove(filepath.Join(packDir, episode2)))
		require.NoError(t, os.Mkdir(filepath.Join(packDir, episode2), 0o755))
		// The torrent expects the size that the folder has, so only its file type is wrong.
		stat, err := os.Stat(filepath.Join(packDir, episode2))
		require.NoError(t, err)
		info := newInfo(t, metainfo.Info{
			Name: packName,
			Files: []metainfo.FileInfo{
				{Path: []string{episode1}, Length: 7},
				{Path: []string{episode2}, Length: stat.Size()},
			},
		})

		assert.False(t, New(info, PackFolderDestination(root)).Complete())
	})

	t.Run("import root that is not on disk", func(t *testing.T) {
		root := filepath.Join(t.TempDir(), "import")

		assert.False(t, New(newPackInfo(t), PackFolderDestination(root)).Complete())
		assert.NoDirExists(t, root, "the completeness check must change nothing on disk")
	})
}

func TestLink_MakesNothingWithoutLinks(t *testing.T) {
	root := filepath.Join(t.TempDir(), "import")

	outcomes := New(newPackInfo(t), PackFolderDestination(root)).Link(nil)

	assert.Empty(t, outcomes)
	assert.NoDirExists(t, root)
}
