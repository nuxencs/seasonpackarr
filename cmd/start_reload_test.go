// Copyright (c) 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package cmd

import (
	"context"
	"testing"

	"github.com/nuxencs/seasonpackarr/internal/domain"
	"github.com/nuxencs/seasonpackarr/internal/inventory"
	"github.com/nuxencs/seasonpackarr/internal/logger/loggertest"
	"github.com/nuxencs/seasonpackarr/internal/torrentclient"
	"github.com/nuxencs/seasonpackarr/internal/torrentclient/torrentclienttest"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeClosingClient records Close like the Deluge adapter, which holds a daemon connection.
type fakeClosingClient struct {
	*torrentclienttest.Client
	closeCalls int
}

func (f *fakeClosingClient) Close() error {
	f.closeCalls++
	return nil
}

// fakeLevelLogger records each log level change as an event, so a test sees it in order with the other events.
type fakeLevelLogger struct{ *loggertest.Logger }

func (f fakeLevelLogger) SetLogLevel(level string) { f.Info().Str("level", level).Msg("log level set") }

func TestReloadParties_SetsLevelThenWarnsThenCleansUp(t *testing.T) {
	previous := zerolog.GlobalLevel()
	zerolog.SetGlobalLevel(zerolog.TraceLevel)
	t.Cleanup(func() { zerolog.SetGlobalLevel(previous) })
	log := fakeLevelLogger{loggertest.New()}
	inv := inventory.New(func(context.Context, *domain.Client) (torrentclient.TorrentClient, error) {
		return &torrentclienttest.Client{}, nil
	}, log)
	_, err := inv.Client(t.Context(), "removed", domain.Config{Clients: map[string]*domain.Client{"removed": {Type: "qbittorrent", Host: "qbittorrent"}}})
	require.NoError(t, err)
	reloaded := domain.Config{LogLevel: "ERROR", Clients: map[string]*domain.Client{"default": {
		Type:      "qbittorrent",
		Host:      "qbittorrent",
		Inventory: domain.InventoryScope{IncludeCategories: []string{"tv-completed"}},
		Import:    domain.ImportPolicy{Category: "tv"},
	}}}

	for _, party := range reloadParties(log, inv) {
		party(reloaded)
	}

	var messages []string
	for _, event := range log.Events(t) {
		messages = append(messages, event["message"].(string))
	}
	require.Len(t, messages, 3)
	assert.Equal(t, "log level set", messages[0])
	assert.Contains(t, messages[1], "inventory scope does not select new packs")
	assert.Equal(t, "dropped caches of removed or changed clients", messages[2])
	log.Events(t).RequireField(t, "log level set", "level", "ERROR")
}

func TestCleanUpInventory_DropsRemovedClientAfterEachReload(t *testing.T) {
	previous := zerolog.GlobalLevel()
	zerolog.SetGlobalLevel(zerolog.TraceLevel)
	t.Cleanup(func() { zerolog.SetGlobalLevel(previous) })
	log := loggertest.New()
	adapter := &fakeClosingClient{Client: &torrentclienttest.Client{
		Torrents: []torrentclient.Torrent{{Name: "Series.S01E01.1080p.WEB-DL-GRP", Hash: "ep1"}},
	}}
	inv := inventory.New(func(context.Context, *domain.Client) (torrentclient.TorrentClient, error) {
		return adapter, nil
	}, log)
	removed := domain.Config{Clients: map[string]*domain.Client{"default": {Type: "qbittorrent", Host: "qbittorrent"}}}
	_, err := inv.Client(t.Context(), "default", removed)
	require.NoError(t, err)
	cleanUp := cleanUpInventory(log, inv)

	// The first reload removes the client. The second removes the connection
	// that a request made in the meantime with the old config.
	cleanUp(domain.Config{})
	require.Equal(t, 1, adapter.closeCalls)
	_, err = inv.Client(t.Context(), "default", removed)
	require.NoError(t, err)
	cleanUp(domain.Config{})
	require.Equal(t, 2, adapter.closeCalls)

	event := log.Events(t).Require(t, "dropped caches of removed or changed clients")
	require.Equal(t, []any{"default"}, event["clients"])
}
