// Copyright (c) 2023 - 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

//go:build e2e

package e2e

import (
	"bytes"
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nuxencs/seasonpackarr/internal/daemontest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// client is the client under test. It supplies only its setup and its state
// reads, so the scenarios hold no client-specific code. It controls the
// daemon through its own API library, not through the seasonpackarr adapter.
type client interface {
	// configName is the client name in test/harness/seasonpackarr/config.yaml.
	configName() string
	// add adds the torrent with its data already complete in dir and starts it.
	add(ctx context.Context, torrent []byte, dir string) error
	// remove removes the torrent hash, not its data.
	remove(ctx context.Context, hash string) error
	// state reads the torrent hash. It returns a zero state when the client
	// does not have the torrent.
	state(ctx context.Context, hash string) (torrentState, error)
	// peerAddress is the <ip>:<port> address at which the seeder reaches the client.
	peerAddress(t *testing.T) string
}

// torrentState is the client state of one torrent, as the client reports it.
type torrentState struct {
	present  bool
	progress float64
	// downloaded is the payload byte count that the client downloaded.
	downloaded int64
	// downloading is true when the client checked the torrent and waits for data.
	downloading bool
	// seeding is true when the client has all data and uploads it.
	seeding bool
	// clientState is the state name of the client, for failure messages.
	clientState string
}

// runScenarios runs every scenario against c. The order is fixed: seasonpackarr
// caches the client inventory for 30 seconds and only an import refreshes it.
// The candidate rejection imports nothing, so a scenario after it would get an
// inventory without its own episodes.
func runScenarios(t *testing.T, c client) {
	h := requireAutobrr(t)
	h.ensureFilter(t, c.configName())

	t.Run("partial pack", func(t *testing.T) { partialPack(t, h, c, h.announceImport) })
	t.Run("complete pack", func(t *testing.T) { completePack(t, h, c) })
	t.Run("candidate rejection", func(t *testing.T) { candidateRejection(t, h, c) })
}

// importPack makes seasonpackarr import the pack name with the .torrent bytes
// torrent, and returns when seasonpackarr sent the import to the client.
type importPack func(t *testing.T, name string, torrent []byte)

// partialPack runs the partial pack: the client has two of the three episodes
// as their own torrents, and the import must end in a seeding pack that reuses
// them and downloads only the missing episode from the seeder.
func partialPack(t *testing.T, h *harness, c client, start importPack) {
	s := daemontest.NewSeeder(t)
	const missing = daemontest.PackEpisodes
	name := packName()
	pack := s.Seed(t, name)
	sourceDir := daemontest.SourceDir(h.importDir)
	present := map[int]daemontest.Episode{}
	for episode := 1; episode < missing; episode++ {
		present[episode] = addEpisode(t, c, sourceDir, name, episode)
	}
	hash := pack.Hashes.Legacy
	removeAtCleanup(t, c, hash)

	start(t, name, pack.Torrent)

	// The client is the truth. The seeder connects only after the check:
	// libtorrent backs off from a peer that it refused while the torrent was
	// stopped or checking.
	read := func() torrentState { return readState(t, c, hash) }
	st, downloading := daemontest.WaitFor(t.Context(), read, func(st torrentState) bool { return st.downloading })
	require.True(t, downloading, "client does not download the pack %s after its check: %s", name, st)

	st, seeding := daemontest.WaitSeededDownload(t, s, hash, c.peerAddress(t), read, func(st torrentState) bool {
		// libtorrent adds payload to the downloaded count once per second,
		// so the count can lag behind the progress.
		return st.seeding && st.downloaded >= daemontest.PackEpisodeSize
	})
	require.True(t, seeding, "pack is not complete and seeding: %s", st)

	packDir := filepath.Join(h.importDir, name)
	for index, file := range pack.Episodes {
		imported := filepath.Join(packDir, file)
		if episode, ok := present[index+1]; ok {
			assertHardlinked(t, filepath.Join(sourceDir, episode.Name, episode.File), imported)
			continue
		}
		assertSameBytes(t, filepath.Join(s.Dir(), name, file), imported)
	}
	// Episodes fill whole pieces, so the missing episode is exactly its size.
	assert.Equal(t, int64(daemontest.PackEpisodeSize), st.downloaded, "only the missing episode may download")
}

// completePack runs the complete pack: the client has every episode as its own
// torrent, and one announce must end in a seeding pack that downloads nothing. No seeder has the
// pack, so the pack can seed only from the hardlinked episodes.
func completePack(t *testing.T, h *harness, c client) {
	name := packName()
	// The pack data stays outside /data. Only the hardlinks may fill the pack.
	pack := daemontest.WriteCompletePack(t, t.TempDir(), name)
	sourceDir := daemontest.SourceDir(h.importDir)
	episodes := make([]daemontest.Episode, 0, daemontest.PackEpisodes)
	for episode := 1; episode <= daemontest.PackEpisodes; episode++ {
		episodes = append(episodes, addEpisode(t, c, sourceDir, name, episode))
	}
	hash := pack.Hashes.Legacy
	removeAtCleanup(t, c, hash)

	h.announce(t, name, pack.Torrent)
	h.requireImportSent(t, name)

	st := waitSeeding(t, c, hash, name)

	packDir := filepath.Join(h.importDir, name)
	for index, file := range pack.Episodes {
		episode := episodes[index]
		assertHardlinked(t, filepath.Join(sourceDir, episode.Name, episode.File), filepath.Join(packDir, file))
	}
	assert.Zero(t, st.downloaded, "the pack must not download episode data")
}

// candidateRejection runs the candidate rejection: the client has only an
// episode of the show in another resolution, so the candidate check rejects the
// announce. autobrr must log the
// rejection by the candidate webhook, and nothing may reach the client or /data.
func candidateRejection(t *testing.T, h *harness, c client) {
	name := packName()
	// autobrr never downloads the pack, because the match webhook does not run.
	pack := daemontest.WriteCompletePack(t, t.TempDir(), name)
	// The pack is 1080p, as the filter requires. The episode is 720p.
	addEpisode(t, c, daemontest.SourceDir(h.importDir), strings.Replace(name, ".1080p.", ".720p.", 1), 1)

	// /data is the shared volume of every container (ADR-0001).
	dataDir := filepath.Dir(h.importDir)
	before := listFiles(t, dataDir)

	h.announce(t, name, pack.Torrent)

	// autobrr stores no release for a rejected announce, so its log is the
	// proof that it processed the announce and the candidate webhook rejected it.
	rej := h.autobrr.waitRejection(t, filterConfig(t, c.configName())["name"].(string), name)
	// seasonpackarr answers 200 (no matches) when no client episode is compatible.
	assert.Equal(t, rejection{webhook: "candidate", status: 200}, rej,
		"autobrr must reject the announce at the candidate webhook")

	rls, err := h.autobrr.findRelease(name)
	require.NoError(t, err, "read the autobrr release %s", name)
	assert.Zero(t, rls, "autobrr must not store the rejected release")
	assert.False(t, readState(t, c, pack.Hashes.Legacy).present, "the client must not get the pack")
	assert.Equal(t, before, listFiles(t, dataDir), "/data must not get new files")
}

// addEpisode writes the 1-based episode of pack name into dir as its own
// release, adds it to the client, and waits until the client seeds it.
// seasonpackarr finds it through the client inventory.
func addEpisode(t *testing.T, c client, dir, name string, episode int) daemontest.Episode {
	t.Helper()
	ep := daemontest.WriteEpisode(t, dir, name, episode)
	hash := ep.Hashes.Legacy
	removeAtCleanup(t, c, hash)
	require.NoError(t, c.add(t.Context(), ep.Torrent, dir), "add episode %s to the client", ep.Name)

	waitSeeding(t, c, hash, ep.Name)
	return ep
}

// waitSeeding waits until the client seeds the torrent hash of the release
// name, and returns its state.
func waitSeeding(t *testing.T, c client, hash, name string) torrentState {
	t.Helper()
	st, seeding := daemontest.WaitFor(t.Context(), func() torrentState { return readState(t, c, hash) },
		func(st torrentState) bool { return st.seeding })
	require.True(t, seeding, "client does not seed %s: %s", name, st)
	return st
}

// removeAtCleanup removes the torrent hash, not its data, when the test ends,
// so a rerun starts without it.
func removeAtCleanup(t *testing.T, c client, hash string) {
	t.Helper()
	t.Cleanup(func() {
		ctx, cancel := daemontest.CleanupContext(t)
		defer cancel()
		assert.NoError(t, c.remove(ctx, hash), "remove torrent %s from the client", hash)
		daemontest.AssertRemoved(t, hash, func() (bool, error) {
			st, err := c.state(ctx, hash)
			return st.present, err
		})
	})
}

func readState(t *testing.T, c client, hash string) torrentState {
	t.Helper()
	st, err := c.state(t.Context(), hash)
	require.NoError(t, err, "read torrent %s from the client", hash)
	return st
}

func (st torrentState) String() string {
	if !st.present {
		return "not in the client"
	}
	return fmt.Sprintf("state=%s progress=%.2f downloaded=%d", st.clientState, st.progress, st.downloaded)
}

// listFiles returns the path of every file and folder below dir.
func listFiles(t *testing.T, dir string) []string {
	t.Helper()
	var paths []string
	require.NoError(t, filepath.WalkDir(dir, func(path string, _ fs.DirEntry, err error) error {
		paths = append(paths, path)
		return err
	}), "list %s", dir)
	return paths
}

// assertHardlinked checks that imported is the same file as source, so the
// pack reuses the source episode and the client did not rewrite it.
func assertHardlinked(t *testing.T, source, imported string) {
	t.Helper()
	sourceInfo, err := os.Stat(source)
	if !assert.NoError(t, err, "source episode") {
		return
	}
	importedInfo, err := os.Stat(imported)
	if assert.NoError(t, err, "reused episode is not in the pack") {
		assert.True(t, os.SameFile(sourceInfo, importedInfo), "reused episode %s must keep the inode of its source", imported)
	}
}

// assertSameBytes checks that the downloaded episode has the seeder's bytes.
func assertSameBytes(t *testing.T, seeded, downloaded string) {
	t.Helper()
	want, err := os.ReadFile(seeded)
	if !assert.NoError(t, err, "seeded episode") {
		return
	}
	got, err := os.ReadFile(downloaded)
	if assert.NoError(t, err, "downloaded episode is not in the pack") {
		// bytes.Equal, because a diff of two 1 MiB files is unreadable.
		assert.True(t, bytes.Equal(want, got), "downloaded episode %s does not match the seeder", downloaded)
	}
}
