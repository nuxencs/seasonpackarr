// Copyright (c) 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package inventory

import (
	"context"
	"errors"
	"slices"
	"sync"
	"testing"
	"testing/synctest"

	"github.com/nuxencs/seasonpackarr/internal/domain"
	"github.com/nuxencs/seasonpackarr/internal/logger/loggertest"
	"github.com/nuxencs/seasonpackarr/internal/torrentclient"

	"github.com/autobrr/rls"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const cleanUpTorrentName = "Series.S01E01.1080p.WEB-DL-GRP"

func newCleanUpAdapter() *fakeAdapter {
	return &fakeAdapter{torrents: []torrentclient.Torrent{{Name: cleanUpTorrentName}}}
}

func TestCleanUp_DropsRemovedClients(t *testing.T) {
	kept := &domain.Client{Type: "qbittorrent", Host: "qbit"}
	removed := &domain.Client{Type: "deluge-v2", Host: "deluge"}
	keptAdapter := newCleanUpAdapter()
	removedAdapter := &fakeClosingAdapter{fakeAdapter: newCleanUpAdapter()}
	connector := &fakeConnectorByHost{adapters: map[string]torrentclient.TorrentClient{"qbit": keptAdapter, "deluge": removedAdapter}}
	store := New(connector.connect, loggertest.New())
	keptRelease := releaseOf(t, readInventory(t, newTestClient(t, store, "default", kept, domain.FuzzyMatching{})), cleanUpTorrentName)
	removedHandle := newTestClient(t, store, "anime", removed, domain.FuzzyMatching{})
	removedRelease := releaseOf(t, readInventory(t, removedHandle), cleanUpTorrentName)

	// smartMode is an unrelated setting, so it must not drop the data of "default".
	dropped, err := store.CleanUp(domain.Config{SmartMode: true, Clients: map[string]*domain.Client{"default": kept}})
	require.NoError(t, err)
	assert.Equal(t, []string{"anime"}, dropped)
	assert.Equal(t, 1, removedAdapter.closeCount(), "the connection of the removed client is closed")

	keptHandle := newTestClient(t, store, "default", kept, domain.FuzzyMatching{})
	assert.Same(t, keptRelease, releaseOf(t, readInventory(t, keptHandle), cleanUpTorrentName))
	assert.Equal(t, 1, connector.connectCount("qbit"), "a configured client keeps its connection")
	assert.Equal(t, 1, keptAdapter.readCount(), "a configured client keeps its inventory")
	store.DiscardClient("default")
	assert.Same(t, keptRelease, releaseOf(t, readInventory(t, keptHandle), cleanUpTorrentName),
		"a configured endpoint keeps its parsed names")

	again := newTestClient(t, store, "anime", removed, domain.FuzzyMatching{})
	assert.Equal(t, 2, connector.connectCount("deluge"), "the connection of the removed client is dropped")
	assert.NotSame(t, removedRelease, releaseOf(t, readInventory(t, again), cleanUpTorrentName),
		"the parsed names of the unused endpoint are dropped")
	assert.Equal(t, 2, removedAdapter.readCount(), "the inventory of the removed client is dropped")
}

func TestCleanUp_DropsOldEndpointAfterHostChange(t *testing.T) {
	adapter := newCleanUpAdapter()
	connector := &fakeConnector{adapter: adapter}
	store := newTestStore(connector)
	old := &domain.Client{Type: "qbittorrent", Host: "http://qui:7476/proxy/old"}
	oldHandle := newTestClient(t, store, "default", old, domain.FuzzyMatching{})
	oldRelease := releaseOf(t, readInventory(t, oldHandle), cleanUpTorrentName)
	changed := old.Clone()
	changed.Host = "http://qui:7476/proxy/new"

	dropped, err := store.CleanUp(configWith(map[string]*domain.Client{"default": &changed}))
	require.NoError(t, err)
	assert.Equal(t, []string{"default"}, dropped)

	assert.NotSame(t, oldRelease, releaseOf(t, readInventory(t, oldHandle), cleanUpTorrentName),
		"the inventory and the parsed names of the old host are dropped")
	newTestClient(t, store, "default", old, domain.FuzzyMatching{})
	assert.Equal(t, 2, connector.connectCount(), "the connection of the old host is dropped")
}

func TestCleanUp_DropsParsedNamesOfOldScope(t *testing.T) {
	const (
		hdName  = "Series.S01E01.1080p.WEB-DL-GRP"
		uhdName = "Series.S01E01.2160p.WEB-DL-GRP"
	)
	hd := &domain.Client{Type: "qbittorrent", Host: "qbit", Inventory: domain.InventoryScope{IncludeCategories: []string{"tv-hd"}}}
	uhd := hd.Clone()
	uhd.Inventory.IncludeCategories = []string{"tv-uhd"}
	adapters := map[string]torrentclient.TorrentClient{
		"tv-hd":  &fakeAdapter{torrents: []torrentclient.Torrent{{Name: hdName}}},
		"tv-uhd": &fakeAdapter{torrents: []torrentclient.Torrent{{Name: uhdName}}},
	}
	store := New(func(_ context.Context, client *domain.Client) (torrentclient.TorrentClient, error) {
		return adapters[client.Inventory.IncludeCategories[0]], nil
	}, loggertest.New())
	hdHandle := newTestClient(t, store, "sonarr-hd", hd, domain.FuzzyMatching{})
	uhdHandle := newTestClient(t, store, "sonarr-uhd", &uhd, domain.FuzzyMatching{})
	hdRelease := releaseOf(t, readInventory(t, hdHandle), hdName)
	uhdRelease := releaseOf(t, readInventory(t, uhdHandle), uhdName)
	changed := hd.Clone()
	changed.Inventory.IncludeCategories = []string{"tv-hd", "tv-hd-completed"}

	dropped, err := store.CleanUp(configWith(map[string]*domain.Client{"sonarr-hd": &changed, "sonarr-uhd": &uhd}))
	require.NoError(t, err)
	assert.Equal(t, []string{"sonarr-hd"}, dropped)

	store.DiscardClient("sonarr-uhd")
	assert.Same(t, uhdRelease, releaseOf(t, readInventory(t, uhdHandle), uhdName), "another scope of the endpoint keeps its parsed names")
	assert.NotSame(t, hdRelease, releaseOf(t, readInventory(t, hdHandle), hdName), "the parsed names of the old scope are dropped")
}

// TestCleanUp_ReadDuringCleanUpAddsNoParsedNames: a read that started before a
// reload changed its scope must not store the names of the old scope after
// the clean-up, because no client would use them until a restart.
func TestCleanUp_ReadDuringCleanUpAddsNoParsedNames(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		started, proceed := make(chan struct{}), make(chan struct{})
		blockFirst := sync.OnceFunc(func() { close(started); <-proceed })
		adapter := &fakeAdapter{getTorrents: func(context.Context) ([]torrentclient.Torrent, error) {
			blockFirst()
			return []torrentclient.Torrent{{Name: cleanUpTorrentName}}, nil
		}}
		store := newTestStore(&fakeConnector{adapter: adapter})
		old := &domain.Client{Type: "qbittorrent", Host: "qbit", Inventory: domain.InventoryScope{IncludeCategories: []string{"tv-hd"}}}
		handle := newTestClient(t, store, "default", old, domain.FuzzyMatching{})
		changed := old.Clone()
		changed.Inventory.IncludeCategories = []string{"tv-hd", "tv-hd-completed"}

		overlapped := make(chan *Inventory, 1)
		go func() {
			inv, err := handle.Inventory(t.Context())
			assert.NoError(t, err)
			overlapped <- inv
		}()
		<-started
		_, err := store.CleanUp(configWith(map[string]*domain.Client{"default": &changed}))
		require.NoError(t, err)
		close(proceed)
		inv := <-overlapped
		require.NotNil(t, inv, "the read returns its result to its caller")

		assert.NotSame(t, releaseOf(t, inv, cleanUpTorrentName), releaseOf(t, readInventory(t, handle), cleanUpTorrentName),
			"the read of the old scope does not store its names after the clean-up")
	})
}

