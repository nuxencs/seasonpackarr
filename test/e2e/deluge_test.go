// Copyright (c) 2023 - 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

//go:build e2e

package e2e

import (
	"context"
	"encoding/base64"
	"os"
	"strconv"
	"testing"

	"github.com/nuxencs/seasonpackarr/internal/daemontest"

	"github.com/autobrr/go-deluge"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	envDelugeType = "SEASONPACKARR_TEST_DELUGE_TYPE"
	envDelugeHost = "SEASONPACKARR_TEST_DELUGE_HOST"
	envDelugePort = "SEASONPACKARR_TEST_DELUGE_PORT"
	envDelugeUser = "SEASONPACKARR_TEST_DELUGE_USER"
	envDelugePass = "SEASONPACKARR_TEST_DELUGE_PASS"
	// delugeClientName and delugeClientType are the client name and type in
	// test/harness/seasonpackarr/config.yaml.
	delugeClientName = "deluge"
	delugeClientType = "deluge-v2"
)

// TestDelugeE2E_Scenarios runs the partial-pack, complete-pack, and
// candidate-rejection scenarios against Deluge.
func TestDelugeE2E_Scenarios(t *testing.T) {
	daemontest.RequireSettings(t, envDelugeType, envDelugeHost, envDelugePort)
	// run.sh sets the type from the newest Deluge integration entry, and the
	// committed config cannot follow it.
	require.Equal(t, delugeClientType, os.Getenv(envDelugeType),
		"the type of the deluge client in test/harness/seasonpackarr/config.yaml must match the newest Deluge entry")
	runScenarios(t, newDeluge(t))
}

// delugeClient is the client interface for Deluge 2, through go-deluge.
type delugeClient struct {
	api  *deluge.ClientV2
	host string
}

func newDeluge(t *testing.T) *delugeClient {
	t.Helper()
	host := os.Getenv(envDelugeHost)
	port, err := strconv.ParseUint(os.Getenv(envDelugePort), 10, 16)
	require.NoError(t, err, "parse %s", envDelugePort)
	api := deluge.NewV2(deluge.Settings{
		Hostname:         host,
		Port:             uint(port),
		Login:            os.Getenv(envDelugeUser),
		Password:         os.Getenv(envDelugePass),
		ReadWriteTimeout: daemontest.Timeout,
	})
	require.NoError(t, api.Connect(t.Context()), "connect to Deluge")
	t.Cleanup(func() {
		assert.NoError(t, api.Close(), "close the Deluge client")
	})
	// The config sets import.tags, and seasonpackarr applies it only through the
	// Label plugin. Each entry starts a fresh Deluge with the plugin off.
	require.NoError(t, api.EnablePlugin(t.Context(), "Label"), "enable the Deluge Label plugin")
	return &delugeClient{api: api, host: host}
}

func (dc *delugeClient) configName() string {
	return delugeClientName
}

func (dc *delugeClient) add(ctx context.Context, torrent []byte, dir string) error {
	// Deluge checks the files of a new torrent before it starts it. It uses the
	// file name only in its log.
	_, err := dc.api.AddTorrentFile(ctx, "e2e.torrent", base64.StdEncoding.EncodeToString(torrent), &deluge.Options{
		DownloadLocation: new(dir),
		AddPaused:        new(false),
	})
	return err
}

func (dc *delugeClient) remove(ctx context.Context, hash string) error {
	st, err := dc.status(ctx, hash)
	if err != nil || st == nil {
		return err
	}
	_, err = dc.api.RemoveTorrent(ctx, hash, false)
	return err
}

func (dc *delugeClient) state(ctx context.Context, hash string) (torrentState, error) {
	st, err := dc.status(ctx, hash)
	if err != nil || st == nil {
		return torrentState{}, err
	}
	state := deluge.TorrentState(st.State)
	return torrentState{
		present:    true,
		progress:   float64(st.Progress) / 100,
		downloaded: st.AllTimeDownload,
		// A queued torrent fails the download check, as queuedDL does for qBittorrent.
		downloading: state == deluge.StateDownloading,
		seeding:     state == deluge.StateSeeding,
		clientState: st.State,
	}, nil
}

// status returns the status of the torrent hash, or nil when Deluge does not
// have it. Like the adapter, it uses the filtered bulk read, which returns no
// entry for an unknown hash.
func (dc *delugeClient) status(ctx context.Context, hash string) (*deluge.TorrentStatus, error) {
	found, err := dc.api.TorrentsStatus(ctx, deluge.StateUnspecified, []string{hash})
	if err != nil {
		return nil, err
	}
	for _, st := range found {
		if st != nil {
			return st, nil
		}
	}
	return nil, nil
}

func (dc *delugeClient) peerAddress(t *testing.T) string {
	t.Helper()
	// Deluge picks a random listen port by default.
	port, err := dc.api.GetListenPort(t.Context())
	require.NoError(t, err, "read the Deluge listen port")
	return daemontest.PeerAddress(t, dc.host, int(port))
}
