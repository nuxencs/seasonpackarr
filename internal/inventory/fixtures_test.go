// Copyright (c) 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package inventory

import (
	"context"
	"sync"
	"testing"

	"github.com/nuxencs/seasonpackarr/internal/domain"
	"github.com/nuxencs/seasonpackarr/internal/logger/loggertest"
	"github.com/nuxencs/seasonpackarr/internal/release"
	"github.com/nuxencs/seasonpackarr/internal/torrentclient"

	"github.com/autobrr/rls"
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

// seasonOf returns the season key of a torrent name for a client with no fuzzy matching.
func seasonOf(name string) release.SeasonKey {
	return release.SeasonKeyOf(new(rls.ParseString(name)), domain.FuzzyMatching{})
}

// releaseOf returns the parsed release of the torrent name in inv. It reads
// each group, so it works for each fuzzy matching setting of the read.
func releaseOf(t *testing.T, inv *Inventory, name string) *rls.Release {
	t.Helper()
	for group := range inv.SeasonGroups() {
		for e := range group {
			if e.Torrent.Name == name {
				return e.Release
			}
		}
	}
	require.Failf(t, "torrent missing from the inventory", "%s", name)
	return nil
}

// fakeClosingAdapter records Close like the Deluge adapter, which holds a daemon
// connection and connects again on its next call.
type fakeClosingAdapter struct {
	*fakeAdapter

	mu       sync.Mutex
	closeErr error
	closes   int
}

func (f *fakeClosingAdapter) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.closes++
	return f.closeErr
}

func (f *fakeClosingAdapter) closeCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.closes
}

// fakeConnectorByHost returns the adapter of the client host and counts the connects.
type fakeConnectorByHost struct {
	mu       sync.Mutex
	adapters map[string]torrentclient.TorrentClient
	connects map[string]int
}

func (f *fakeConnectorByHost) connect(_ context.Context, client *domain.Client) (torrentclient.TorrentClient, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.connects == nil {
		f.connects = make(map[string]int)
	}
	f.connects[client.Host]++
	return f.adapters[client.Host], nil
}

func (f *fakeConnectorByHost) connectCount(host string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.connects[host]
}

// fakeProvider is a config provider whose snapshot a test can replace.
type fakeProvider struct {
	mu  sync.Mutex
	cfg domain.Config
}

func (f *fakeProvider) Snapshot() domain.Config {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.cfg
}

func (f *fakeProvider) store(cfg domain.Config) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.cfg = cfg
}
