// Copyright (c) 2023 - 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package http

import (
	"context"
	"encoding/base64"
	"testing"
	"time"

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

	p := newImportProcessor()
	p.req = &request{
		Name:       "Candidate.S01.1080p.WEB-DL.H.264-RlsGrp",
		Client:     torrentClient,
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
	p := newImportProcessor()
	p.req = &request{
		Name:       "Candidate.S01.1080p.WEB-DL.H.264-RlsGrp",
		Client:     torrentClient,
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
	candidateProcessor := newImportProcessor()
	candidateProcessor.req = &request{
		Name:       "Inventory.S01.1080p.WEB-DL.H.264-RlsGrp",
		Client:     torrentClient,
		ClientName: "default",
	}

	statusCode, err := candidateProcessor.candidateSeasonPack(t.Context())
	require.NoError(t, err)
	require.Equal(t, domain.StatusSuccessfulMatch, statusCode)

	matchProcessor := newImportProcessor()
	matchProcessor.req = &request{
		Name:       "Inventory.S01.1080p.WEB-DL.H.264-RlsGrp",
		Torrent:    []byte(base64.StdEncoding.EncodeToString(torrentBytes)),
		Client:     torrentClient,
		ClientName: "default",
	}
	statusCode, err = matchProcessor.matchSeasonPack(t.Context())
	require.NoError(t, err)
	require.Equal(t, domain.StatusSuccessfulMatch, statusCode)
	require.Equal(t, 1, torrentClient.torrentCalls, "candidate and match must share one inventory snapshot")
}

var clientConfigsEqualSink bool

func TestClientConfigsEqual_CoversEveryField(t *testing.T) {
	base := domain.Client{
		Type:     "qbittorrent",
		Host:     "localhost",
		Port:     8080,
		Username: "user",
		Password: "password",
		APIKey:   "api-key",
		Import: domain.ImportPolicy{
			SavePath:      "/save",
			Tags:          []string{"one", "two"},
			Category:      "category",
			DownloadPath:  "/download",
			ContentLayout: "subfolder",
		},
	}
	require.True(t, clientConfigsEqual(base, cloneClientConfig(base)))

	tests := map[string]func(*domain.Client){
		"type":           func(client *domain.Client) { client.Type = "transmission" },
		"host":           func(client *domain.Client) { client.Host = "other" },
		"port":           func(client *domain.Client) { client.Port++ },
		"username":       func(client *domain.Client) { client.Username = "other" },
		"password":       func(client *domain.Client) { client.Password = "other" },
		"api key":        func(client *domain.Client) { client.APIKey = "other" },
		"save path":      func(client *domain.Client) { client.Import.SavePath = "/other" },
		"tags":           func(client *domain.Client) { client.Import.Tags[0] = "other" },
		"category":       func(client *domain.Client) { client.Import.Category = "other" },
		"download path":  func(client *domain.Client) { client.Import.DownloadPath = "/other" },
		"content layout": func(client *domain.Client) { client.Import.ContentLayout = "original" },
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			changed := cloneClientConfig(base)
			mutate(&changed)
			require.False(t, clientConfigsEqual(base, changed))
		})
	}

	withoutTags := cloneClientConfig(base)
	withoutTags.Import.Tags = nil
	emptyTags := cloneClientConfig(base)
	emptyTags.Import.Tags = []string{}
	require.True(t, clientConfigsEqual(withoutTags, emptyTags), "nil and empty tags have the same policy")

	allocations := testing.AllocsPerRun(1000, func() {
		clientConfigsEqualSink = clientConfigsEqual(base, base)
	})
	require.Zero(t, allocations, "config equality is on the request path")
}

func TestInventory_IsScopedToClientAndFuzzyConfig(t *testing.T) {
	resetProcessorGlobals()
	torrentClient := &fakeTorrentClient{torrents: []torrentclient.Torrent{{Name: "Series.S01E01.1080p.WEB-DL-GRP"}}}
	p := newTestProcessor(torrentClient)
	client := &domain.Client{Type: "qbittorrent", Import: domain.ImportPolicy{Category: "one"}}

	_, err := p.getAllTorrents(t.Context(), "default", client, domain.FuzzyMatching{})
	require.NoError(t, err)
	cached, ok := entryMap.Load("default")
	require.True(t, ok)
	cached.expiresAt = time.Now().Add(time.Minute)
	_, err = p.getAllTorrents(t.Context(), "default", client, domain.FuzzyMatching{})
	require.NoError(t, err)
	require.Equal(t, 1, torrentClient.torrentCalls, "unchanged config should reuse cached entries")

	changedClient := cloneClientConfig(*client)
	changedClient.Import.Category = "two"
	_, err = p.getAllTorrents(t.Context(), "default", &changedClient, domain.FuzzyMatching{})
	require.NoError(t, err)
	require.Equal(t, 2, torrentClient.torrentCalls, "client config change should refresh cached entries")

	_, err = p.getAllTorrents(t.Context(), "default", &changedClient, domain.FuzzyMatching{SkipYearCompare: true})
	require.NoError(t, err)
	require.Equal(t, 3, torrentClient.torrentCalls, "fuzzy config change should refresh cached entries")
}

func TestInventory_ReusesParsedReleaseAndComparableTitle(t *testing.T) {
	resetProcessorGlobals()
	const torrentName = "Series.S01E01.1080p.WEB-DL-GRP"
	torrentClient := &fakeTorrentClient{torrents: []torrentclient.Torrent{{Name: torrentName}}}
	p := newTestProcessor(torrentClient)
	client := &domain.Client{Type: "qbittorrent"}

	_, err := p.getAllTorrents(t.Context(), "default", client, domain.FuzzyMatching{})
	require.NoError(t, err)
	original, ok := entryMap.Load("default")
	require.True(t, ok)
	originalRelease, ok := original.rlsMap[torrentName]
	require.True(t, ok)
	require.NotEmpty(t, originalRelease.comparableTitle)

	original.expiresAt = time.Time{}
	_, err = p.getAllTorrents(t.Context(), "default", client, domain.FuzzyMatching{})
	require.NoError(t, err)
	refreshed, ok := entryMap.Load("default")
	require.True(t, ok)
	refreshedRelease, ok := refreshed.rlsMap[torrentName]
	require.True(t, ok)

	require.Same(t, originalRelease.release, refreshedRelease.release)
	require.Equal(t, originalRelease.comparableTitle, refreshedRelease.comparableTitle)
	require.Same(t, refreshedRelease.release, refreshed.entriesMap[refreshedRelease.comparableTitle][0].release)
}

func TestInventory_InvalidationDuringScanCannotRestoreStaleCache(t *testing.T) {
	resetProcessorGlobals()
	started, proceed := make(chan struct{}), make(chan struct{})
	torrentClient := &fakeTorrentClient{getTorrents: func(context.Context) ([]torrentclient.Torrent, error) {
		close(started)
		<-proceed
		return []torrentclient.Torrent{{Name: "Example.S01E01.1080p.WEB-DL.H.264-RlsGrp"}}, nil
	}}
	p := newTestProcessor(torrentClient)
	done := make(chan error, 1)
	go func() {
		_, err := p.getAllTorrents(t.Context(), "default", &domain.Client{}, domain.FuzzyMatching{})
		done <- err
	}()
	<-started
	invalidateClientImports(domain.Client{})
	close(proceed)
	require.NoError(t, <-done)
	_, cached := entryMap.Load("default")
	require.False(t, cached, "an import invalidated the in-flight inventory")
}
