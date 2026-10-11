// Copyright (c) 2023 - 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package daemontest

import (
	"context"
	"net"
	"net/netip"
	"os"
	"testing"
	"time"

	"github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The seeder is a qBittorrent daemon that seeds full packs to the client under
// test. The seed folder must be on the same volume as the import folder.
const (
	envSeederHost = "SEASONPACKARR_TEST_SEEDER_HOST"
	envSeederUser = "SEASONPACKARR_TEST_SEEDER_USER"
	envSeederPass = "SEASONPACKARR_TEST_SEEDER_PASS"
	envSeedDir    = "SEASONPACKARR_TEST_SEED_DIR"
)

// peerRetryInterval repeats addPeers, so one failed connect attempt of the
// seeder cannot stall a download test.
const peerRetryInterval = 2 * time.Second

// Seeder controls the seeder daemon through go-qbittorrent directly, not
// through the production adapter. The seeder finds no peers by itself (no
// tracker, DHT, PEX or LPD), so a test connects it to the client under test.
type Seeder struct {
	api *qbittorrent.Client
	dir string
}

// NewSeeder logs in to the seeder. Like RequireSettings, it skips the test when
// the seeder settings are missing, and fails it in strict mode.
func NewSeeder(t *testing.T) *Seeder {
	t.Helper()
	RequireSettings(t, envSeederHost, envSeedDir)
	api := qbittorrent.NewClient(qbittorrent.Config{
		Host:     os.Getenv(envSeederHost),
		Username: os.Getenv(envSeederUser),
		Password: os.Getenv(envSeederPass),
	})
	require.NoError(t, api.LoginCtx(t.Context()), "log in to the seeder")
	return &Seeder{api: api, dir: os.Getenv(envSeedDir)}
}

// Dir is the seed folder that holds the complete packs.
func (s *Seeder) Dir() string {
	return s.dir
}

// Seed writes the complete pack name into the seed folder and adds it to the
// seeder, complete and started. It removes the torrent, not its data, at cleanup.
func (s *Seeder) Seed(t *testing.T, name string) Pack {
	t.Helper()
	pack := WriteCompletePack(t, s.dir, name)
	hash := pack.Hashes.Legacy

	lookup := func(ctx context.Context) (qbittorrent.Torrent, bool, error) {
		found, err := s.api.GetTorrentsCtx(ctx, qbittorrent.TorrentFilterOptions{Hashes: []string{hash}})
		if err != nil || len(found) == 0 {
			return qbittorrent.Torrent{}, false, err
		}
		return found[0], true, nil
	}

	t.Cleanup(func() {
		ctx, cancel := CleanupContext(t)
		defer cancel()
		assert.NoError(t, s.api.DeleteTorrentsCtx(ctx, []string{hash}, false), "remove torrent from the seeder")
		AssertRemoved(t, hash, func() (bool, error) {
			_, found, err := lookup(ctx)
			return found, err
		})
	})
	options := (&qbittorrent.TorrentAddOptions{SavePath: s.dir, SkipHashCheck: true}).Prepare()
	_, err := s.api.AddTorrentFromMemoryCtx(t.Context(), pack.Torrent, options)
	require.NoError(t, err, "add torrent to the seeder")

	tor, seeding := WaitFor(t.Context(), func() qbittorrent.Torrent {
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

// PeerAddress returns the <ip>:<port> address of the client under test that
// qBittorrent addPeers needs. host is the client's compose service name.
func PeerAddress(t *testing.T, host string, port int) string {
	t.Helper()
	require.True(t, port > 0 && port <= 65535, "client listen port %d", port)
	addrs, err := net.DefaultResolver.LookupNetIP(t.Context(), "ip4", host)
	require.NoError(t, err, "resolve client host %s", host)
	require.NotEmpty(t, addrs, "client host %s has no IPv4 address", host)
	return netip.AddrPortFrom(addrs[0], uint16(port)).String()
}

// WaitSeededDownload connects the seeder to the client under test at peer, then
// calls read until done reports true or Timeout ends.
func WaitSeededDownload[T any](t *testing.T, s *Seeder, hash, peer string, read func() T, done func(T) bool) (T, bool) {
	t.Helper()
	t.Logf("seeder connects to the client at %s", peer)

	var added time.Time
	return WaitFor(t.Context(), func() T {
		if time.Since(added) >= peerRetryInterval {
			require.NoError(t, s.api.AddPeersForTorrentsCtx(t.Context(), []string{hash}, []string{peer}), "add peer to the seeder")
			added = time.Now()
		}
		return read()
	}, done)
}
