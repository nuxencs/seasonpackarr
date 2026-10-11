// Copyright (c) 2023 - 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

//go:build e2e

package e2e

import (
	"bytes"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"github.com/nuxencs/seasonpackarr/internal/daemontest"

	"github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	envQbitHost = "SEASONPACKARR_TEST_QBIT_HOST"
	envQbitUser = "SEASONPACKARR_TEST_QBIT_USER"
	envQbitPass = "SEASONPACKARR_TEST_QBIT_PASS"
	// qbitClientName is the client name in test/harness/seasonpackarr/config.yaml.
	qbitClientName = "qbit"
)

// TestQbitE2E_DownloadsMissingEpisodes runs the user flow for a partial pack:
// qBittorrent has two of the three episodes as their own torrents, and one
// announce must end in a seeding pack that reuses them and downloads only the
// missing episode from the seeder.
func TestQbitE2E_DownloadsMissingEpisodes(t *testing.T) {
	h := requireHarness(t)
	daemontest.RequireSettings(t, envQbitHost)
	q := newQbit(t)
	s := daemontest.NewSeeder(t)
	h.requireFilter(t, qbitClientName)

	const missing = daemontest.PackEpisodes
	name := packName()
	pack := s.Seed(t, name)
	sourceDir := daemontest.SourceDir(h.importDir)
	present := map[int]daemontest.Episode{}
	for episode := 1; episode < missing; episode++ {
		present[episode] = q.addComplete(t, sourceDir, name, episode)
	}
	hash := pack.Hashes.Legacy
	q.removeAtCleanup(t, hash)

	h.announce(t, name, pack.Torrent)

	rls := h.autobrr.waitReleaseDone(t, name)
	assert.Equal(t, "FILTER_APPROVED", rls.FilterStatus, "autobrr filter status")
	for _, status := range rls.ActionStatus {
		assert.Equal(t, "PUSH_APPROVED", status.Status, "autobrr action %s: %v", status.Action, status.Rejections)
	}

	// autobrr ignores the import response, so qBittorrent is the truth. The
	// seeder connects only after the check: libtorrent backs off from a peer
	// that it refused while the torrent was stopped or checking.
	read := func() qbittorrent.Torrent {
		tor, _ := q.find(t, hash)
		return tor
	}
	tor, downloading := daemontest.WaitFor(t.Context(), read, func(tor qbittorrent.Torrent) bool {
		return tor.State == qbittorrent.TorrentStateDownloading || tor.State == qbittorrent.TorrentStateStalledDl
	})
	require.True(t, downloading, "qBittorrent does not download the pack %s after its check: state=%q progress=%.2f",
		name, tor.State, tor.Progress)

	peer := daemontest.PeerAddress(t, q.host, q.listenPort(t))
	tor, seeding := daemontest.WaitSeededDownload(t, s, hash, peer, read, func(tor qbittorrent.Torrent) bool {
		// libtorrent adds payload to the downloaded count once per second,
		// so the count can lag behind the progress.
		return isSeeding(tor) && tor.Downloaded >= daemontest.PackEpisodeSize
	})
	require.True(t, seeding, "pack is not complete and seeding: state=%s progress=%.2f", tor.State, tor.Progress)

	packDir := filepath.Join(h.importDir, name)
	for index, file := range pack.Episodes {
		imported := filepath.Join(packDir, file)
		if episode, ok := present[index+1]; ok {
			assertSameFile(t, filepath.Join(sourceDir, episode.Name, episode.File), imported)
			continue
		}
		assertSameBytes(t, filepath.Join(s.Dir(), name, file), imported)
	}
	// Episodes fill whole pieces, so the missing episode is exactly its size.
	assert.Equal(t, int64(daemontest.PackEpisodeSize), tor.Downloaded, "only the missing episode may download")
}

// qbit controls the client under test through go-qbittorrent directly, not
// through the seasonpackarr adapter.
type qbit struct {
	api  *qbittorrent.Client
	host string
}

