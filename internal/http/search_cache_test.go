// Copyright (c) 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package http

import (
	"fmt"
	"testing"
	"time"

	"github.com/nuxencs/seasonpackarr/internal/prowlarr"
	"github.com/stretchr/testify/require"
)

func TestSearchMetadataCache_ExpiryIdentityAndEviction(t *testing.T) {
	now := time.Now()
	key := metadataKey(1, prowlarr.Result{GUID: "pack", Title: "show"})
	var cache searchMetadataCache
	cache.put(key, []byte("torrent"), now)
	require.Equal(t, []byte("torrent"), cache.get(key, now.Add(time.Hour)))
	require.Nil(t, cache.get(metadataKey(2, prowlarr.Result{GUID: "pack", Title: "show"}), now))
	require.Nil(t, cache.get(key, now.Add(searchMetadataTTL)))
	require.Zero(t, cache.bytes)
	cache.put(metadataKey(1, prowlarr.Result{Title: "show"}), []byte("no identity"), now)
	require.Empty(t, cache.entries)
	for i := range searchMetadataMaxEntries {
		cache.put(metadataKey(1, prowlarr.Result{GUID: fmt.Sprint(i)}), []byte{1}, now.Add(time.Duration(i)))
	}
	// Touch the oldest, then add an entry: the second oldest must be evicted.
	require.NotNil(t, cache.get(metadataKey(1, prowlarr.Result{GUID: "0"}), now.Add(time.Second)))
	cache.put(key, []byte("torrent"), now.Add(2*time.Second))
	require.Len(t, cache.entries, searchMetadataMaxEntries)
	require.Nil(t, cache.get(metadataKey(1, prowlarr.Result{GUID: "1"}), now.Add(3*time.Second)))
	require.NotNil(t, cache.get(metadataKey(1, prowlarr.Result{GUID: "0"}), now.Add(3*time.Second)))
}

func TestSearchMetadataCache_ByteBudget(t *testing.T) {
	var cache searchMetadataCache
	now := time.Now()
	first, second := metadataKey(1, prowlarr.Result{GUID: "first"}), metadataKey(1, prowlarr.Result{GUID: "second"})
	cache.put(first, make([]byte, searchMetadataMaxBytes), now)
	cache.put(second, []byte{1}, now.Add(time.Second))
	require.Nil(t, cache.get(first, now.Add(2*time.Second)))
	require.Equal(t, []byte{1}, cache.get(second, now.Add(2*time.Second)))
	require.Equal(t, 1, cache.bytes)
}
