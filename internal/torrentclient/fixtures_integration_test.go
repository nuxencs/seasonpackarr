// Copyright (c) 2023 - 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

//go:build integration

package torrentclient

import (
	"bytes"
	"context"
	"crypto/sha1"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nuxencs/seasonpackarr/internal/torrents"

	"github.com/autobrr/go-torrent/bencode"
	"github.com/autobrr/go-torrent/metainfo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Integration tests run against real daemons. The import folder must be the
// same path for this process and for the daemon.
const (
	envImportDir = "SEASONPACKARR_TEST_IMPORT_DIR"

	envQbitHost = "SEASONPACKARR_TEST_QBIT_HOST"
	envQbitUser = "SEASONPACKARR_TEST_QBIT_USER"
	envQbitPass = "SEASONPACKARR_TEST_QBIT_PASS"

	envTransmissionHost = "SEASONPACKARR_TEST_TRANSMISSION_HOST"
	envTransmissionUser = "SEASONPACKARR_TEST_TRANSMISSION_USER"
	envTransmissionPass = "SEASONPACKARR_TEST_TRANSMISSION_PASS"

	envDelugeType = "SEASONPACKARR_TEST_DELUGE_TYPE"
	envDelugeHost = "SEASONPACKARR_TEST_DELUGE_HOST"
	envDelugePort = "SEASONPACKARR_TEST_DELUGE_PORT"
	envDelugeUser = "SEASONPACKARR_TEST_DELUGE_USER"
	envDelugePass = "SEASONPACKARR_TEST_DELUGE_PASS"
)

const (
	packEpisodes    = 3
	packPieceLength = 256 * 1024
	// Multi-piece episodes make a missing file read as incomplete instead of
	// sharing one tiny piece with a present file.
	packEpisodeSize = 1 << 20
	daemonTimeout   = 30 * time.Second
)

// requireDaemon skips the test unless the import folder and the daemon address
// variable are set. It returns the import folder.
func requireDaemon(t *testing.T, hostKey string) string {
	t.Helper()
	var missing []string
	for _, key := range []string{hostKey, envImportDir} {
		if os.Getenv(key) == "" {
			missing = append(missing, key)
		}
	}
	if len(missing) > 0 {
		t.Skipf("set %s to run this integration test", strings.Join(missing, " and "))
	}
	return os.Getenv(envImportDir)
}

// cleanupContext is for t.Cleanup functions: t.Context is canceled before they run.
func cleanupContext(t *testing.T) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(t.Context()), daemonTimeout)
}

// testPack is a season pack on disk in the import folder plus its .torrent.
type testPack struct {
	name    string
	torrent []byte
	hashes  torrents.Hashes
}

func (p testPack) importRequest(savePath string, dataComplete bool) ImportRequest {
	return ImportRequest{
		TorrentBytes: p.torrent,
		LegacyHash:   p.hashes.Legacy,
		V2Hash:       p.hashes.V2,
		HasV1:        p.hashes.HasV1,
		SavePath:     savePath,
		DataComplete: dataComplete,
	}
}

// packName names a pack after the test, so tests do not share pack folders in
// one import folder. parts are added for runs of one test against different
// daemons, such as the Deluge client type.
func packName(t *testing.T, parts ...string) string {
	name := strings.ReplaceAll(strings.TrimPrefix(t.Name(), "Test"), "/", ".")
	return strings.Join(append([]string{name}, parts...), ".") + ".S01.1080p.WEB-DL.H.264-RlsGrp"
}

// writeCompletePack writes every episode of the pack name.
func writeCompletePack(t *testing.T, importDir, name string) testPack {
	t.Helper()
	return writePack(t, importDir, name, 0)
}

// writePartialPack builds the .torrent for every episode, then keeps only the
// 1-based episode keep on disk, like an import that reuses one episode.
func writePartialPack(t *testing.T, importDir, name string, keep int) testPack {
	t.Helper()
	require.True(t, keep >= 1 && keep <= packEpisodes, "keep=%d", keep)
	return writePack(t, importDir, name, keep)
}

