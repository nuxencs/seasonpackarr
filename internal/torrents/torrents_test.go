// Copyright (c) 2023 - 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package torrents

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/autobrr/go-torrent/bencode"
	"github.com/autobrr/go-torrent/metainfo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInfoHashes_UsesV2IdentityForPureV2Torrent(t *testing.T) {
	infoBytes, err := bencode.Marshal(metainfo.Info{
		Name:        "PureV2.S01",
		PieceLength: 16 * 1024,
		MetaVersion: 2,
	})
	require.NoError(t, err)

	var torrentBytes bytes.Buffer
	require.NoError(t, (&metainfo.MetaInfo{InfoBytes: infoBytes}).Write(&torrentBytes))

	hashes, err := InfoHashes(torrentBytes.Bytes())
	require.NoError(t, err)
	require.False(t, hashes.HasV1)
	require.Equal(t, metainfo.HashBytes(infoBytes).HexString(), hashes.Legacy)
	require.Equal(t, metainfo.HashV2Bytes(infoBytes).HexString(), hashes.V2)
}

func TestInfoHashes_MarksHybridTorrentAsV1Capable(t *testing.T) {
	infoBytes, err := bencode.Marshal(metainfo.Info{
		Name:        "Hybrid.S01",
		PieceLength: 16 * 1024,
		MetaVersion: 2,
		Pieces:      make([]byte, 20),
	})
	require.NoError(t, err)

	var torrentBytes bytes.Buffer
	require.NoError(t, (&metainfo.MetaInfo{InfoBytes: infoBytes}).Write(&torrentBytes))

	hashes, err := InfoHashes(torrentBytes.Bytes())
	require.NoError(t, err)
	require.True(t, hashes.HasV1)
	require.NotEmpty(t, hashes.Legacy)
	require.NotEmpty(t, hashes.V2)
}

// mustTorrentWithFiles builds a torrent for a release named name. Without any
// paths it describes a single file, otherwise a directory holding those paths.
func mustTorrentWithFiles(t *testing.T, name string, paths ...string) []byte {
	t.Helper()

	root := filepath.Join(t.TempDir(), name)

	if len(paths) == 0 {
		require.NoError(t, os.WriteFile(root, []byte("0"), 0o644))
	} else {
		require.NoError(t, os.Mkdir(root, 0o755))

		for _, path := range paths {
			full := filepath.Join(root, filepath.FromSlash(path))
			require.NoError(t, os.MkdirAll(filepath.Dir(full), 0o755))
			require.NoError(t, os.WriteFile(full, []byte("0"), 0o644))
		}
	}

	torrentBytes, err := torrentFromFolder(root)
	require.NoError(t, err)

	return torrentBytes
}

func TestTorrentFromRls_RoundTrip(t *testing.T) {
	const rlsName = "Series.S01.1080p.WEB-DL.DDP5.1.H.264-RlsGrp"

	torrentBytes, err := TorrentFromRls(rlsName, 3)
	require.NoError(t, err)

	info, err := Info(torrentBytes)
	require.NoError(t, err)

	assert.Equal(t, rlsName, info.Name)
	assert.True(t, info.IsDir())

	episodes, err := Episodes(info)
	require.NoError(t, err)

	assert.Equal(t, []Episode{
		{Path: "Series.S01E01.1080p.WEB-DL.DDP5.1.H.264-RlsGrp.mkv", Size: 1},
		{Path: "Series.S01E02.1080p.WEB-DL.DDP5.1.H.264-RlsGrp.mkv", Size: 1},
		{Path: "Series.S01E03.1080p.WEB-DL.DDP5.1.H.264-RlsGrp.mkv", Size: 1},
	}, episodes)
}

func TestTorrentFromRls_RejectsNameWithoutSeason(t *testing.T) {
	_, err := TorrentFromRls("Series.1080p.WEB-DL.DDP5.1.H.264-RlsGrp", 3)
	assert.ErrorContains(t, err, "no season information found in release name")
}

func TestInfo_RejectsInvalidBytes(t *testing.T) {
	_, err := Info([]byte("not a torrent"))
	assert.Error(t, err)
}

// mustTorrentFromInfo builds a torrent from metadata that no folder on disk can
// have, such as a path with a parent element.
func mustTorrentFromInfo(t *testing.T, info metainfo.Info) []byte {
	t.Helper()

	info.PieceLength = 256 * 1024
	infoBytes, err := bencode.Marshal(info)
	require.NoError(t, err)
	var torrent bytes.Buffer
	require.NoError(t, (&metainfo.MetaInfo{InfoBytes: infoBytes}).Write(&torrent))

	return torrent.Bytes()
}

