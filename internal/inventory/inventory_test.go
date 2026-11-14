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
	"time"

	"github.com/nuxencs/seasonpackarr/internal/domain"
	"github.com/nuxencs/seasonpackarr/internal/logger/loggertest"
	"github.com/nuxencs/seasonpackarr/internal/torrentclient"

	"github.com/autobrr/rls"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeAdapter is a torrent client adapter. The module calls only GetTorrents.
type fakeAdapter struct {
	torrentclient.TorrentClient

	mu          sync.Mutex
	torrents    []torrentclient.Torrent
	getTorrents func(context.Context) ([]torrentclient.Torrent, error)
	reads       int
}

func (f *fakeAdapter) GetTorrents(ctx context.Context) ([]torrentclient.Torrent, error) {
	f.mu.Lock()
	f.reads++
	getTorrents, torrents := f.getTorrents, f.torrents
	f.mu.Unlock()
	if getTorrents != nil {
		return getTorrents(ctx)
	}
	return torrents, nil
}

func (f *fakeAdapter) readCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.reads
}

// fakeConnector returns adapter for each connect and counts the connects.
type fakeConnector struct {
	mu       sync.Mutex
	adapter  torrentclient.TorrentClient
	err      error
	connects int
}

func (f *fakeConnector) connect(context.Context, *domain.Client) (torrentclient.TorrentClient, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.connects++
	if f.err != nil {
		return nil, f.err
	}
	return f.adapter, nil
}

func (f *fakeConnector) connectCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.connects
}

func newTestStore(connector *fakeConnector) *Store {
	return New(connector.connect, loggertest.New())
}

func configWith(clients map[string]*domain.Client) domain.Config {
	return domain.Config{Clients: clients}
}

func TestStore_ReusesConnectionOfUnchangedConfig(t *testing.T) {
	connector := &fakeConnector{adapter: &fakeAdapter{}}
	store := newTestStore(connector)
	client := &domain.Client{Type: "qbittorrent", Host: "qbit", Import: domain.ImportPolicy{Tags: []string{"tv"}}}
	cfg := configWith(map[string]*domain.Client{"default": client})

	first, err := store.Client(t.Context(), "default", cfg)
	require.NoError(t, err)
	unchanged := client.Clone()
	second, err := store.Client(t.Context(), "default", configWith(map[string]*domain.Client{"default": &unchanged}))
	require.NoError(t, err)
	assert.Same(t, connector.adapter, first.Adapter())
	assert.Same(t, first.Adapter(), second.Adapter())
	assert.Equal(t, 1, connector.connectCount(), "an unchanged config uses its connection again")

	changed := client.Clone()
	changed.Import.Tags = []string{"tv", "4k"}
	_, err = store.Client(t.Context(), "default", configWith(map[string]*domain.Client{"default": &changed}))
	require.NoError(t, err)
	assert.Equal(t, 2, connector.connectCount(), "a changed config connects again")
}

func newTestClient(t *testing.T, store *Store, name string, client *domain.Client, fuzzy domain.FuzzyMatching) *Client {
	t.Helper()
	cfg := configWith(map[string]*domain.Client{name: client})
	cfg.FuzzyMatching = fuzzy
	handle, err := store.Client(t.Context(), name, cfg)
	require.NoError(t, err)
	return handle
}

func readInventory(t *testing.T, client *Client) *Inventory {
	t.Helper()
	inv, err := client.Inventory(t.Context())
	require.NoError(t, err)
	return inv
}

func torrentNames(entries []Entry) []string {
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Torrent.Name)
	}
	return names
}

