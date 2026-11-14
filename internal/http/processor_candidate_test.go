// Copyright (c) 2023 - 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package http

import (
	"context"
	"encoding/base64"
	"errors"
	"testing"
	"time"

	"github.com/nuxencs/seasonpackarr/internal/domain"
	"github.com/nuxencs/seasonpackarr/internal/torrentclient"
	"github.com/nuxencs/seasonpackarr/internal/torrents"

	"github.com/autobrr/rls"
	"github.com/stretchr/testify/assert"
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

	changedClient := client.Clone()
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
	originalRelease := requireParsedRelease(t, *client, torrentName)
	require.NotEmpty(t, originalRelease.comparableTitle)

	original.expiresAt = time.Time{}
	_, err = p.getAllTorrents(t.Context(), "default", client, domain.FuzzyMatching{})
	require.NoError(t, err)
	refreshed, ok := entryMap.Load("default")
	require.True(t, ok)
	refreshedRelease := requireParsedRelease(t, *client, torrentName)

	require.Same(t, originalRelease.release, refreshedRelease.release)
	require.Equal(t, originalRelease.comparableTitle, refreshedRelease.comparableTitle)
	require.Same(t, refreshedRelease.release, refreshed.entriesMap[refreshedRelease.comparableTitle][0].release)
}

func TestInventory_ReusesParsedReleasesAfterImportInvalidation(t *testing.T) {
	resetProcessorGlobals()
	const torrentName = "Series.S01E01.1080p.WEB-DL-GRP"
	torrentClient := &fakeTorrentClient{torrents: []torrentclient.Torrent{{Name: torrentName}, {Name: torrentName}}}
	p := newTestProcessor(torrentClient)
	client := &domain.Client{Type: "qbittorrent", Host: "qbittorrent", Import: domain.ImportPolicy{Category: "tv-hd"}}
	alias := client.Clone()
	alias.Import.Category = "tv-uhd"

	entries, err := p.getAllTorrents(t.Context(), "default", client, domain.FuzzyMatching{})
	require.NoError(t, err)
	original := requireParsedRelease(t, *client, torrentName)
	crossSeeds := entries[original.comparableTitle]
	require.Len(t, crossSeeds, 2)
	require.Same(t, crossSeeds[0].release, crossSeeds[1].release, "a repeated name in one scan is parsed once")

	invalidateClientImports(*client)
	_, cached := entryMap.Load("default")
	require.False(t, cached, "an import drops the inventory")

	entries, err = p.getAllTorrents(t.Context(), "default-4k", &alias, domain.FuzzyMatching{})
	require.NoError(t, err)
	require.Equal(t, 2, torrentClient.torrentCalls, "the dropped inventory is read again")
	require.Same(t, original.release, entries[original.comparableTitle][0].release,
		"a client that shares the endpoint reuses the names parsed before the import")

	_, err = p.getAllTorrents(t.Context(), "default", client, domain.FuzzyMatching{SkipYearCompare: true})
	require.NoError(t, err)
	require.NotSame(t, original.release, requireParsedRelease(t, *client, torrentName).release,
		"changed fuzzy matching parses the names again")
}

func requireParsedRelease(t *testing.T, client domain.Client, torrentName string) cachedRelease {
	t.Helper()
	names, ok := parsedReleaseMap.Load(parsedNamesKeyFor(client))
	require.True(t, ok, "parsed names missing")
	parsed, ok := names.releases[torrentName]
	require.True(t, ok, "parsed release missing")
	return parsed
}

// TestInventory_KeepsParsedNamesOfEachScopeOnOneEndpoint: two clients of one
// qBittorrent with different inventory scopes must not drop each other's parsed names.
func TestInventory_KeepsParsedNamesOfEachScopeOnOneEndpoint(t *testing.T) {
	resetProcessorGlobals()
	const (
		hdName     = "Series.S01E01.1080p.WEB-DL-GRP"
		uhdName    = "Series.S01E01.2160p.WEB-DL-GRP"
		sharedName = "Other.S01E01.1080p.WEB-DL-GRP"
	)
	hd := &domain.Client{
		Type: "qbittorrent", Host: "qbittorrent",
		Import:    domain.ImportPolicy{Category: "tv-hd"},
		Inventory: domain.InventoryScope{IncludeCategories: []string{"tv-hd", "cross-seed"}},
	}
	uhd := hd.Clone()
	uhd.Import.Category = "tv-uhd"
	uhd.Inventory.IncludeCategories = []string{"tv-uhd", "cross-seed"}
	hdProcessor := newTestProcessor(&fakeTorrentClient{torrents: []torrentclient.Torrent{{Name: hdName}, {Name: sharedName}}})
	uhdProcessor := newTestProcessor(&fakeTorrentClient{torrents: []torrentclient.Torrent{{Name: uhdName}, {Name: sharedName}}})

	hdEntries, err := hdProcessor.getAllTorrents(t.Context(), "sonarr-hd", hd, domain.FuzzyMatching{})
	require.NoError(t, err)
	uhdEntries, err := uhdProcessor.getAllTorrents(t.Context(), "sonarr-uhd", &uhd, domain.FuzzyMatching{})
	require.NoError(t, err)
	assert.Same(t, inventoryRelease(t, hdEntries, sharedName), inventoryRelease(t, uhdEntries, sharedName),
		"a name in both scopes is parsed once")

	cached, ok := entryMap.Load("sonarr-hd")
	require.True(t, ok)
	cached.expiresAt = time.Time{}
	refreshed, err := hdProcessor.getAllTorrents(t.Context(), "sonarr-hd", hd, domain.FuzzyMatching{})
	require.NoError(t, err)
	assert.Same(t, inventoryRelease(t, hdEntries, hdName), inventoryRelease(t, refreshed, hdName),
		"the scan of another scope keeps the parsed names of this scope")
}

