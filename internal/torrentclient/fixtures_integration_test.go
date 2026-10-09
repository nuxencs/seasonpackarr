// Copyright (c) 2023 - 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

//go:build integration

package torrentclient

import (
	"bytes"
	"context"
	"crypto/sha1"
	"fmt"
	"net"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nuxencs/seasonpackarr/internal/torrents"

	"github.com/autobrr/go-qbittorrent"
	"github.com/autobrr/go-torrent/bencode"
	"github.com/autobrr/go-torrent/metainfo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Integration tests run against real daemons. The import folder must be the
// same path for this process and for the daemon.
const (
	envImportDir = "SEASONPACKARR_TEST_IMPORT_DIR"
	// envRequireDaemons set to 1 fails tests whose gate settings are missing,
	// so a harness configuration mistake cannot pass as a run of skips.
	envRequireDaemons = "SEASONPACKARR_TEST_REQUIRE_DAEMONS"

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

	// The seeder is a qBittorrent daemon that seeds full packs to the client
	// under test. The seed folder must be on the same volume as the import folder.
	envSeederHost = "SEASONPACKARR_TEST_SEEDER_HOST"
	envSeederUser = "SEASONPACKARR_TEST_SEEDER_USER"
	envSeederPass = "SEASONPACKARR_TEST_SEEDER_PASS"
	envSeedDir    = "SEASONPACKARR_TEST_SEED_DIR"
)

const (
	packEpisodes    = 3
	packPieceLength = 256 * 1024
	// Multi-piece episodes make a missing file read as incomplete instead of
	// sharing one tiny piece with a present file.
	packEpisodeSize = 1 << 20
	daemonTimeout   = 30 * time.Second
	// peerRetryInterval repeats addPeers, so one failed connect attempt of the
	// seeder cannot stall a download test.
	peerRetryInterval = 2 * time.Second
)

// requireDaemon skips the test unless the import folder and the client's gate
// variable are set. In strict mode it fails the test instead. It returns the
// import folder.
func requireDaemon(t *testing.T, gateKey string) string {
	t.Helper()
	requireSettings(t, gateKey, envImportDir)
	return os.Getenv(envImportDir)
}

// requireSettings skips the test unless every key is set. In strict mode it
// fails the test instead.
func requireSettings(t *testing.T, keys ...string) {
	t.Helper()
	var missing []string
	for _, key := range keys {
		if os.Getenv(key) == "" {
			missing = append(missing, key)
		}
	}
	if len(missing) > 0 {
		message := fmt.Sprintf("set %s to run this integration test", strings.Join(missing, " and "))
		if os.Getenv(envRequireDaemons) == "1" {
			t.Fatalf("%s: %s=1 requires every daemon setting", message, envRequireDaemons)
		}
		t.Skip(message)
	}
}

// cleanupContext is for t.Cleanup functions: t.Context is canceled before they run.
func cleanupContext(t *testing.T) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(t.Context()), daemonTimeout)
}

// testPack is a season pack on disk in the import folder plus its .torrent.
type testPack struct {
	name string
	// episodes are the episode file names in pack order.
	episodes []string
	torrent  []byte
	hashes   torrents.Hashes
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
	// A rerun must not write through hardlinks that an earlier run left behind.
	require.NoError(t, os.RemoveAll(packDir))
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
	return testPack{name: name, episodes: episodes, torrent: torrent, hashes: hashes}
}

// sourceDir is the folder of the episodes that an import reuses. It is below
// the import folder, so hardlinks into the import folder stay on one volume.
func sourceDir(importDir string) string {
	return filepath.Join(importDir, "source")
}

