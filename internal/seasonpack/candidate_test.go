// Copyright (c) 2023 - 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package seasonpack

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/nuxencs/seasonpackarr/internal/domain"
	"github.com/nuxencs/seasonpackarr/internal/torrentclient"
	"github.com/nuxencs/seasonpackarr/internal/torrentclient/torrentclienttest"
	"github.com/nuxencs/seasonpackarr/internal/torrents"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCandidate_UsesTorrentSummariesOnly(t *testing.T) {
	torrentClient := &torrentclienttest.Client{
		Torrents: []torrentclient.Torrent{
			{Name: "Candidate.S01E01.1080p.WEB-DL.H.264-RlsGrp", Hash: "ep1"},
		},
		SavePath: "/data/tv",
		FilesByHash: map[string][]torrentclient.File{
			"ep1": {{Name: "Candidate.S01E01.1080p.WEB-DL.H.264-RlsGrp.mkv", Size: 1}},
		},
	}
	f := newFixture(torrentClient)

	outcome := f.processor.Candidate(t.Context(), f.defaultClient(t), "Candidate.S01.1080p.WEB-DL.H.264-RlsGrp")

	require.NoError(t, outcome.Err)
	require.Equal(t, domain.StatusSuccessfulMatch, outcome.Status)
	require.Equal(t, Accepted, outcome.Class())
	require.Nil(t, outcome.Coverage, "the candidate check reads no torrent")
	require.Equal(t, 1, torrentClient.TorrentCalls)
	require.Zero(t, torrentClient.FileCalls, "candidate evaluation must not request torrent file details")
}

func TestCandidate_PropagatesCancellation(t *testing.T) {
	torrentClient := &torrentclienttest.Client{
		GetTorrentsFunc: func(ctx context.Context) ([]torrentclient.Torrent, error) {
			return nil, ctx.Err()
		},
	}
	f := newFixture(torrentClient)
	client := f.defaultClient(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	outcome := f.processor.Candidate(ctx, client, "Candidate.S01.1080p.WEB-DL.H.264-RlsGrp")

	require.ErrorIs(t, outcome.Err, context.Canceled)
	require.Equal(t, domain.StatusGetTorrentsError, outcome.Status)
	require.Equal(t, Failed, outcome.Class())
}

func TestCandidate_ReturnsStatusOfEachRejection(t *testing.T) {
	const episode = "Candidate.S01E01.1080p.WEB-DL.H.264-RlsGrp"
	tests := []struct {
		name     string
		release  string
		torrents []string
		want     domain.StatusCode
		class    Class
	}{
		{
			name:     "no matches for another title",
			release:  "Other.S01.1080p.WEB-DL.H.264-RlsGrp",
			torrents: []string{episode},
			want:     domain.StatusNoMatches,
			class:    Rejected,
		},
		{
			name:     "no matches for another release group",
			release:  "Candidate.S01.1080p.WEB-DL.H.264-OtherGrp",
			torrents: []string{episode},
			want:     domain.StatusNoMatches,
			class:    Rejected,
		},
		{
			name:     "already in client",
			release:  "Candidate.S01.1080p.WEB-DL.H.264-RlsGrp",
			torrents: []string{episode, "Candidate.S01.1080p.WEB-DL.H.264-RlsGrp"},
			want:     domain.StatusAlreadyInClient,
			class:    Rejected,
		},
		{
			name:    "season pack with no torrent of its season",
			release: "Candidate.S01.1080p.WEB-DL.H.264-RlsGrp",
			want:    domain.StatusNoMatches,
			class:   Rejected,
		},
		{
			name:     "not a season pack with a torrent of its season",
			release:  "Candidate.S01E02.1080p.WEB-DL.H.264-RlsGrp.mkv",
			torrents: []string{episode},
			want:     domain.StatusNotASeasonPack,
			class:    Rejected,
		},
		{
			name:     "not a season pack with no torrent of its season",
			release:  "Candidate.S02E01.1080p.WEB-DL.H.264-RlsGrp",
			torrents: []string{episode},
			want:     domain.StatusNotASeasonPack,
			class:    Rejected,
		},
		{
			name:     "empty release name",
			torrents: []string{episode},
			want:     domain.StatusAnnounceNameError,
			class:    Failed,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			torrentClient := &torrentclienttest.Client{}
			for _, name := range tt.torrents {
				torrentClient.Torrents = append(torrentClient.Torrents, torrentclient.Torrent{Name: name, Hash: name})
			}
			f := newFixture(torrentClient)

			outcome := f.processor.Candidate(t.Context(), f.defaultClient(t), tt.release)

			assert.Equal(t, tt.want, outcome.Status)
			assert.EqualError(t, outcome.Err, tt.want.String())
			assert.Equal(t, tt.class, outcome.Class())
			assert.Zero(t, torrentClient.FileCalls)
		})
	}
}

