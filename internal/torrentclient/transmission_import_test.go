// Copyright (c) 2023 - 2025, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package torrentclient

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/hekmon/transmissionrpc/v3"
	"github.com/nuxencs/seasonpackarr/internal/domain"
	"github.com/stretchr/testify/require"
)

type stubTransmissionAPI struct {
	addCalled  bool
	addPayload transmissionrpc.TorrentAddPayload

	verifyCalls [][]string
	startCalls  [][]string

	statusSeq   []transmissionrpc.TorrentStatus
	statusIdx   int
	errorString string

	sessionDir string
	sessionErr error
}

func (s *stubTransmissionAPI) TorrentGet(context.Context, []string, []int64) ([]transmissionrpc.Torrent, error) {
	return nil, nil
}

func (s *stubTransmissionAPI) TorrentGetHashes(context.Context, []string, []string) ([]transmissionrpc.Torrent, error) {
	if len(s.statusSeq) == 0 {
		return nil, nil
	}
	idx := s.statusIdx
	if idx >= len(s.statusSeq) {
		idx = len(s.statusSeq) - 1
	}
	s.statusIdx++

	status := s.statusSeq[idx]
	tr := transmissionrpc.Torrent{Status: &status}
	if s.errorString != "" {
		es := s.errorString
		tr.ErrorString = &es
	}
	return []transmissionrpc.Torrent{tr}, nil
}

func (s *stubTransmissionAPI) TorrentAdd(_ context.Context, payload transmissionrpc.TorrentAddPayload) (transmissionrpc.Torrent, error) {
	s.addCalled = true
	s.addPayload = payload
	return transmissionrpc.Torrent{}, nil
}

func (s *stubTransmissionAPI) TorrentVerifyHashes(_ context.Context, hashes []string) error {
	s.verifyCalls = append(s.verifyCalls, append([]string(nil), hashes...))
	return nil
}

func (s *stubTransmissionAPI) TorrentStartHashes(_ context.Context, hashes []string) error {
	s.startCalls = append(s.startCalls, append([]string(nil), hashes...))
	return nil
}

func (s *stubTransmissionAPI) SessionArgumentsGetAll(context.Context) (transmissionrpc.SessionArguments, error) {
	dir := s.sessionDir
	return transmissionrpc.SessionArguments{DownloadDir: &dir}, s.sessionErr
}

func newTestTransmissionClient(stub *stubTransmissionAPI, policy domain.ImportPolicy) *transmissionClient {
	return &transmissionClient{
		c:             stub,
		policy:        policy,
		verifyTimeout: time.Second,
		pollInterval:  time.Millisecond,
	}
}

func TestTransmissionImportVerifiesThenStarts(t *testing.T) {
	const hash = "abc123"
	stub := &stubTransmissionAPI{
		statusSeq: []transmissionrpc.TorrentStatus{
			transmissionrpc.TorrentStatusCheck,   // verifying
			transmissionrpc.TorrentStatusStopped, // settled
		},
	}
	tc := newTestTransmissionClient(stub, domain.ImportPolicy{SavePath: "/data/tv", Tags: []string{"seasonpackarr"}})

	err := tc.Import(ImportRequest{TorrentBytes: []byte("torrent"), LegacyHash: hash, HasV1: true, SavePath: "/data/tv"})
	require.NoError(t, err)

	require.True(t, stub.addCalled)
	require.NotNil(t, stub.addPayload.MetaInfo)
	require.NotNil(t, stub.addPayload.DownloadDir)
	require.Equal(t, "/data/tv", *stub.addPayload.DownloadDir)
	require.NotNil(t, stub.addPayload.Paused)
	require.True(t, *stub.addPayload.Paused)
	require.Equal(t, []string{"seasonpackarr"}, stub.addPayload.Labels)

	require.Len(t, stub.verifyCalls, 1)
	require.Equal(t, []string{hash}, stub.verifyCalls[0])
	require.Len(t, stub.startCalls, 1)
	require.Equal(t, []string{hash}, stub.startCalls[0])
}

func TestTransmissionImportFailsOnError(t *testing.T) {
	stub := &stubTransmissionAPI{
		statusSeq:   []transmissionrpc.TorrentStatus{transmissionrpc.TorrentStatusStopped},
		errorString: "no data found",
	}
	tc := newTestTransmissionClient(stub, domain.ImportPolicy{SavePath: "/data/tv"})

	err := tc.Import(ImportRequest{TorrentBytes: []byte("t"), LegacyHash: "h", HasV1: true, SavePath: "/data/tv"})
	require.Error(t, err)
	requireImportFailure(t, err, domain.ReasonTorrentRecheckFailed, domain.FaultDependency)
	require.Empty(t, stub.startCalls)
}

func TestTransmissionImportRejectsPureV2Torrent(t *testing.T) {
	tc := newTestTransmissionClient(&stubTransmissionAPI{}, domain.ImportPolicy{SavePath: "/data/tv"})

	err := tc.Import(ImportRequest{TorrentBytes: []byte("torrent"), V2Hash: "v2-hash", SavePath: "/data/tv"})

	requireImportFailure(t, err, domain.ReasonTorrentUnsupported, domain.FaultRequest)
}

func TestTransmissionImportDestination(t *testing.T) {
	t.Run("explicit save path wins", func(t *testing.T) {
		tc := newTestTransmissionClient(&stubTransmissionAPI{sessionDir: "/downloads"}, domain.ImportPolicy{SavePath: "/data/tv"})
		destination, err := tc.ImportDestination()
		require.NoError(t, err)
		require.Equal(t, normalizePath("/data/tv"), destination.SavePath())
	})

	t.Run("falls back to session download dir", func(t *testing.T) {
		tc := newTestTransmissionClient(&stubTransmissionAPI{sessionDir: "/downloads"}, domain.ImportPolicy{})
		destination, err := tc.ImportDestination()
		require.NoError(t, err)
		require.Equal(t, normalizePath("/downloads"), destination.SavePath())
	})

	t.Run("errors when download dir empty", func(t *testing.T) {
		tc := newTestTransmissionClient(&stubTransmissionAPI{sessionDir: ""}, domain.ImportPolicy{})
		_, err := tc.ImportDestination()
		require.Error(t, err)
		requireImportFailure(t, err, domain.ReasonImportConfigInvalid, domain.FaultInternal)
	})

	t.Run("reports session read as dependency failure", func(t *testing.T) {
		tc := newTestTransmissionClient(&stubTransmissionAPI{sessionErr: errors.New("session unavailable")}, domain.ImportPolicy{})
		_, err := tc.ImportDestination()
		requireImportFailure(t, err, domain.ReasonImportDestinationFailed, domain.FaultDependency)
	})
}
