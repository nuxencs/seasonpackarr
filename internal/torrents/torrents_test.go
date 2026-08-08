// Copyright (c) 2023 - 2025, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package torrents

import (
	"bytes"
	"testing"

	"github.com/anacrolix/torrent/bencode"
	"github.com/anacrolix/torrent/metainfo"
	infohash_v2 "github.com/anacrolix/torrent/types/infohash-v2"
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
	v2Hash := infohash_v2.HashBytes(infoBytes)
	require.Equal(t, v2Hash.HexString(), hashes.V2)
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
