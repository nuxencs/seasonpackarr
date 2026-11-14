// Copyright (c) 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package http

import (
	"context"
	"testing"
	"time"

	"github.com/nuxencs/seasonpackarr/internal/domain"
	"github.com/nuxencs/seasonpackarr/internal/torrentclient"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const cacheTestTorrentName = "Series.S01E01.1080p.WEB-DL-GRP"

// closingTorrentClient records Close like the Deluge adapter, which holds a daemon connection.
type closingTorrentClient struct {
	*fakeTorrentClient
	closeCalls int
}

func (c *closingTorrentClient) Close() error {
	c.closeCalls++
	return nil
}

// cacheClient fills the client, inventory, and parsed-name caches of name.
func cacheClient(t *testing.T, name string, cfg *domain.Client, client torrentclient.TorrentClient) {
	t.Helper()
	storeTestClient(name, cfg, client)
	p := newTestProcessor(client)
	_, err := p.getAllTorrents(t.Context(), name, cfg, domain.FuzzyMatching{})
	require.NoError(t, err)
}

func newCacheTestClient() *fakeTorrentClient {
	return &fakeTorrentClient{torrents: []torrentclient.Torrent{{Name: cacheTestTorrentName, Hash: "ep1"}}}
}

func TestPruneClientCaches_DropsRemovedClients(t *testing.T) {
	resetProcessorGlobals()
	kept := &domain.Client{Type: "qbittorrent", Host: "qbittorrent"}
	removed := &domain.Client{Type: "deluge-v2", Host: "deluge"}
	removedClient := &closingTorrentClient{fakeTorrentClient: newCacheTestClient()}
	cacheClient(t, "default", kept, newCacheTestClient())
	cacheClient(t, "anime", removed, removedClient)
	keptRelease := requireParsedRelease(t, *kept, cacheTestTorrentName)

	// smartMode is an unrelated setting, so it must not drop the caches of "default".
	dropped, err := pruneClientCaches(domain.Config{SmartMode: true, Clients: map[string]*domain.Client{"default": kept}})
	require.NoError(t, err)
	assert.Equal(t, []string{"anime"}, dropped)

	_, ok := clientMap.Load("anime")
	assert.False(t, ok, "the removed client is dropped")
	_, ok = entryMap.Load("anime")
	assert.False(t, ok, "the inventory of the removed client is dropped")
	_, ok = parsedReleaseMap.Load(parsedNamesKeyFor(*removed))
	assert.False(t, ok, "the parsed names of the unused endpoint are dropped")
	assert.Equal(t, 1, removedClient.closeCalls, "the connection of the removed client is closed")

	_, ok = clientMap.Load("default")
	assert.True(t, ok, "a configured client keeps its connection")
	_, ok = entryMap.Load("default")
	assert.True(t, ok, "a configured client keeps its inventory")
	assert.Same(t, keptRelease.release, requireParsedRelease(t, *kept, cacheTestTorrentName).release,
		"a configured endpoint keeps its parsed names")
}

func TestPruneClientCaches_DropsOldEndpointAfterHostChange(t *testing.T) {
	resetProcessorGlobals()
	old := &domain.Client{Type: "qbittorrent", Host: "http://qui:7476/proxy/old"}
	cacheClient(t, "default", old, newCacheTestClient())
	changed := old.Clone()
	changed.Host = "http://qui:7476/proxy/new"

	dropped, err := pruneClientCaches(domain.Config{Clients: map[string]*domain.Client{"default": &changed}})
	require.NoError(t, err)
	assert.Equal(t, []string{"default"}, dropped)

	_, ok := clientMap.Load("default")
	assert.False(t, ok, "the client of the old host is dropped")
	_, ok = entryMap.Load("default")
	assert.False(t, ok, "the inventory of the old host is dropped")
	_, ok = parsedReleaseMap.Load(parsedNamesKeyFor(*old))
	assert.False(t, ok, "the parsed names of the old host are dropped")
}

func TestPruneClientCaches_DropsParsedNamesOfOldScope(t *testing.T) {
	resetProcessorGlobals()
	hd := &domain.Client{Type: "qbittorrent", Host: "qbittorrent", Inventory: domain.InventoryScope{IncludeCategories: []string{"tv-hd"}}}
	uhd := hd.Clone()
	uhd.Inventory.IncludeCategories = []string{"tv-uhd"}
	cacheClient(t, "sonarr-hd", hd, newCacheTestClient())
	cacheClient(t, "sonarr-uhd", &uhd, newCacheTestClient())
	changed := hd.Clone()
	changed.Inventory.IncludeCategories = []string{"tv-hd", "tv-hd-completed"}

	dropped, err := pruneClientCaches(domain.Config{Clients: map[string]*domain.Client{"sonarr-hd": &changed, "sonarr-uhd": &uhd}})
	require.NoError(t, err)
	assert.Equal(t, []string{"sonarr-hd"}, dropped)

	_, ok := parsedReleaseMap.Load(parsedNamesKeyFor(*hd))
	assert.False(t, ok, "the parsed names of the old scope are dropped")
	_, ok = parsedReleaseMap.Load(parsedNamesKeyFor(uhd))
	assert.True(t, ok, "another scope of the endpoint keeps its parsed names")
}

// TestPruneClientCaches_ScanDuringReloadStoresNoOldScope: a scan that started
// before a reload changed its scope must not store the names of the old scope
// after the clean-up, because no client would use them until a restart.
func TestPruneClientCaches_ScanDuringReloadStoresNoOldScope(t *testing.T) {
	resetProcessorGlobals()
	started, proceed := make(chan struct{}), make(chan struct{})
	torrentClient := &fakeTorrentClient{getTorrents: func(context.Context) ([]torrentclient.Torrent, error) {
		close(started)
		<-proceed
		return []torrentclient.Torrent{{Name: cacheTestTorrentName, Hash: "ep1"}}, nil
	}}
	old := &domain.Client{Type: "qbittorrent", Host: "qbittorrent", Inventory: domain.InventoryScope{IncludeCategories: []string{"tv-hd"}}}
	storeTestClient("default", old, torrentClient)
	changed := old.Clone()
	changed.Inventory.IncludeCategories = []string{"tv-hd", "tv-hd-completed"}
	p := newTestProcessor(torrentClient)
	done := make(chan error, 1)
	go func() {
		_, err := p.getAllTorrents(t.Context(), "default", old, domain.FuzzyMatching{})
		done <- err
	}()
	<-started

	_, err := pruneClientCaches(domain.Config{Clients: map[string]*domain.Client{"default": &changed}})
	require.NoError(t, err)
	close(proceed)
	require.NoError(t, <-done)

	_, ok := parsedReleaseMap.Load(parsedNamesKeyFor(*old))
	assert.False(t, ok, "the scan of the old scope does not store its names after the reload")
}

func TestWatchConfigReloads_PrunesAfterEachReload(t *testing.T) {
	resetProcessorGlobals()
	client := &domain.Client{Type: "qbittorrent", Host: "qbittorrent"}
	cacheClient(t, "default", client, newCacheTestClient())
	cfg := &fakeConfig{config: domain.Config{Clients: map[string]*domain.Client{"default": client}}}
	reloads := make(chan struct{}, 1)
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	log := newTraceLogger(t)
	go func() {
		defer close(done)
		watchConfigReloads(ctx, log, cfg, reloads)
	}()

	cfg.Store(domain.Config{})
	reloads <- struct{}{}
	// The watcher logs after the clean-up, so stop it before reading the log.
	require.Eventually(t, func() bool { return len(reloads) == 0 && !hasCachedClient("default") }, time.Second, time.Millisecond)
	cancel()
	<-done

	_, ok := parsedReleaseMap.Load(parsedNamesKeyFor(*client))
	assert.False(t, ok, "a reload that removes the client drops its parsed names")
	log.Events(t).Require(t, "dropped caches of removed or changed clients")
}

func hasCachedClient(name string) bool {
	_, ok := clientMap.Load(name)
	return ok
}
