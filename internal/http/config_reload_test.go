// Copyright (c) 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package http

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nuxencs/seasonpackarr/internal/domain"
	"github.com/nuxencs/seasonpackarr/internal/torrentclient"

	"github.com/stretchr/testify/require"
)

// fakeClosingClient records Close like the Deluge adapter, which holds a daemon connection.
type fakeClosingClient struct {
	*fakeTorrentClient
	closeCalls atomic.Int32
}

func (f *fakeClosingClient) Close() error {
	f.closeCalls.Add(1)
	return nil
}

func TestWatchConfigReloads_CleansUpAfterEachReload(t *testing.T) {
	adapter := &fakeClosingClient{fakeTorrentClient: &fakeTorrentClient{
		torrents: []torrentclient.Torrent{{Name: "Series.S01E01.1080p.WEB-DL-GRP", Hash: "ep1"}},
	}}
	inv := newTestInventory(adapter)
	client := &domain.Client{Type: "qbittorrent", Host: "qbittorrent"}
	cfg := &fakeConfig{config: domain.Config{Clients: map[string]*domain.Client{"default": client}}}
	_, err := inv.Client(t.Context(), "default", cfg.Snapshot())
	require.NoError(t, err)
	reloads := make(chan struct{})
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	log := newTraceLogger(t)
	go func() {
		defer close(done)
		watchConfigReloads(ctx, log, cfg, inv, reloads)
	}()

	// The first reload removes the client. The second removes the connection
	// that a request made in the meantime with the old config.
	removed := cfg.Snapshot()
	cfg.Store(domain.Config{})
	reloads <- struct{}{}
	require.Eventually(t, func() bool { return adapter.closeCalls.Load() == 1 }, time.Second, time.Millisecond)
	_, err = inv.Client(t.Context(), "default", removed)
	require.NoError(t, err)
	reloads <- struct{}{}
	require.Eventually(t, func() bool { return adapter.closeCalls.Load() == 2 }, time.Second, time.Millisecond)
	// The watcher logs after the clean-up, so stop it before reading the log.
	cancel()
	<-done

	event := log.Events(t).Require(t, "dropped caches of removed or changed clients")
	require.Equal(t, []any{"default"}, event["clients"])
}