func TestInventory_SameTitleReturnsEntriesOfTheReleaseTitle(t *testing.T) {
	adapter := &fakeAdapter{torrents: []torrentclient.Torrent{
		{Name: "Series.S01E01.1080p.WEB-DL-GRP", Hash: "a"},
		{Name: "Series.S01E02.2160p.WEB-DL-OTHER", Hash: "b"},
		{Name: "Other.Show.S01E01.1080p.WEB-DL-GRP", Hash: "c"},
	}}
	store := newTestStore(&fakeConnector{adapter: adapter})
	inv := readInventory(t, newTestClient(t, store, "default", &domain.Client{}, domain.FuzzyMatching{}))

	same := inv.SameTitle(rls.ParseString("Series.S01.1080p.WEB-DL-GRP"))
	assert.ElementsMatch(t, []string{"Series.S01E01.1080p.WEB-DL-GRP", "Series.S01E02.2160p.WEB-DL-OTHER"}, torrentNames(same))
	for _, e := range same {
		assert.Equal(t, "Series", e.Release.Title, "an entry holds the parsed release of its torrent")
	}
	assert.Empty(t, inv.SameTitle(rls.ParseString("Missing.S01.1080p.WEB-DL-GRP")))
}

func TestInventory_CallerCannotChangeIt(t *testing.T) {
	adapter := &fakeAdapter{torrents: []torrentclient.Torrent{{Name: "Series.S01E01.1080p.WEB-DL-GRP"}}}
	store := newTestStore(&fakeConnector{adapter: adapter})
	client := newTestClient(t, store, "default", &domain.Client{}, domain.FuzzyMatching{})
	request := rls.ParseString("Series.S01.1080p.WEB-DL-GRP")

	same := readInventory(t, client).SameTitle(request)
	require.Len(t, same, 1)
	same[0] = Entry{}
	_ = append(same[:0], Entry{})
	for group := range readInventory(t, client).TitleGroups() {
		group[0] = Entry{}
	}

	again := readInventory(t, client).SameTitle(request)
	require.Len(t, again, 1)
	assert.Equal(t, "Series.S01E01.1080p.WEB-DL-GRP", again[0].Torrent.Name)
	assert.Equal(t, 1, adapter.readCount(), "the reads after the first come from the stored inventory")
}

func TestStore_SeparatesConnectionAndReadFailures(t *testing.T) {
	connectErr, readErr := errors.New("login refused"), errors.New("timeout")
	cfg := configWith(map[string]*domain.Client{"default": {}})

	_, err := newTestStore(&fakeConnector{err: connectErr}).Client(t.Context(), "default", cfg)
	require.ErrorIs(t, err, connectErr)
	assert.EqualError(t, err, "failed to create torrent client: login refused")

	_, err = newTestStore(&fakeConnector{}).Client(t.Context(), "missing", cfg)
	require.ErrorIs(t, err, ErrUnknownClient)

	adapter := &fakeAdapter{getTorrents: func(context.Context) ([]torrentclient.Torrent, error) { return nil, readErr }}
	client, err := newTestStore(&fakeConnector{adapter: adapter}).Client(t.Context(), "default", cfg)
	require.NoError(t, err, "a read failure is not a connection failure")
	_, err = client.Inventory(t.Context())
	require.ErrorIs(t, err, readErr)
}

func TestInventory_GroupsEntriesByTitle(t *testing.T) {
	adapter := &fakeAdapter{torrents: []torrentclient.Torrent{
		{Name: "Series.S01E01.1080p.WEB-DL-GRP"},
		{Name: "Series.S01E02.1080p.WEB-DL-GRP"},
		{Name: "Other.Show.S01E01.1080p.WEB-DL-GRP"},
	}}
	store := newTestStore(&fakeConnector{adapter: adapter})
	inv := readInventory(t, newTestClient(t, store, "default", &domain.Client{}, domain.FuzzyMatching{}))

	var groups [][]string
	for group := range inv.TitleGroups() {
		names := torrentNames(group)
		slices.Sort(names)
		groups = append(groups, names)
	}
	assert.ElementsMatch(t, [][]string{
		{"Series.S01E01.1080p.WEB-DL-GRP", "Series.S01E02.1080p.WEB-DL-GRP"},
		{"Other.Show.S01E01.1080p.WEB-DL-GRP"},
	}, groups)
}