func TestCleanUp_KeepsAnInventoryThatARequestStoredMeanwhile(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		started, proceed := make(chan struct{}), make(chan struct{})
		blockSecond := sync.OnceFunc(func() { close(started); <-proceed })
		adapter := &fakeAdapter{}
		adapter.getTorrents = func(context.Context) ([]torrentclient.Torrent, error) {
			if adapter.readCount() == 2 {
				blockSecond()
			}
			return []torrentclient.Torrent{{Name: cleanUpTorrentName}}, nil
		}
		connector := &fakeConnector{adapter: adapter}
		store := newTestStore(connector)
		old := &domain.Client{Type: "qbittorrent", Host: "qbit", Import: domain.ImportPolicy{Category: "tv"}}
		readInventory(t, newTestClient(t, store, "default", old, domain.FuzzyMatching{}))
		changed := old.Clone()
		changed.Import.Category = "tv-hd"
		newCfg := configWith(map[string]*domain.Client{"default": &changed})
		handle, err := store.Client(t.Context(), "default", newCfg)
		require.NoError(t, err)

		stored := make(chan *Inventory, 1)
		go func() {
			inv, err := handle.Inventory(t.Context())
			assert.NoError(t, err)
			stored <- inv
		}()
		<-started
		dropped, err := store.CleanUp(newCfg)
		require.NoError(t, err)
		close(proceed)
		inv := <-stored

		assert.Empty(t, dropped, "the request already connected with the new config")
		assert.Same(t, inv, readInventory(t, handle), "the clean-up keeps the inventory of the new config")
		assert.Equal(t, 2, adapter.readCount())
		assert.Equal(t, 2, connector.connectCount())
	})
}

func TestCleanUp_ClosesDroppedAdaptersThatRequestsStillUse(t *testing.T) {
	closing := &fakeClosingAdapter{fakeAdapter: newCleanUpAdapter()}
	failing := &fakeClosingAdapter{fakeAdapter: newCleanUpAdapter(), closeErr: errors.New("daemon gone")}
	plain := newCleanUpAdapter()
	connector := &fakeConnectorByHost{adapters: map[string]torrentclient.TorrentClient{"b": closing, "c": failing, "a": plain}}
	store := New(connector.connect, loggertest.New())
	handles := map[string]*Client{}
	for _, name := range []string{"b", "c", "a"} {
		handles[name] = newTestClient(t, store, name, &domain.Client{Host: name}, domain.FuzzyMatching{})
	}

	dropped, err := store.CleanUp(domain.Config{})
	assert.Equal(t, []string{"a", "b", "c"}, dropped, "the dropped names are sorted")
	require.ErrorContains(t, err, "daemon gone", "the clean-up returns the close error")
	assert.Equal(t, 1, closing.closeCount())
	assert.Equal(t, 1, failing.closeCount(), "a close error does not stop the other closes")

	assert.Same(t, closing, handles["b"].Adapter(), "a request keeps its adapter")
	assert.Len(t, slices.Collect(readInventory(t, handles["b"]).SameTitle(new(rls.ParseString(cleanUpTorrentName)))), 1, "a request that holds a closed adapter still reads")
}
