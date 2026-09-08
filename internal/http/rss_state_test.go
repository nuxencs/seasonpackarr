// Copyright (c) 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package http

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/nuxencs/seasonpackarr/internal/prowlarr"
	"github.com/stretchr/testify/require"
)

func TestRSSState_BoundsExpiryAndSelection(t *testing.T) {
	var state rssState
	now := time.Now()
	indexers := []prowlarr.Indexer{{ID: 1}, {ID: 2}}
	state.prune(indexers, now)
	for i := range rssMaxEntries + 1 {
		result := prowlarr.Result{Title: fmt.Sprint(i), GUID: fmt.Sprint(i)}
		state.remember(rssIdentity(1, result), result, now)
	}
	require.Len(t, state.entries, rssMaxEntries)
	require.Equal(t, "1", state.results(1)[0].Title, "oldest entry is evicted")
	large := prowlarr.Result{Title: "large", GUID: "large", Link: strings.Repeat("x", rssMaxBytes-100)}
	state.remember(rssIdentity(2, large), large, now)
	require.LessOrEqual(t, state.bytes, rssMaxBytes)
	require.Less(t, len(state.entries), rssMaxEntries)
	state.checkpoints[2] = map[searchMetadataKey]bool{rssIdentity(2, large): true}
	state.prune(indexers[:1], now)
	require.Empty(t, state.results(2))
	require.NotContains(t, state.checkpoints, 2)
	state.prune(indexers, now.Add(rssRetention))
	require.Empty(t, state.entries)
	require.Zero(t, state.bytes)
}

func TestRSSState_RefreshKeepsExpiryAndCloneIsIsolated(t *testing.T) {
	var state rssState
	now := time.Now()
	state.prune([]prowlarr.Indexer{{ID: 1}}, now)
	result := prowlarr.Result{Title: "pack", GUID: "one", Link: "/old"}
	key := rssIdentity(1, result)
	state.remember(key, result, now)
	result.Link = "/fresh"
	state.remember(key, result, now.Add(time.Hour))
	require.Equal(t, now.Add(rssRetention), state.entries[key].expires)
	require.Equal(t, "/fresh", state.entries[key].result.Link)
	copy := state.clone()
	copy.remove(key)
	copy.checkpoints[1] = map[searchMetadataKey]bool{key: true}
	require.Contains(t, state.entries, key)
	require.Empty(t, state.checkpoints)
}