func TestInventory_IsValidFor30Seconds(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		adapter := &fakeAdapter{torrents: []torrentclient.Torrent{{Name: "Series.S01E01.1080p.WEB-DL-GRP"}}}
		store := newTestStore(&fakeConnector{adapter: adapter})
		client := newTestClient(t, store, "default", &domain.Client{}, domain.FuzzyMatching{})

		first := readInventory(t, client)
		time.Sleep(30*time.Second - time.Nanosecond)
		assert.Same(t, first, readInventory(t, client), "the inventory is valid before 30 seconds")
		assert.Equal(t, 1, adapter.readCount())

		time.Sleep(time.Nanosecond)
		assert.NotSame(t, first, readInventory(t, client), "the inventory expires after 30 seconds")
		assert.Equal(t, 2, adapter.readCount())
	})
}

func TestInventory_ChangedClientConfigOrFuzzyMatchingReadsAgain(t *testing.T) {
	adapter := &fakeAdapter{torrents: []torrentclient.Torrent{{Name: "Series.S01E01.1080p.WEB-DL-GRP"}}}
	store := newTestStore(&fakeConnector{adapter: adapter})
	client := &domain.Client{Type: "qbittorrent", Import: domain.ImportPolicy{Category: "one"}}

	readInventory(t, newTestClient(t, store, "default", client, domain.FuzzyMatching{}))
	unchanged := client.Clone()
	readInventory(t, newTestClient(t, store, "default", &unchanged, domain.FuzzyMatching{}))
	require.Equal(t, 1, adapter.readCount(), "an unchanged config uses the stored inventory")

	changed := client.Clone()
	changed.Import.Category = "two"
	readInventory(t, newTestClient(t, store, "default", &changed, domain.FuzzyMatching{}))
	require.Equal(t, 2, adapter.readCount(), "a changed client config reads again")

	readInventory(t, newTestClient(t, store, "default", &changed, domain.FuzzyMatching{SkipYearCompare: true}))
	require.Equal(t, 3, adapter.readCount(), "changed fuzzy matching settings read again")
}

func TestInventory_OneReadAtATimeForEachClient(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		proceed := make(chan struct{})
		adapter := &fakeAdapter{getTorrents: func(context.Context) ([]torrentclient.Torrent, error) {
			<-proceed
			return []torrentclient.Torrent{{Name: "Series.S01E01.1080p.WEB-DL-GRP"}}, nil
		}}
		otherAdapter := &fakeAdapter{}
		store := New(func(_ context.Context, client *domain.Client) (torrentclient.TorrentClient, error) {
			if client.Host == "other" {
				return otherAdapter, nil
			}
			return adapter, nil
		}, loggertest.New())
		client := newTestClient(t, store, "default", &domain.Client{}, domain.FuzzyMatching{})
		other := newTestClient(t, store, "other", &domain.Client{Host: "other"}, domain.FuzzyMatching{})

		results := make([]*Inventory, 3)
		var wg sync.WaitGroup
		for i := range results {
			wg.Go(func() {
				inv, err := client.Inventory(t.Context())
				assert.NoError(t, err)
				results[i] = inv
			})
		}
		synctest.Wait()
		assert.Equal(t, 1, adapter.readCount(), "the other callers wait for the first read")
		readInventory(t, other)
		assert.Equal(t, 1, otherAdapter.readCount(), "another client reads during the read of this client")

		close(proceed)
		wg.Wait()
		assert.Equal(t, 1, adapter.readCount())
		require.NotNil(t, results[0])
		assert.Same(t, results[0], results[1], "the callers that wait get the same result")
		assert.Same(t, results[0], results[2], "the callers that wait get the same result")
	})
}

