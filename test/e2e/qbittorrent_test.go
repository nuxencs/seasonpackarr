// Copyright (c) 2023 - 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

//go:build e2e

package e2e

import (
	"context"
	"net/url"
	"os"
	"testing"

	"github.com/nuxencs/seasonpackarr/internal/daemontest"

	"github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/require"
)

const (
	envQbitHost = "SEASONPACKARR_TEST_QBIT_HOST"
	envQbitUser = "SEASONPACKARR_TEST_QBIT_USER"
	envQbitPass = "SEASONPACKARR_TEST_QBIT_PASS"
	// qbitClientName is the client name in test/harness/seasonpackarr/config.yaml.
	qbitClientName = "qbit"
)

// TestQbitE2E_Scenarios runs the partial-pack, complete-pack, and
// candidate-rejection scenarios against qBittorrent.
func TestQbitE2E_Scenarios(t *testing.T) {
	daemontest.RequireSettings(t, envQbitHost)
	runScenarios(t, newQbit(t))
}

// qbit is the client interface for qBittorrent, through go-qbittorrent.
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
	// The default queue keeps 3 torrents active. The complete pack is the
	// fourth seeding torrent and would stay in queuedUP. Each entry starts a
	// fresh qBittorrent, so the integration entries keep the default.
	require.NoError(t, api.SetPreferencesCtx(t.Context(), map[string]any{"queueing_enabled": false}),
		"turn off the qBittorrent queue")
	return &qbit{api: api, host: host.Hostname()}
}

func (q *qbit) configName() string {
	return qbitClientName
}

func (q *qbit) add(ctx context.Context, torrent []byte, dir string) error {
	options := (&qbittorrent.TorrentAddOptions{SavePath: dir}).Prepare()
	_, err := q.api.AddTorrentFromMemoryCtx(ctx, torrent, options)
	return err
}

func (q *qbit) remove(ctx context.Context, hash string) error {
	return q.api.DeleteTorrentsCtx(ctx, []string{hash}, false)
}

func (q *qbit) state(ctx context.Context, hash string) (torrentState, error) {
	found, err := q.api.GetTorrentsCtx(ctx, qbittorrent.TorrentFilterOptions{Hashes: []string{hash}})
	if err != nil || len(found) == 0 {
		return torrentState{}, err
	}
	tor := found[0]
	return torrentState{
		present:     true,
		progress:    tor.Progress,
		downloaded:  tor.Downloaded,
		downloading: tor.State == qbittorrent.TorrentStateDownloading || tor.State == qbittorrent.TorrentStateStalledDl,
		seeding:     daemontest.QbitSeeding(tor),
		raw:         string(tor.State),
	}, nil
}

func (q *qbit) peerAddress(t *testing.T) string {
	t.Helper()
	prefs, err := q.api.GetAppPreferencesCtx(t.Context())
	require.NoError(t, err, "read qBittorrent preferences")
	return daemontest.PeerAddress(t, q.host, prefs.ListenPort)
}
