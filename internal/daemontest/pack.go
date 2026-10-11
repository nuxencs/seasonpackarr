// Copyright (c) 2023 - 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package daemontest

import (
	"bytes"
	"crypto/sha1"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nuxencs/seasonpackarr/internal/torrents"

	"github.com/autobrr/go-torrent/bencode"
	"github.com/autobrr/go-torrent/metainfo"
	"github.com/stretchr/testify/require"
)

const (
	// PackEpisodes is the episode count of every test pack.
	PackEpisodes = 3
	// PackEpisodeSize is the size of every episode. Multi-piece episodes make a
	// missing file read as incomplete instead of sharing one tiny piece with a
	// present file.
	PackEpisodeSize = 1 << 20
	packPieceLength = 256 * 1024
)

// Pack is a season pack on disk plus its .torrent.
type Pack struct {
	Name string
	// Episodes are the episode file names in pack order.
	Episodes []string
	Torrent  []byte
	Hashes   torrents.Hashes
}

// PackName names a pack after the test, so tests that write into one import or
// seed folder do not share pack folders. parts are added for runs of one test against different daemons,
// such as the Deluge client type.
func PackName(t *testing.T, parts ...string) string {
	name := strings.ReplaceAll(strings.TrimPrefix(t.Name(), "Test"), "/", ".")
	return strings.Join(append([]string{name}, parts...), ".") + ".S01.1080p.WEB-DL.H.264-RlsGrp"
}

// WriteCompletePack writes every episode of the pack name into dir.
func WriteCompletePack(t *testing.T, dir, name string) Pack {
	t.Helper()
	return writePack(t, dir, name, 0)
}

// WritePartialPack builds the .torrent for every episode, then keeps only the
// 1-based episode keep on disk, like an import that reuses one episode.
func WritePartialPack(t *testing.T, dir, name string, keep int) Pack {
	t.Helper()
	require.True(t, keep >= 1 && keep <= PackEpisodes, "keep=%d", keep)
	return writePack(t, dir, name, keep)
}

// writePack keeps every episode when keep is 0.
func writePack(t *testing.T, dir, name string, keep int) Pack {
	t.Helper()

	packDir := filepath.Join(dir, name)
	// A rerun must not write through hardlinks that an earlier run left behind.
	require.NoError(t, os.RemoveAll(packDir))
	require.NoError(t, os.MkdirAll(packDir, 0o755))

	content := make([]byte, PackEpisodeSize)
	episodes := make([]string, 0, PackEpisodes)
	for episode := 1; episode <= PackEpisodes; episode++ {
		file := strings.Replace(name, ".S01.", fmt.Sprintf(".S01E%02d.", episode), 1) + ".mkv"
		episodes = append(episodes, file)
		content[0] = byte(episode)
		require.NoError(t, os.WriteFile(filepath.Join(packDir, file), content, 0o644))
	}

	torrent := torrentFromDir(t, packDir)
	hashes, err := torrents.InfoHashes(torrent)
	require.NoError(t, err)

	if keep > 0 {
		for index, file := range episodes {
			if index+1 != keep {
				require.NoError(t, os.Remove(filepath.Join(packDir, file)))
			}
		}
	}
	return Pack{Name: name, Episodes: episodes, Torrent: torrent, Hashes: hashes}
}

// SourceDir is the folder of the episodes that an import reuses. It is below
// the import folder, so hardlinks into the import folder stay on one volume.
func SourceDir(importDir string) string {
	return filepath.Join(importDir, "source")
}

// WriteLinkedPack writes the complete pack name into SourceDir(importDir).
// Then it hardlinks every episode except the 1-based episode missing into the
// import folder, like an import that reuses all other episodes.
func WriteLinkedPack(t *testing.T, importDir, name string, missing int) Pack {
	t.Helper()
	require.True(t, missing >= 1 && missing <= PackEpisodes, "missing=%d", missing)

	pack := WriteCompletePack(t, SourceDir(importDir), name)
	packDir := filepath.Join(importDir, name)
	require.NoError(t, os.RemoveAll(packDir))
	require.NoError(t, os.MkdirAll(packDir, 0o755))
	for index, file := range pack.Episodes {
		if index+1 != missing {
			require.NoError(t, os.Link(filepath.Join(SourceDir(importDir), name, file), filepath.Join(packDir, file)))
		}
	}
	return pack
}

// torrentFromDir builds a v1 torrent with piece hashes for the flat folder dir,
// so a real client can check the files on disk.
func torrentFromDir(t *testing.T, dir string) []byte {
	t.Helper()

	info := metainfo.Info{Name: filepath.Base(dir), PieceLength: packPieceLength}
	var content []byte
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		require.NoError(t, err)
		info.Files = append(info.Files, metainfo.FileInfo{Path: []string{entry.Name()}, Length: int64(len(data))})
		content = append(content, data...)
	}
	for offset := 0; offset < len(content); offset += packPieceLength {
		hash := sha1.Sum(content[offset:min(offset+packPieceLength, len(content))])
		info.Pieces = append(info.Pieces, hash[:]...)
	}

	infoBytes, err := bencode.Marshal(info)
	require.NoError(t, err)
	var torrent bytes.Buffer
	require.NoError(t, (&metainfo.MetaInfo{InfoBytes: infoBytes}).Write(&torrent))
	return torrent.Bytes()
}