func TestInventory_DiscardDuringReadStoresNoResult(t *testing.T) {
	const name = "Series.S01E01.1080p.WEB-DL-GRP"
	tests := map[string]func(*Store){
		"client":   func(s *Store) { s.DiscardClient("default") },
		"endpoint": func(s *Store) { s.DiscardEndpoint(torrentclient.EndpointOf(domain.Client{Host: "qbit"})) },
	}
	for discard, run := range tests {
		t.Run(discard, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				started, proceed := make(chan struct{}), make(chan struct{})
				blockFirst := sync.OnceFunc(func() { close(started); <-proceed })
				adapter := &fakeAdapter{getTorrents: func(context.Context) ([]torrentclient.Torrent, error) {
					blockFirst()
					return []torrentclient.Torrent{{Name: name}}, nil
				}}
				store := newTestStore(&fakeConnector{adapter: adapter})
				client := newTestClient(t, store, "default", &domain.Client{Host: "qbit"}, domain.FuzzyMatching{})

				read := func(done chan<- *Inventory) {
					inv, err := client.Inventory(t.Context())
					assert.NoError(t, err)
					done <- inv
				}
				overlapped, waiter := make(chan *Inventory, 1), make(chan *Inventory, 1)
				go read(overlapped)
				<-started
				go read(waiter)
				synctest.Wait()
				run(store)
				close(proceed)

				inv := <-overlapped
				require.NotNil(t, inv)
				assert.Len(t, inv.SameTitle(rls.ParseString(name)), 1, "the caller gets the result of its read")
				waited := <-waiter
				assert.NotSame(t, inv, waited, "a caller that waited gets no inventory from before the discard")
				assert.Equal(t, 2, adapter.readCount(), "the caller that waited read again")
				assert.Same(t, waited, readInventory(t, client), "the read after the discard stored its inventory")
				assert.Equal(t, 2, adapter.readCount())
			})
		})
	}
}

func TestInventory_DiscardEndpointKeepsOtherEndpoints(t *testing.T) {
	adapter := &fakeAdapter{torrents: []torrentclient.Torrent{{Name: "Series.S01E01.1080p.WEB-DL-GRP"}}}
	store := newTestStore(&fakeConnector{adapter: adapter})
	hd := newTestClient(t, store, "hd", &domain.Client{Host: "qbit", Import: domain.ImportPolicy{Category: "hd"}}, domain.FuzzyMatching{})
	alias := newTestClient(t, store, "uhd", &domain.Client{Type: "qbittorrent", Host: "qbit", Import: domain.ImportPolicy{Category: "uhd"}}, domain.FuzzyMatching{})
	other := newTestClient(t, store, "other", &domain.Client{Host: "other"}, domain.FuzzyMatching{})
	for _, client := range []*Client{hd, alias, other} {
		readInventory(t, client)
	}
	require.Equal(t, 3, adapter.readCount())

	store.DiscardEndpoint(torrentclient.EndpointOf(domain.Client{Host: "qbit"}))
	for _, client := range []*Client{hd, alias, other} {
		readInventory(t, client)
	}
	assert.Equal(t, 5, adapter.readCount(), "a discard drops each client on the endpoint, also with a type alias")

	store.DiscardClient("hd")
	for _, client := range []*Client{hd, alias, other} {
		readInventory(t, client)
	}
	assert.Equal(t, 6, adapter.readCount(), "a discard of one client keeps the other clients")
}

// releaseOf returns the parsed release of the torrent name in inv.
func releaseOf(t *testing.T, inv *Inventory, name string) *rls.Release {
	t.Helper()
	for _, e := range inv.SameTitle(rls.ParseString(name)) {
		if e.Torrent.Name == name {
			return e.Release
		}
	}
	require.Failf(t, "torrent missing from the inventory", "%s", name)
	return nil
}

