// Copyright (c) 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package inventory

import (
	"context"
	"errors"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/nuxencs/seasonpackarr/internal/domain"
	"github.com/nuxencs/seasonpackarr/internal/logger/loggertest"
	"github.com/nuxencs/seasonpackarr/internal/torrentclient"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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

func TestReadAtStartup_ReadsEachClientAndSharesParsedNames(t *testing.T) {
	const name = "Series.S01E01.1080p.WEB-DL-GRP"
	adapter := &fakeAdapter{torrents: []torrentclient.Torrent{{Name: name}}}
	log := loggertest.New()
	store := New((&fakeConnector{adapter: adapter}).connect, log)
	hd := &domain.Client{Type: "qbittorrent", Host: "qbit", Import: domain.ImportPolicy{Category: "tv-hd"}}
	uhd := &domain.Client{Type: "qbittorrent", Host: "qbit", Import: domain.ImportPolicy{Category: "tv-uhd"}}
	cfg := configWith(map[string]*domain.Client{"default": hd, "default-4k": uhd})

	store.ReadAtStartup(t.Context(), &fakeProvider{cfg: cfg})

	assert.Equal(t, 2, adapter.readCount(), "each client has its own inventory")
	first := readInventory(t, newTestClient(t, store, "default", hd, domain.FuzzyMatching{}))
	second := readInventory(t, newTestClient(t, store, "default-4k", uhd, domain.FuzzyMatching{}))
	assert.Equal(t, 2, adapter.readCount(), "the first announce finds the inventory of each client")
	assert.Same(t, releaseOf(t, first, name), releaseOf(t, second, name), "the clients of one endpoint share the parsed names")
	event := log.Events(t).RequireField(t, "client inventory ready", "clientname", "default")
	assert.EqualValues(t, 1, event["titles"])
	assert.Contains(t, event, "duration_ms")
	log.Events(t).RequireField(t, "client inventory ready", "clientname", "default-4k")
}

func TestReadAtStartup_TriesAFailedClientFourTimesWithoutStoppingOthers(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		started := time.Now()
		var readTimes []time.Time
		failing := &fakeAdapter{getTorrents: func(context.Context) ([]torrentclient.Torrent, error) {
			readTimes = append(readTimes, time.Now())
			return nil, errors.New("connection refused")
		}}
		var workingRead time.Time
		working := &fakeAdapter{getTorrents: func(context.Context) ([]torrentclient.Torrent, error) {
			workingRead = time.Now()
			return []torrentclient.Torrent{{Name: "Series.S01E01.1080p.WEB-DL-GRP"}}, nil
		}}
		connector := &fakeConnectorByHost{adapters: map[string]torrentclient.TorrentClient{"down": failing, "up": working}}
		log := loggertest.New()
		store := New(connector.connect, log)
		// "anime" sorts first, so a read in sequence would read "default" after the retries of "anime".
		cfg := configWith(map[string]*domain.Client{"anime": {Host: "down"}, "default": {Host: "up"}})

		store.ReadAtStartup(t.Context(), &fakeProvider{cfg: cfg})

		assert.Equal(t, 30*time.Second, time.Since(started), "the startup read waits 10 seconds between attempts")
		require.Len(t, readTimes, 4, "a failed client is tried four times")
		for i := 1; i < len(readTimes); i++ {
			assert.Equal(t, 10*time.Second, readTimes[i].Sub(readTimes[i-1]))
		}
		assert.Equal(t, 1, working.readCount(), "a ready client is read once")
		assert.Equal(t, started, workingRead, "the startup read reads each endpoint in parallel")
		events := log.Events(t)
		events.RequireField(t, "client inventory ready", "clientname", "default")
		warning := events.RequireField(t, "could not read client inventory at startup", "clientname", "anime")
		assert.Equal(t, "warn", warning["level"])
		assert.Equal(t, "connection refused", warning["error"])
		retry := events.RequireField(t, "client inventory not ready at startup", "clientname", "anime")
		assert.Equal(t, "debug", retry["level"])
		assert.EqualValues(t, 1, retry["attempt"])
	})
}