func newQbit(t *testing.T) *qbit {
	t.Helper()
	host, err := url.Parse(os.Getenv(envQbitHost))
	require.NoError(t, err)
	api := qbittorrent.NewClient(qbittorrent.Config{
		Host:     host.String(),
		Username: os.Getenv(envQbitUser),
		Password: os.Getenv(envQbitPass),
	})
	require.NoError(t, api.LoginCtx(t.Context()), "log in to qBittorrent")
	return &qbit{api: api, host: host.Hostname()}
}

// addComplete writes the 1-based episode of pack name into dir as its own
// release, adds it to qBittorrent, and waits until qBittorrent checked it and
// seeds it. seasonpackarr finds it through the client inventory.
func (q *qbit) addComplete(t *testing.T, dir, name string, episode int) daemontest.Episode {
	t.Helper()
	ep := daemontest.WriteEpisode(t, dir, name, episode)
	q.removeAtCleanup(t, ep.Hashes.Legacy)
	options := (&qbittorrent.TorrentAddOptions{SavePath: dir}).Prepare()
	_, err := q.api.AddTorrentFromMemoryCtx(t.Context(), ep.Torrent, options)
	require.NoError(t, err, "add episode %s to qBittorrent", ep.Name)

	tor, seeding := daemontest.WaitFor(t.Context(), func() qbittorrent.Torrent {
		tor, _ := q.find(t, ep.Hashes.Legacy)
		return tor
	}, isSeeding)
	require.True(t, seeding, "qBittorrent does not seed episode %s: state=%s progress=%.2f", ep.Name, tor.State, tor.Progress)
	return ep
}

// removeAtCleanup removes the torrent hash, not its data, when the test ends,
// so a rerun starts without it.
func (q *qbit) removeAtCleanup(t *testing.T, hash string) {
	t.Helper()
	t.Cleanup(func() {
		ctx, cancel := daemontest.CleanupContext(t)
		defer cancel()
		assert.NoError(t, q.api.DeleteTorrentsCtx(ctx, []string{hash}, false), "remove torrent %s from qBittorrent", hash)
		daemontest.AssertRemoved(t, hash, func() (bool, error) {
			found, err := q.api.GetTorrentsCtx(ctx, qbittorrent.TorrentFilterOptions{Hashes: []string{hash}})
			return len(found) > 0, err
		})
	})
}

func (q *qbit) find(t *testing.T, hash string) (qbittorrent.Torrent, bool) {
	t.Helper()
	found, err := q.api.GetTorrentsCtx(t.Context(), qbittorrent.TorrentFilterOptions{Hashes: []string{hash}})
	require.NoError(t, err, "read torrent %s from qBittorrent", hash)
	if len(found) == 0 {
		return qbittorrent.Torrent{}, false
	}
	return found[0], true
}

func (q *qbit) listenPort(t *testing.T) int {
	t.Helper()
	prefs, err := q.api.GetAppPreferencesCtx(t.Context())
	require.NoError(t, err, "read qBittorrent preferences")
	return prefs.ListenPort
}

func isSeeding(tor qbittorrent.Torrent) bool {
	return tor.Progress >= 1 &&
		(tor.State == qbittorrent.TorrentStateUploading || tor.State == qbittorrent.TorrentStateStalledUp)
}

// assertSameFile checks that the pack reuses the source file as a hardlink.
func assertSameFile(t *testing.T, source, imported string) {
	t.Helper()
	sourceInfo, err := os.Stat(source)
	require.NoError(t, err, "source episode")
	importedInfo, err := os.Stat(imported)
	if assert.NoError(t, err, "reused episode is not in the pack") {
		assert.True(t, os.SameFile(sourceInfo, importedInfo), "reused episode %s must keep the inode of its source", imported)
	}
}

// assertSameBytes checks that the downloaded episode has the seeder's bytes.
func assertSameBytes(t *testing.T, seeded, downloaded string) {
	t.Helper()
	want, err := os.ReadFile(seeded)
	require.NoError(t, err, "seeded episode")
	got, err := os.ReadFile(downloaded)
	if assert.NoError(t, err, "downloaded episode is not in the pack") {
		// bytes.Equal, because a diff of two 1 MiB files is unreadable.
		assert.True(t, bytes.Equal(want, got), "downloaded episode %s does not match the seeder", downloaded)
	}
}