func TestCandidate_ReadsClientBeforeSeasonPackCheck(t *testing.T) {
	// `setup filter` sends a single-episode sample and needs 468 to find a client that seasonpackarr cannot read.
	torrentClient := &torrentclienttest.Client{TorrentsErr: errors.New("client offline")}
	f := newFixture(torrentClient)

	outcome := f.processor.Candidate(t.Context(), f.defaultClient(t), "Candidate.S01E01.1080p.WEB-DL.H.264-RlsGrp")

	assert.Equal(t, domain.StatusGetTorrentsError, outcome.Status)
	assert.ErrorContains(t, outcome.Err, "client offline")
	assert.Equal(t, Failed, outcome.Class())
}

func TestCandidate_LogsEachIncompatibleProperty(t *testing.T) {
	tests := []struct {
		name   string
		pack   string
		client string
		field  string
		want   any
		got    any
	}{
		{name: "resolution", pack: "Show.S01.1080p.WEB-DL.H.264-GRP", client: "Show.S01E01.2160p.WEB-DL.H.264-GRP", field: "resolution", want: "1080p", got: "2160p"},
		{name: "source", pack: "Show.S01.1080p.WEB-DL.H.264-GRP", client: "Show.S01E01.1080p.BluRay.H.264-GRP", field: "source", want: "WEB-DL", got: "BluRay"},
		{name: "release group", pack: "Show.S01.1080p.WEB-DL.H.264-GRP", client: "Show.S01E01.1080p.WEB-DL.H.264-OTHER", field: "release_group", want: "GRP", got: "OTHER"},
		{name: "cut", pack: "Show.S01.Uncut.1080p.WEB-DL.H.264-GRP", client: "Show.S01E01.1080p.WEB-DL.H.264-GRP", field: "cut", want: []any{"Uncut"}, got: nil},
		{name: "edition", pack: "Show.S01.1080p.WEB-DL.H.264-GRP", client: "Show.S01E01.Special.Edition.1080p.WEB-DL.H.264-GRP", field: "edition", want: nil, got: []any{"Special.Edition"}},
		{name: "repack status", pack: "Show.S01.1080p.WEB-DL.H.264-GRP", client: "Show.S01E01.REPACK.1080p.WEB-DL.H.264-GRP", field: "repack_status", want: nil, got: []any{"REPACK"}},
		{name: "HDR", pack: "Show.S01.1080p.WEB-DL.HDR.H.265-GRP", client: "Show.S01E01.1080p.WEB-DL.H.265-GRP", field: "hdr", want: []any{"HDR"}, got: nil},
		{name: "streaming service", pack: "Show.S01.1080p.AMZN.WEB-DL.H.264-GRP", client: "Show.S01E01.1080p.NF.WEB-DL.H.264-GRP", field: "streaming_service", want: "AMZN", got: "NF"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture(&torrentclienttest.Client{Torrents: []torrentclient.Torrent{{Name: tt.client, Hash: "ep1"}}})

			outcome := f.processor.Candidate(t.Context(), f.defaultClient(t), tt.pack)

			require.Equal(t, domain.StatusNoMatches, outcome.Status)
			mismatch := f.log.Events(t).Require(t, "client release is not compatible")
			assert.Equal(t, "compatibility_mismatch", mismatch["reason"])
			assert.Equal(t, tt.field, mismatch["field"])
			assert.Equal(t, tt.want, mismatch["want"])
			assert.Equal(t, tt.got, mismatch["got"])
			assert.Equal(t, tt.client, mismatch["client_release"])
		})
	}
}

func TestInventory_SharedByCandidateAndMatch(t *testing.T) {
	torrentBytes, err := torrents.TorrentFromRls("Inventory.S01.1080p.WEB-DL.H.264-RlsGrp", 1)
	require.NoError(t, err)

	sourceDir := t.TempDir()
	writeEpisode(t, filepath.Join(sourceDir, "Inventory.S01E01.1080p.WEB-DL.H.264-RlsGrp.mkv"))
	torrentClient := &torrentclienttest.Client{
		Torrents: []torrentclient.Torrent{
			{Name: "Inventory.S01E01.1080p.WEB-DL.H.264-RlsGrp", Hash: "ep1"},
		},
		SavePath: sourceDir,
		FilesByHash: map[string][]torrentclient.File{
			"ep1": {{Name: "Inventory.S01E01.1080p.WEB-DL.H.264-RlsGrp.mkv", Size: 1}},
		},
	}
	f := newFixture(torrentClient)

	// Each request has its own handle, as in the webhook handlers.
	outcome := f.processor.Candidate(t.Context(), f.defaultClient(t), "Inventory.S01.1080p.WEB-DL.H.264-RlsGrp")
	require.NoError(t, outcome.Err)
	require.Equal(t, domain.StatusSuccessfulMatch, outcome.Status)

	outcome = f.processor.Match(t.Context(), f.defaultClient(t), "Inventory.S01.1080p.WEB-DL.H.264-RlsGrp", TorrentBytes(torrentBytes))
	require.NoError(t, outcome.Err)
	require.Equal(t, domain.StatusSuccessfulMatch, outcome.Status)
	require.Equal(t, 1, torrentClient.TorrentCalls, "candidate and match must share one inventory snapshot")
}
