// Copyright (c) 2023 - 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

//go:build e2e

package e2e

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/url"
	"os"
	"testing"

	"github.com/nuxencs/seasonpackarr/internal/daemontest"

	"github.com/hekmon/transmissionrpc/v3"
	"github.com/stretchr/testify/require"
)

const (
	envTransmissionHost = "SEASONPACKARR_TEST_TRANSMISSION_HOST"
	envTransmissionUser = "SEASONPACKARR_TEST_TRANSMISSION_USER"
	envTransmissionPass = "SEASONPACKARR_TEST_TRANSMISSION_PASS"
	// transmissionClientName is the client name in test/harness/seasonpackarr/config.yaml.
	transmissionClientName = "transmission"
)

// TestTransmissionE2E_Scenarios runs the partial-pack, complete-pack, and
// candidate-rejection scenarios against Transmission.
func TestTransmissionE2E_Scenarios(t *testing.T) {
	daemontest.RequireSettings(t, envTransmissionHost)
	runScenarios(t, newTransmission(t))
}

// transmission is the client interface for Transmission, through transmissionrpc.
type transmission struct {
	api  *transmissionrpc.Client
	host string
}

// transmissionFields are the torrent fields that state reads.
var transmissionFields = []string{"status", "percentDone", "downloadedEver", "errorString"}

func newTransmission(t *testing.T) *transmission {
	t.Helper()
	host, err := url.Parse(os.Getenv(envTransmissionHost))
	require.NoError(t, err)
	endpoint := host.JoinPath("/transmission/rpc")
	endpoint.User = url.UserPassword(os.Getenv(envTransmissionUser), os.Getenv(envTransmissionPass))
	api, err := transmissionrpc.New(endpoint, nil)
	require.NoError(t, err, "create the Transmission client")
	return &transmission{api: api, host: host.Hostname()}
}

func (tc *transmission) configName() string {
	return transmissionClientName
}

func (tc *transmission) add(ctx context.Context, torrent []byte, dir string) error {
	// Transmission checks the files of a new torrent before it starts it.
	_, err := tc.api.TorrentAdd(ctx, transmissionrpc.TorrentAddPayload{
		MetaInfo:    new(base64.StdEncoding.EncodeToString(torrent)),
		DownloadDir: new(dir),
		Paused:      new(false),
	})
	return err
}

func (tc *transmission) remove(ctx context.Context, hash string) error {
	found, err := tc.api.TorrentGetHashes(ctx, []string{"id"}, []string{hash})
	if err != nil || len(found) == 0 {
		return err
	}
	if found[0].ID == nil {
		return fmt.Errorf("torrent %s has no id", hash)
	}
	return tc.api.TorrentRemove(ctx, transmissionrpc.TorrentRemovePayload{IDs: []int64{*found[0].ID}})
}

func (tc *transmission) state(ctx context.Context, hash string) (torrentState, error) {
	found, err := tc.api.TorrentGetHashes(ctx, transmissionFields, []string{hash})
	if err != nil || len(found) == 0 {
		return torrentState{}, err
	}
	tor := found[0]
	if tor.Status == nil || tor.PercentDone == nil || tor.DownloadedEver == nil {
		return torrentState{}, fmt.Errorf("torrent %s does not have a requested field", hash)
	}
	status := *tor.Status
	clientState := status.String()
	if tor.ErrorString != nil && *tor.ErrorString != "" {
		clientState += fmt.Sprintf(" (error: %s)", *tor.ErrorString)
	}
	return torrentState{
		present:     true,
		progress:    *tor.PercentDone,
		downloaded:  *tor.DownloadedEver,
		downloading: status == transmissionrpc.TorrentStatusDownload,
		// Transmission renames each <file>.part when the file completes, so a
		// seeding pack has its final file names for the byte checks.
		seeding:     status == transmissionrpc.TorrentStatusSeed,
		clientState: clientState,
	}, nil
}

func (tc *transmission) peerAddress(t *testing.T) string {
	t.Helper()
	session, err := tc.api.SessionArgumentsGetAll(t.Context())
	require.NoError(t, err, "read the Transmission session")
	require.NotNil(t, session.PeerPort, "session has no peer-port")
	return daemontest.PeerAddress(t, tc.host, int(*session.PeerPort))
}
