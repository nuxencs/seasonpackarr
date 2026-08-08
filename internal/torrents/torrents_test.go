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

func TestInfoHashesUsesV2IdentityForPureV2Torrent(t *testing.T) {
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

func TestInfoHashesMarksHybridTorrentAsV1Capable(t *testing.T) {
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

func Test_TorrentFromRls_RoundTrip(t *testing.T) {
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

func Test_TorrentFromRls_NoSeasonInName(t *testing.T) {
	_, err := TorrentFromRls("Series.1080p.WEB-DL.DDP5.1.H.264-RlsGrp", 3)
	assert.ErrorContains(t, err, "no season information found in release name")
}

func Test_Info_InvalidBytes(t *testing.T) {
	_, err := Info([]byte("not a torrent"))
	assert.Error(t, err)
}

func Test_Episodes_SkipsNonMkvFiles(t *testing.T) {
	info, err := Info(mustTorrentWithFiles(t, "Series.S01.1080p.WEB-DL.DDP5.1.H.264-RlsGrp",
		"Series.S01E01.mkv", "Series.S01E01.nfo", "Sample/sample.mp4", "Series.S01E02.mkv"))
	require.NoError(t, err)

	episodes, err := Episodes(info)
	require.NoError(t, err)

	assert.Equal(t, []Episode{
		{Path: "Series.S01E01.mkv", Size: 1},
		{Path: "Series.S01E02.mkv", Size: 1},
	}, episodes)
}

func Test_Episodes_NoMkvFiles(t *testing.T) {
	info, err := Info(mustTorrentWithFiles(t, "Series.S01.1080p.WEB-DL.DDP5.1.H.264-RlsGrp",
		"Series.S01E01.nfo", "Series.S01E02.nfo"))
	require.NoError(t, err)

	_, err = Episodes(info)
	assert.ErrorContains(t, err, "no .mkv files found")
}

func Test_Episodes_NotADirectory(t *testing.T) {
	info, err := Info(mustTorrentWithFiles(t, "Series.S01E01.1080p.WEB-DL.DDP5.1.H.264-RlsGrp.mkv"))
	require.NoError(t, err)

	_, err = Episodes(info)
	assert.ErrorContains(t, err, "not a directory")
}