// TestInfo_RejectsPathsOutsidePackFolder: the name and the file paths become
// hardlink targets below the import root (issue #290).
func TestInfo_RejectsPathsOutsidePackFolder(t *testing.T) {
	episode := []metainfo.FileInfo{{Path: []string{"Show.S01E01.mkv"}, Length: 1}}
	tests := []struct {
		name string
		info metainfo.Info
		want string
	}{
		{name: "parent name", info: metainfo.Info{Name: "..", Files: episode}, want: "torrent name"},
		{name: "name with parent element", info: metainfo.Info{Name: "../Show.S01", Files: episode}, want: "torrent name"},
		{name: "name with nested folder", info: metainfo.Info{Name: "tv/Show.S01", Files: episode}, want: "torrent name"},
		{name: "absolute name", info: metainfo.Info{Name: "/data/Show.S01", Files: episode}, want: "torrent name"},
		{name: "current folder name", info: metainfo.Info{Name: ".", Files: episode}, want: "torrent name"},
		{name: "empty name", info: metainfo.Info{Files: episode}, want: "torrent name"},
		{name: "single file with parent element", info: metainfo.Info{Name: "../Show.S01E01.mkv", Length: 1}, want: "torrent name"},
		{
			name: "file path with parent elements",
			info: metainfo.Info{Name: "Show.S01", Files: []metainfo.FileInfo{{Path: []string{"..", "..", "Show.S01E01.mkv"}, Length: 1}}},
			want: "torrent file path",
		},
		{
			name: "file path that returns below the folder",
			info: metainfo.Info{Name: "Show.S01", Files: []metainfo.FileInfo{{Path: []string{"..", "Show.S01", "Show.S01E01.mkv"}, Length: 1}}},
			want: "torrent file path",
		},
		{
			name: "absolute file path",
			info: metainfo.Info{Name: "Show.S01", Files: []metainfo.FileInfo{{Path: []string{"/data/Show.S01E01.mkv"}, Length: 1}}},
			want: "torrent file path",
		},
		{
			name: "parent element in one path element",
			info: metainfo.Info{Name: "Show.S01", Files: []metainfo.FileInfo{{Path: []string{"../../Show.S01E01.mkv"}, Length: 1}}},
			want: "torrent file path",
		},
		{
			name: "empty file path",
			info: metainfo.Info{Name: "Show.S01", Files: []metainfo.FileInfo{{Path: []string{""}, Length: 1}}},
			want: "torrent file path",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Info(mustTorrentFromInfo(t, tt.info))
			assert.ErrorContains(t, err, tt.want)
		})
	}
}

func TestInfo_AcceptsPathsBelowPackFolder(t *testing.T) {
	info, err := Info(mustTorrentFromInfo(t, metainfo.Info{
		Name: "Show.S01..1080p",
		Files: []metainfo.FileInfo{
			{Path: []string{"Show.S01E01.mkv"}, Length: 1},
			{Path: []string{"Extras", "..Interview.mkv"}, Length: 1},
			{Path: []string{".pad", "1024"}, Length: 1024, Attr: "p"},
		},
	}))
	require.NoError(t, err)
	assert.Len(t, info.Files, 3)

	_, err = Info(mustTorrentFromInfo(t, metainfo.Info{Name: "Show.S01E01.mkv", Length: 1}))
	assert.NoError(t, err)
}

func TestEpisodes_ReturnsMKVAndMP4Files(t *testing.T) {
	info, err := Info(mustTorrentWithFiles(t, "Series.S01.1080p.WEB-DL.DDP5.1.H.264-RlsGrp",
		"Series.S01E01.mkv", "Series.S01E01.nfo", "Series.S01E02.mp4", "Series.S01E03.MP4"))
	require.NoError(t, err)

	episodes, err := Episodes(info)
	require.NoError(t, err)

	assert.Equal(t, []Episode{
		{Path: "Series.S01E01.mkv", Size: 1},
		{Path: "Series.S01E02.mp4", Size: 1},
		{Path: "Series.S01E03.MP4", Size: 1},
	}, episodes)
}

func TestEpisodes_RejectsTorrentWithoutSupportedVideoFiles(t *testing.T) {
	info, err := Info(mustTorrentWithFiles(t, "Series.S01.1080p.WEB-DL.DDP5.1.H.264-RlsGrp",
		"Series.S01E01.nfo", "Series.S01E02.nfo"))
	require.NoError(t, err)

	_, err = Episodes(info)
	assert.ErrorContains(t, err, "no supported video files found")
}

func TestEpisodes_RejectsSingleFileTorrent(t *testing.T) {
	info, err := Info(mustTorrentWithFiles(t, "Series.S01E01.1080p.WEB-DL.DDP5.1.H.264-RlsGrp.mkv"))
	require.NoError(t, err)

	_, err = Episodes(info)
	assert.ErrorContains(t, err, "not a directory")
}