func TestReadAtStartup_RetriesAConnectionFailure(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		adapter := &fakeAdapter{torrents: []torrentclient.Torrent{{Name: "Series.S01E01.1080p.WEB-DL-GRP"}}}
		connects := 0
		log := loggertest.New()
		store := New(func(context.Context, *domain.Client) (torrentclient.TorrentClient, error) {
			connects++
			if connects == 1 {
				return nil, errors.New("login refused")
			}
			return adapter, nil
		}, log)
		cfg := configWith(map[string]*domain.Client{"default": {Host: "qbit"}})

		store.ReadAtStartup(t.Context(), &fakeProvider{cfg: cfg})

		assert.Equal(t, 2, connects, "a client that starts later is tried again")
		assert.Equal(t, 1, adapter.readCount())
		log.Events(t).RequireField(t, "client inventory ready", "clientname", "default")
	})
}

func TestReadAtStartup_ReadsNoClientThatAReloadRemoved(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		provider := &fakeProvider{}
		adapter := &fakeAdapter{}
		adapter.getTorrents = func(context.Context) ([]torrentclient.Torrent, error) {
			// A reload removes the client while it is not ready.
			provider.store(domain.Config{})
			return nil, errors.New("connection refused")
		}
		provider.store(configWith(map[string]*domain.Client{"default": {Host: "qbit"}}))
		log := loggertest.New()
		store := New((&fakeConnector{adapter: adapter}).connect, log)
		started := time.Now()

		store.ReadAtStartup(t.Context(), provider)

		assert.Equal(t, 1, adapter.readCount(), "a client that the reload removed is not read again")
		assert.Equal(t, 10*time.Second, time.Since(started), "the startup read stops at the next attempt")
		assert.Len(t, log.Events(t), 1, "only the first failure logs")
	})
}

func TestReadAtStartup_StopsWhenTheContextEnds(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		adapter := &fakeAdapter{getTorrents: func(context.Context) ([]torrentclient.Torrent, error) {
			cancel()
			return nil, errors.New("context canceled")
		}}
		log := loggertest.New()
		store := New((&fakeConnector{adapter: adapter}).connect, log)
		cfg := configWith(map[string]*domain.Client{"default": {Host: "qbit"}})
		started := time.Now()

		store.ReadAtStartup(ctx, &fakeProvider{cfg: cfg})

		assert.Zero(t, time.Since(started), "the startup read does not wait for a retry after shutdown")
		assert.Equal(t, 1, adapter.readCount())
		assert.Empty(t, log.Events(t), "a shutdown is not a failure")
	})
}

func TestReadAtStartup_AppliesAReloadToTheNextAttempt(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		provider := &fakeProvider{}
		down := &fakeAdapter{}
		down.getTorrents = func(context.Context) ([]torrentclient.Torrent, error) {
			// A reload moves the client to another host while it is not ready.
			provider.store(configWith(map[string]*domain.Client{"default": {Host: "new"}}))
			return nil, errors.New("connection refused")
		}
		up := &fakeAdapter{torrents: []torrentclient.Torrent{{Name: "Series.S01E01.1080p.WEB-DL-GRP"}}}
		connector := &fakeConnectorByHost{adapters: map[string]torrentclient.TorrentClient{"old": down, "new": up}}
		provider.store(configWith(map[string]*domain.Client{"default": {Host: "old"}}))
		log := loggertest.New()
		store := New(connector.connect, log)

		store.ReadAtStartup(t.Context(), provider)

		assert.Equal(t, 1, down.readCount())
		assert.Equal(t, 1, up.readCount(), "the next attempt uses the reloaded config")
		log.Events(t).RequireField(t, "client inventory ready", "clientname", "default")
	})
}