// writeLinkedPack writes the complete pack name into sourceDir(importDir).
// Then it hardlinks every episode except the 1-based episode missing into the
// import folder, like an import that reuses all other episodes.
func writeLinkedPack(t *testing.T, importDir, name string, missing int) testPack {
	t.Helper()
	require.True(t, missing >= 1 && missing <= packEpisodes, "missing=%d", missing)

	pack := writeCompletePack(t, sourceDir(importDir), name)
	packDir := filepath.Join(importDir, name)
	require.NoError(t, os.RemoveAll(packDir))
	require.NoError(t, os.MkdirAll(packDir, 0o755))
	for index, file := range pack.episodes {
		if index+1 != missing {
			require.NoError(t, os.Link(filepath.Join(sourceDir(importDir), name, file), filepath.Join(packDir, file)))
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

// assertRemoved waits until present reports that the daemon no longer holds
// hash. Cleanups use it so a test cannot leave a torrent behind for the next run.
func assertRemoved(t *testing.T, hash string, present func() (bool, error)) {
	t.Helper()
	var err error
	// waitFor bounds the wait; t.Context is already canceled in a cleanup.
	_, removed := waitFor(context.WithoutCancel(t.Context()), func() bool {
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

// requireListedPack stops the test unless the adapter's read path reports the
// imported pack with its save path and every file.
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

// seeder controls the seeder daemon through go-qbittorrent directly, not
// through the production adapter. The seeder finds no peers by itself (no
// tracker, DHT, PEX or LPD), so a test connects it to the client under test.
type seeder struct {
	api *qbittorrent.Client
	dir string
}

// newSeeder logs in to the seeder. Like requireDaemon, it skips the test when
// the seeder settings are missing, and fails it in strict mode.
func newSeeder(t *testing.T) *seeder {
	t.Helper()
	requireSettings(t, envSeederHost, envSeedDir)
	api := qbittorrent.NewClient(qbittorrent.Config{
		Host:     os.Getenv(envSeederHost),
		Username: os.Getenv(envSeederUser),
		Password: os.Getenv(envSeederPass),
	})
	require.NoError(t, api.LoginCtx(t.Context()), "log in to the seeder")
	return &seeder{api: api, dir: os.Getenv(envSeedDir)}
}

// seed writes the complete pack name into the seed folder and adds it to the
// seeder, complete and started. It removes the torrent, not its data, at cleanup.
func (s *seeder) seed(t *testing.T, name string) testPack {
	t.Helper()
	pack := writeCompletePack(t, s.dir, name)
	hash := pack.hashes.Legacy

	lookup := func(ctx context.Context) (qbittorrent.Torrent, bool, error) {
		found, err := s.api.GetTorrentsCtx(ctx, qbittorrent.TorrentFilterOptions{Hashes: []string{hash}})
		if err != nil || len(found) == 0 {
			return qbittorrent.Torrent{}, false, err
		}
		return found[0], true, nil
	}

	t.Cleanup(func() {
		ctx, cancel := cleanupContext(t)
		defer cancel()
		assert.NoError(t, s.api.DeleteTorrentsCtx(ctx, []string{hash}, false), "remove torrent from the seeder")
		assertRemoved(t, hash, func() (bool, error) {
			_, found, err := lookup(ctx)
			return found, err
		})
	})
	options := (&qbittorrent.TorrentAddOptions{SavePath: s.dir, SkipHashCheck: true}).Prepare()
	_, err := s.api.AddTorrentFromMemoryCtx(t.Context(), pack.torrent, options)
	require.NoError(t, err, "add torrent to the seeder")

	tor, seeding := waitFor(t.Context(), func() qbittorrent.Torrent {
		tor, _, err := lookup(t.Context())
		require.NoError(t, err)
		return tor
	}, func(tor qbittorrent.Torrent) bool {
		return tor.Progress >= 1 &&
			(tor.State == qbittorrent.TorrentStateUploading || tor.State == qbittorrent.TorrentStateStalledUp)
	})
	require.True(t, seeding, "seeder does not seed the pack: state=%s progress=%.2f", tor.State, tor.Progress)
	return pack
}

// newPeerAddress returns the <ip>:<port> address of the client under test that
// qBittorrent addPeers needs. host is the client's compose service name.
func newPeerAddress(t *testing.T, host string, port int) string {
	t.Helper()
	require.True(t, port > 0 && port <= 65535, "client listen port %d", port)
	addrs, err := net.DefaultResolver.LookupNetIP(t.Context(), "ip4", host)
	require.NoError(t, err, "resolve client host %s", host)
	require.NotEmpty(t, addrs, "client host %s has no IPv4 address", host)
	return netip.AddrPortFrom(addrs[0], uint16(port)).String()
}

// waitSeededDownload connects the seeder to the client under test at peer, then
// calls read until done reports true or daemonTimeout ends.
func waitSeededDownload[T any](t *testing.T, s *seeder, hash, peer string, read func() T, done func(T) bool) (T, bool) {
	t.Helper()
	t.Logf("seeder connects to the client at %s", peer)

	var added time.Time
	return waitFor(t.Context(), func() T {
		if time.Since(added) >= peerRetryInterval {
			require.NoError(t, s.api.AddPeersForTorrentsCtx(t.Context(), []string{hash}, []string{peer}), "add peer to the seeder")
			added = time.Now()
		}
		return read()
	}, done)
}

// assertDownloadedPack asserts the final state of a download test. Every pack
// file is in the import folder with the source content. Reused episodes keep
// the inode of their source file, and the 1-based episode missing is a new
// file. downloaded is the client's payload byte count for the torrent: only the
// missing episode may download.
func assertDownloadedPack(t *testing.T, importDir string, pack testPack, missing int, downloaded int64) {
	t.Helper()
	for index, file := range pack.episodes {
		sourcePath := filepath.Join(sourceDir(importDir), pack.name, file)
		importPath := filepath.Join(importDir, pack.name, file)
		importInfo, err := os.Stat(importPath)
		if !assert.NoError(t, err, "pack file %s is not in the import folder", file) {
			continue
		}
		sourceInfo, err := os.Stat(sourcePath)
		if !assert.NoError(t, err, "source file %s", file) {
			continue
		}

		source, err := os.ReadFile(sourcePath)
		if !assert.NoError(t, err, "source file %s", file) {
			continue
		}
		imported, err := os.ReadFile(importPath)
		if !assert.NoError(t, err, "pack file %s", file) {
			continue
		}
		// bytes.Equal, because a diff of two 1 MiB files is unreadable.
		assert.True(t, bytes.Equal(source, imported), "content of %s does not match the source", file)

		if index+1 == missing {
			assert.False(t, os.SameFile(sourceInfo, importInfo), "downloaded episode %s must be a new file", file)
		} else {
			assert.True(t, os.SameFile(sourceInfo, importInfo), "reused episode %s must keep the inode of its source", file)
		}
	}
	// Episodes fill whole pieces, so the missing episode is exactly its size.
	assert.Equal(t, int64(packEpisodeSize), downloaded, "only the missing episode may download")
}