// inventoryRelease returns the parsed release of the torrent name in entries.
func inventoryRelease(t *testing.T, entries map[string][]entry, name string) *rls.Release {
	t.Helper()
	for _, bucket := range entries {
		for _, e := range bucket {
			if e.torrent.Name == name {
				return e.release
			}
		}
	}
	require.Failf(t, "torrent missing from the inventory", "%s", name)
	return nil
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
	_, parsed := parsedReleaseMap.Load(parsedNamesKeyFor(domain.Client{}))
	assert.True(t, parsed, "an import keeps the parsed names of the scan, because they stay valid")
}

func TestInventoryWarmer_WarmsEveryClientAndSharesParsedNames(t *testing.T) {
	resetProcessorGlobals()
	const torrentName = "Series.S01E01.1080p.WEB-DL-GRP"
	torrentClient := &fakeTorrentClient{torrents: []torrentclient.Torrent{{Name: torrentName, Hash: "ep1"}}}
	hd := &domain.Client{Type: "qbittorrent", Host: "qbittorrent", Import: domain.ImportPolicy{Category: "tv-hd"}}
	uhd := &domain.Client{Type: "qbittorrent", Host: "qbittorrent", Import: domain.ImportPolicy{Category: "tv-uhd"}}
	storeTestClient("default", hd, torrentClient)
	storeTestClient("default-4k", uhd, torrentClient)
	cfg := &fakeConfig{config: domain.Config{Clients: map[string]*domain.Client{"default": hd, "default-4k": uhd}}}

	inventoryWarmer{log: newTraceLogger(t), cfg: cfg, retryDelay: time.Millisecond}.run(t.Context())

	assert.Equal(t, 2, torrentClient.torrentCalls, "each client has its own inventory")
	first, ok := entryMap.Load("default")
	require.True(t, ok, "the first announce finds the inventory")
	second, ok := entryMap.Load("default-4k")
	require.True(t, ok, "the first announce of an alias finds its inventory")
	parsed := requireParsedRelease(t, *uhd, torrentName)
	assert.Same(t, parsed.release, first.entriesMap[parsed.comparableTitle][0].release)
	assert.Same(t, parsed.release, second.entriesMap[parsed.comparableTitle][0].release, "the alias reuses the parsed names")
}

func TestInventoryWarmer_ContinuesAfterClientFailure(t *testing.T) {
	resetProcessorGlobals()
	failing := &fakeTorrentClient{torrentsErr: errors.New("connection refused")}
	working := &fakeTorrentClient{torrents: []torrentclient.Torrent{{Name: "Series.S01E01.1080p.WEB-DL-GRP", Hash: "ep1"}}}
	down := &domain.Client{Type: "qbittorrent", Host: "down"}
	up := &domain.Client{Type: "qbittorrent", Host: "up"}
	storeTestClient("anime", down, failing)
	storeTestClient("default", up, working)
	cfg := &fakeConfig{config: domain.Config{Clients: map[string]*domain.Client{"anime": down, "default": up}}}
	log := newTraceLogger(t)

	inventoryWarmer{log: log, cfg: cfg, retryDelay: time.Millisecond}.run(t.Context())

	assert.Equal(t, 1, working.torrentCalls, "a ready client is read once")
	assert.Equal(t, inventoryWarmAttempts, failing.torrentCalls, "a failed client is tried again")
	_, ok := entryMap.Load("default")
	assert.True(t, ok, "a failed client does not stop the others")
	log.Events(t).RequireField(t, "could not read client inventory at startup", "clientname", "anime")
}

func TestInventoryWarmer_RetriesClientThatStartsLater(t *testing.T) {
	resetProcessorGlobals()
	torrentClient := &fakeTorrentClient{}
	torrentClient.getTorrents = func(context.Context) ([]torrentclient.Torrent, error) {
		if torrentClient.torrentCalls == 1 {
			return nil, errors.New("connection refused")
		}
		return []torrentclient.Torrent{{Name: "Series.S01E01.1080p.WEB-DL-GRP", Hash: "ep1"}}, nil
	}
	client := &domain.Client{Type: "qbittorrent", Host: "qbittorrent"}
	storeTestClient("default", client, torrentClient)
	cfg := &fakeConfig{config: domain.Config{Clients: map[string]*domain.Client{"default": client}}}
	log := newTraceLogger(t)

	inventoryWarmer{log: log, cfg: cfg, retryDelay: time.Millisecond}.run(t.Context())

	assert.Equal(t, 2, torrentClient.torrentCalls)
	_, ok := entryMap.Load("default")
	assert.True(t, ok, "the second attempt fills the inventory")
	log.Events(t).RequireField(t, "client inventory ready", "clientname", "default")
}

func TestInventoryWarmer_UsesReloadedConfig(t *testing.T) {
	resetProcessorGlobals()
	cfg := &fakeConfig{}
	torrentClient := &fakeTorrentClient{}
	client := &domain.Client{Type: "qbittorrent", Host: "qbittorrent"}
	torrentClient.getTorrents = func(context.Context) ([]torrentclient.Torrent, error) {
		if torrentClient.torrentCalls == 1 {
			// A reload removes the client while it is not ready.
			cfg.Store(domain.Config{})
			return nil, errors.New("connection refused")
		}
		return nil, nil
	}
	storeTestClient("default", client, torrentClient)
	cfg.Store(domain.Config{Clients: map[string]*domain.Client{"default": client}})

	inventoryWarmer{log: newTraceLogger(t), cfg: cfg, retryDelay: time.Millisecond}.run(t.Context())

	assert.Equal(t, 1, torrentClient.torrentCalls, "a client that the reload removed is not read again")
}
