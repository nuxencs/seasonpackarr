// Copyright (c) 2023 - 2025, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package torrentclient

import (
	"context"
	"testing"

	"github.com/hekmon/transmissionrpc/v3"
	"github.com/nuxencs/seasonpackarr/internal/domain"
	"github.com/stretchr/testify/require"
)

type stubTransmissionAPI struct {
	addCalled  bool
	addPayload transmissionrpc.TorrentAddPayload

	getErr error

	sessionDir string
}

func (s *stubTransmissionAPI) TorrentGet(context.Context, []string, []int64) ([]transmissionrpc.Torrent, error) {
	return nil, nil
}

func (s *stubTransmissionAPI) TorrentGetHashes(context.Context, []string, []string) ([]transmissionrpc.Torrent, error) {
	return nil, s.getErr
}

func (s *stubTransmissionAPI) TorrentAdd(_ context.Context, payload transmissionrpc.TorrentAddPayload) (transmissionrpc.Torrent, error) {
	s.addCalled = true
	s.addPayload = payload
	return transmissionrpc.Torrent{}, nil
}

func (s *stubTransmissionAPI) SessionArgumentsGetAll(context.Context) (transmissionrpc.SessionArguments, error) {
	dir := s.sessionDir
	return transmissionrpc.SessionArguments{DownloadDir: &dir}, nil
}

func newTestTransmissionClient(stub *stubTransmissionAPI, policy domain.ImportPolicy) *transmissionClient {
	return &transmissionClient{
		c:      stub,
		policy: policy,
	}
}

// TestTransmissionImport_AddsStartedWithoutVerify is the regression guard for
// the autobrr timeout: Transmission verifies a partial pack and then starts it
// by itself, so the adapter must not force or wait for a verify.
func TestTransmissionImport_AddsStartedWithoutVerify(t *testing.T) {
	const hash = "abc123"
	stub := &stubTransmissionAPI{}
	tc := newTestTransmissionClient(stub, domain.ImportPolicy{SavePath: "/data/tv", Tags: []string{"seasonpackarr"}})

	report, err := tc.Import(t.Context(), ImportRequest{TorrentBytes: []byte("torrent"), LegacyHash: hash, HasV1: true, SavePath: "/data/tv"})
	require.NoError(t, err)
	require.Equal(t, []ImportStage{
		ImportStageConfig,
		ImportStageAdd,
	}, importStageNames(report))

	require.True(t, stub.addCalled)
	require.NotNil(t, stub.addPayload.MetaInfo)
	require.NotNil(t, stub.addPayload.DownloadDir)
	require.Equal(t, "/data/tv", *stub.addPayload.DownloadDir)
	require.NotNil(t, stub.addPayload.Paused)
	require.False(t, *stub.addPayload.Paused)
	require.Equal(t, []string{"seasonpackarr"}, stub.addPayload.Labels)
}

func TestTransmissionImportDestination(t *testing.T) {
	t.Run("explicit save path wins", func(t *testing.T) {
		tc := newTestTransmissionClient(&stubTransmissionAPI{sessionDir: "/downloads"}, domain.ImportPolicy{SavePath: "/data/tv"})
		destination, err := tc.ImportDestination(t.Context())
		require.NoError(t, err)
		require.Equal(t, normalizePath("/data/tv"), destination.SavePath())
	})

	t.Run("falls back to session download dir", func(t *testing.T) {
		tc := newTestTransmissionClient(&stubTransmissionAPI{sessionDir: "/downloads"}, domain.ImportPolicy{})
		destination, err := tc.ImportDestination(t.Context())
		require.NoError(t, err)
		require.Equal(t, normalizePath("/downloads"), destination.SavePath())
	})

	t.Run("errors when download dir empty", func(t *testing.T) {
		tc := newTestTransmissionClient(&stubTransmissionAPI{sessionDir: ""}, domain.ImportPolicy{})
		_, err := tc.ImportDestination(t.Context())
		require.Error(t, err)
		require.Equal(t, domain.StatusImportConfigError, ImportStatusCode(err))
	})
}
