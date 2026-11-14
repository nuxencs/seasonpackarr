// Copyright (c) 2023 - 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package http

import (
	"context"
	"encoding/base64"
	"testing"

	"github.com/nuxencs/seasonpackarr/internal/domain"
	"github.com/nuxencs/seasonpackarr/internal/torrentclient"
	"github.com/nuxencs/seasonpackarr/internal/torrents"

	"github.com/stretchr/testify/require"
)

func TestCandidateSeasonPack_UsesTorrentSummariesOnly(t *testing.T) {
	resetProcessorGlobals()
	torrentClient := &fakeTorrentClient{
		torrents: []torrentclient.Torrent{
			{Name: "Candidate.S01E01.1080p.WEB-DL.H.264-RlsGrp", Hash: "ep1", SavePath: "/data/tv"},
		},
		filesByHash: map[string][]torrentclient.File{
			"ep1": {{Name: "Candidate.S01E01.1080p.WEB-DL.H.264-RlsGrp.mkv", Size: 1}},
		},
	}

	p := newImportProcessor(newTestInventory(torrentClient))
	p.req = &request{
		Name:       "Candidate.S01.1080p.WEB-DL.H.264-RlsGrp",
		ClientName: "default",
	}

	statusCode, err := p.candidateSeasonPack(t.Context())
	require.NoError(t, err)
	require.Equal(t, domain.StatusSuccessfulMatch, statusCode)
	require.Equal(t, 1, torrentClient.torrentCalls)
	require.Zero(t, torrentClient.fileCalls, "candidate evaluation must not request torrent file details")
}

func TestCandidateSeasonPack_PropagatesCancellation(t *testing.T) {
	resetProcessorGlobals()
	torrentClient := &fakeTorrentClient{
		getTorrents: func(ctx context.Context) ([]torrentclient.Torrent, error) {
			return nil, ctx.Err()
		},
	}
	p := newImportProcessor(newTestInventory(torrentClient))
	p.req = &request{
		Name:       "Candidate.S01.1080p.WEB-DL.H.264-RlsGrp",
		ClientName: "default",
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	statusCode, err := p.candidateSeasonPack(ctx)
	require.ErrorIs(t, err, context.Canceled)
	require.Equal(t, domain.StatusGetTorrentsError, statusCode)
}

func TestCandidateMismatchField(t *testing.T) {
	t.Parallel()

	tests := map[domain.StatusCode]string{
		domain.StatusResolutionMismatch:       "resolution",
		domain.StatusSourceMismatch:           "source",
		domain.StatusRlsGrpMismatch:           "release_group",
		domain.StatusCutMismatch:              "cut",
		domain.StatusEditionMismatch:          "edition",
		domain.StatusRepackStatusMismatch:     "repack_status",
		domain.StatusHdrMismatch:              "hdr",
		domain.StatusStreamingServiceMismatch: "streaming_service",
	}
	for statusCode, want := range tests {
		require.Equal(t, want, candidateMismatchField(statusCode))
	}
}

func TestInventory_SharedByCandidateAndMatch(t *testing.T) {
	resetProcessorGlobals()
	torrentBytes, err := torrents.TorrentFromRls("Inventory.S01.1080p.WEB-DL.H.264-RlsGrp", 1)
	require.NoError(t, err)

	torrentClient := &fakeTorrentClient{
		torrents: []torrentclient.Torrent{
			{Name: "Inventory.S01E01.1080p.WEB-DL.H.264-RlsGrp", Hash: "ep1", SavePath: "/data/tv"},
		},
		filesByHash: map[string][]torrentclient.File{
			"ep1": {{Name: "Inventory.S01E01.1080p.WEB-DL.H.264-RlsGrp.mkv", Size: 1}},
		},
	}
	inv := newTestInventory(torrentClient)
	candidateProcessor := newImportProcessor(inv)
	candidateProcessor.req = &request{
		Name:       "Inventory.S01.1080p.WEB-DL.H.264-RlsGrp",
		ClientName: "default",
	}

	statusCode, err := candidateProcessor.candidateSeasonPack(t.Context())
	require.NoError(t, err)
	require.Equal(t, domain.StatusSuccessfulMatch, statusCode)

	matchProcessor := newImportProcessor(inv)
	matchProcessor.req = &request{
		Name:       "Inventory.S01.1080p.WEB-DL.H.264-RlsGrp",
		Torrent:    []byte(base64.StdEncoding.EncodeToString(torrentBytes)),
		ClientName: "default",
	}
	statusCode, err = matchProcessor.matchSeasonPack(t.Context())
	require.NoError(t, err)
	require.Equal(t, domain.StatusSuccessfulMatch, statusCode)
	require.Equal(t, 1, torrentClient.torrentCalls, "candidate and match must share one inventory snapshot")
}
