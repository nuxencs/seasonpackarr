// Copyright (c) 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package state

import (
	"fmt"
	"testing"
	"time"

	"github.com/nuxencs/seasonpackarr/internal/prowlarr"

	"github.com/stretchr/testify/require"
)

func TestTorrentCache_ExpiryIdentityAndEviction(t *testing.T) {
	s, _ := newTestStore(t)
	ctx := t.Context()
	now := time.Now()
	key := TorrentCacheKey(1, prowlarr.Release{GUID: "pack", Title: "show"})
	get := func(key Key, at time.Time) []byte {
		data, err := s.CachedTorrent(ctx, key, at)
		require.NoError(t, err)
		return data
	}
	require.NoError(t, s.PutCachedTorrent(ctx, key, []byte("torrent"), now))
	require.Equal(t, []byte("torrent"), get(key, now.Add(time.Hour)))
	require.Nil(t, get(TorrentCacheKey(2, prowlarr.Release{GUID: "pack", Title: "show"}), now))
	require.Nil(t, get(TorrentCacheKey(1, prowlarr.Release{GUID: "pack", Title: "other"}), now))
	require.Nil(t, get(key, now.Add(torrentCacheTTL)))
	require.NoError(t, s.PutCachedTorrent(ctx, TorrentCacheKey(1, prowlarr.Release{Title: "show"}), []byte("no identity"), now))
	var count int
	require.NoError(t, s.db.QueryRow("SELECT count(*) FROM metadata").Scan(&count))
	require.Zero(t, count)
	for i := range torrentCacheMaxEntries {
		require.NoError(t, s.PutCachedTorrent(ctx, TorrentCacheKey(1, prowlarr.Release{GUID: fmt.Sprint(i)}), []byte{1}, now.Add(time.Duration(i))))
	}
	require.NotNil(t, get(TorrentCacheKey(1, prowlarr.Release{GUID: "0"}), now.Add(time.Second)))
	require.NoError(t, s.PutCachedTorrent(ctx, key, []byte("torrent"), now.Add(2*time.Second)))
	require.NoError(t, s.db.QueryRow("SELECT count(*) FROM metadata").Scan(&count))
	require.Equal(t, torrentCacheMaxEntries, count)
	require.Nil(t, get(TorrentCacheKey(1, prowlarr.Release{GUID: "1"}), now.Add(3*time.Second)))
	require.NotNil(t, get(TorrentCacheKey(1, prowlarr.Release{GUID: "0"}), now.Add(3*time.Second)))
}

func TestTorrentCache_ByteBudget(t *testing.T) {
	s, _ := newTestStore(t)
	ctx := t.Context()
	now := time.Now()
	first, second := TorrentCacheKey(1, prowlarr.Release{GUID: "first"}), TorrentCacheKey(1, prowlarr.Release{GUID: "second"})
	require.NoError(t, s.PutCachedTorrent(ctx, first, make([]byte, torrentCacheMaxBytes), now))
	require.NoError(t, s.PutCachedTorrent(ctx, second, []byte{1}, now.Add(time.Second)))
	data, err := s.CachedTorrent(ctx, first, now.Add(2*time.Second))
	require.NoError(t, err)
	require.Nil(t, data)
	data, err = s.CachedTorrent(ctx, second, now.Add(2*time.Second))
	require.NoError(t, err)
	require.Equal(t, []byte{1}, data)
	var size int
	require.NoError(t, s.db.QueryRow("SELECT sum(length(data)) FROM metadata").Scan(&size))
	require.Equal(t, 1, size)
}