// writePack keeps every episode when keep is 0.
func writePack(t *testing.T, importDir, name string, keep int) testPack {
	t.Helper()

	packDir := filepath.Join(importDir, name)
	require.NoError(t, os.MkdirAll(packDir, 0o755))

	content := make([]byte, packEpisodeSize)
	episodes := make([]string, 0, packEpisodes)
	for episode := 1; episode <= packEpisodes; episode++ {
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
	return testPack{name: name, torrent: torrent, hashes: hashes}
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

// assertRemoved waits until present reports that the daemon no longer holds
// hash. Cleanups use it so a test cannot leave a torrent behind for the next run.
func assertRemoved(t *testing.T, ctx context.Context, hash string, present func() (bool, error)) {
	t.Helper()
	var err error
	_, removed := waitFor(ctx, func() bool {
		var found bool
		found, err = present()
		return found
	}, func(found bool) bool { return err != nil || !found })
	if assert.NoError(t, err, "check removal of torrent %s", hash) {
		assert.True(t, removed, "daemon still holds the torrent %s after removal", hash)
	}
}

// waitFor calls read until done reports true, ctx ends, or daemonTimeout ends.
// It returns the last value and whether done reported true, so the caller can
// assert on the final daemon state.
func waitFor[T any](ctx context.Context, read func() T, done func(T) bool) (T, bool) {
	ctx, cancel := context.WithTimeout(ctx, daemonTimeout)
	defer cancel()
	for {
		value := read()
		if done(value) {
			return value, true
		}
		select {
		case <-ctx.Done():
			return value, false
		case <-time.After(100 * time.Millisecond):
		}
	}
}

// assertPartialProgress asserts that the client checked a partial pack to the
// share of present episodes. progress is a fraction from 0 to 1. Episodes fill
// whole pieces, so the expected value is exact up to rounding.
func assertPartialProgress(t *testing.T, progress float64, present int) {
	t.Helper()
	want := float64(present) / packEpisodes
	assert.InDelta(t, want, progress, 0.01, "the check must find exactly the %d present of %d episodes", present, packEpisodes)
}

// requireListedPack asserts that the adapter's read path reports the imported
// pack with its save path and every file.
func requireListedPack(t *testing.T, client TorrentClient, pack testPack, savePath string) {
	t.Helper()

	listed, err := client.GetTorrents(t.Context())
	require.NoError(t, err)
	var found *Torrent
	for index := range listed {
		if strings.EqualFold(listed[index].Hash, pack.hashes.Legacy) {
			found = &listed[index]
		}
	}
	require.NotNil(t, found, "GetTorrents does not list %s", pack.hashes.Legacy)
	require.Equal(t, normalizePath(savePath), normalizePath(found.SavePath))

	results := client.GetFiles(t.Context(), []string{pack.hashes.Legacy})
	require.Len(t, results, 1)
	require.NoError(t, results[0].Err)
	require.Len(t, results[0].Files, packEpisodes)
	for _, file := range results[0].Files {
		require.True(t, strings.HasPrefix(file.Name, pack.name+"/"), "file %q is outside the pack folder", file.Name)
	}
}

// requireFileReadLoad sends the adapter's multi-hash read shape to a real
// daemon. One repeated hash isolates read batching and concurrency from
// torrent setup cost. The unknown hash checks partial-result handling.
func requireFileReadLoad(t *testing.T, client TorrentClient, hash string) {
	t.Helper()

	const readCount = 24
	hashes := make([]string, readCount+1)
	for index := range readCount {
		hashes[index] = hash
	}
	hashes[readCount] = strings.Repeat("0", 40)

	started := time.Now()
	results := client.GetFiles(t.Context(), hashes)
	duration := time.Since(started)
	require.Len(t, results, len(hashes))
	for index := range readCount {
		require.Equal(t, hash, results[index].Hash, "result %d", index)
		require.NoError(t, results[index].Err, "result %d", index)
		require.NotEmpty(t, results[index].Files, "result %d", index)
	}
	require.Error(t, results[readCount].Err, "unknown hash must report an error")
	t.Logf("GetFiles returned %d file lists and one unknown-hash error in %s", readCount, duration)
}