func TestInventory_ParsedNamesSurviveADiscard(t *testing.T) {
	const name = "Series.S01E01.1080p.WEB-DL-GRP"
	adapter := &fakeAdapter{torrents: []torrentclient.Torrent{{Name: name}}}
	store := newTestStore(&fakeConnector{adapter: adapter})
	client := &domain.Client{Host: "qbit", Import: domain.ImportPolicy{Category: "hd"}}
	alias := client.Clone()
	alias.Import.Category = "uhd"
	handle := newTestClient(t, store, "hd", client, domain.FuzzyMatching{})

	original := releaseOf(t, readInventory(t, handle), name)
	store.DiscardEndpoint(torrentclient.EndpointOf(*client))
	assert.Same(t, original, releaseOf(t, readInventory(t, handle), name), "the read after a discard parses no known name")
	assert.Equal(t, 2, adapter.readCount(), "the discard dropped the inventory")

	store.DiscardClient("hd")
	aliasHandle := newTestClient(t, store, "uhd", &alias, domain.FuzzyMatching{})
	assert.Same(t, original, releaseOf(t, readInventory(t, aliasHandle), name),
		"a client of the same endpoint and scope reuses the parsed names")

	fuzzy := newTestClient(t, store, "hd", client, domain.FuzzyMatching{SkipYearCompare: true})
	assert.NotSame(t, original, releaseOf(t, readInventory(t, fuzzy), name), "changed fuzzy matching parses the names again")
}

func TestInventory_NameInTwoScopesOfOneEndpointIsParsedOnce(t *testing.T) {
	const (
		hdName     = "Series.S01E01.1080p.WEB-DL-GRP"
		uhdName    = "Series.S01E01.2160p.WEB-DL-GRP"
		sharedName = "Other.S01E01.1080p.WEB-DL-GRP"
	)
	hd := &domain.Client{
		Type: "qbittorrent", Host: "qbit",
		Inventory: domain.InventoryScope{IncludeCategories: []string{"tv-hd", "cross-seed"}},
	}
	uhd := hd.Clone()
	uhd.Inventory.IncludeCategories = []string{"tv-uhd", "cross-seed"}
	hdAdapter := &fakeAdapter{torrents: []torrentclient.Torrent{{Name: hdName}, {Name: sharedName}}}
	uhdAdapter := &fakeAdapter{torrents: []torrentclient.Torrent{{Name: uhdName}, {Name: sharedName}}}
	adapters := map[string]torrentclient.TorrentClient{"tv-hd": hdAdapter, "tv-uhd": uhdAdapter}
	store := New(func(_ context.Context, client *domain.Client) (torrentclient.TorrentClient, error) {
		return adapters[client.Inventory.IncludeCategories[0]], nil
	}, loggertest.New())
	hdClient := newTestClient(t, store, "sonarr-hd", hd, domain.FuzzyMatching{})
	uhdClient := newTestClient(t, store, "sonarr-uhd", &uhd, domain.FuzzyMatching{})

	hdInventory := readInventory(t, hdClient)
	uhdInventory := readInventory(t, uhdClient)
	assert.Same(t, releaseOf(t, hdInventory, sharedName), releaseOf(t, uhdInventory, sharedName), "a name in both scopes is parsed once")

	store.DiscardClient("sonarr-hd")
	assert.Same(t, releaseOf(t, hdInventory, hdName), releaseOf(t, readInventory(t, hdClient), hdName),
		"the read of another scope keeps the parsed names of this scope")
}

func TestInventory_NameThatRepeatsInOneScanIsParsedOnce(t *testing.T) {
	const name = "Series.S01E01.1080p.WEB-DL-GRP"
	adapter := &fakeAdapter{torrents: []torrentclient.Torrent{{Name: name, Hash: "a"}, {Name: name, Hash: "b"}}}
	store := newTestStore(&fakeConnector{adapter: adapter})

	crossSeeds := readInventory(t, newTestClient(t, store, "default", &domain.Client{}, domain.FuzzyMatching{})).SameTitle(rls.ParseString(name))
	require.Len(t, crossSeeds, 2)
	assert.Same(t, crossSeeds[0].Release, crossSeeds[1].Release)
}
