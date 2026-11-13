// Copyright (c) 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package state

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/nuxencs/seasonpackarr/internal/prowlarr"
	"github.com/stretchr/testify/require"
)

func TestFeeds_BoundsExpiryAndSelection(t *testing.T) {
	var feeds Feeds
	now := time.Now()
	indexers := []prowlarr.Indexer{{ID: 1}, {ID: 2}}
	feeds.Prune(indexers, now)
	for i := range rssMaxEntries + 1 {
		result := prowlarr.Result{Title: fmt.Sprint(i), GUID: fmt.Sprint(i)}
		feeds.Remember(RSSIdentity(1, result), result, now)
	}
	require.Len(t, feeds.entries, rssMaxEntries)
	require.Equal(t, "1", feeds.Results(1)[0].Title, "oldest entry is evicted")
	large := prowlarr.Result{Title: "large", GUID: "large", Link: strings.Repeat("x", rssMaxBytes-100)}
	feeds.Remember(RSSIdentity(2, large), large, now)
	require.LessOrEqual(t, feeds.bytes, rssMaxBytes)
	require.Less(t, len(feeds.entries), rssMaxEntries)
	feeds.SetCheckpoint(2, map[Key]bool{RSSIdentity(2, large): true})
	feeds.Prune(indexers[:1], now)
	require.Empty(t, feeds.Results(2))
	require.Empty(t, feeds.Checkpoint(2))
	feeds.Prune(indexers, now.Add(rssRetention))
	require.Empty(t, feeds.entries)
	require.Zero(t, feeds.bytes)
}

func TestFeeds_RefreshKeepsExpiryAndWorkingSetIsIsolated(t *testing.T) {
	s, _ := testStore(t)
	ctx := t.Context()
	feeds, err := s.Feeds(ctx)
	require.NoError(t, err)
	now := time.Now()
	result := prowlarr.Result{Title: "pack", GUID: "one", Link: "/old"}
	key := RSSIdentity(1, result)
	feeds.Remember(key, result, now)
	require.NoError(t, s.SaveFeeds(ctx, feeds))
	result.Link = "/fresh"
	feeds.Remember(key, result, now.Add(time.Hour))
	require.Equal(t, now.Add(rssRetention), feeds.entries[key].expires)
	require.Equal(t, "/fresh", feeds.entries[key].result.Link)
	copy, err := s.Feeds(ctx)
	require.NoError(t, err)
	require.Equal(t, "/old", copy.entries[key].result.Link, "uncommitted changes must remain isolated")
	copy.remove(key)
	copy.SetCheckpoint(1, map[Key]bool{key: true})
	require.Contains(t, feeds.entries, key)
	require.Empty(t, feeds.Checkpoint(1))
}

func TestFeeds_FailedSaveRollsBackCheckpointAndCandidates(t *testing.T) {
	s, _ := testStore(t)
	ctx := t.Context()
	feeds, err := s.Feeds(ctx)
	require.NoError(t, err)
	old := prowlarr.Result{Title: "old", GUID: "old"}
	key := RSSIdentity(1, old)
	feeds.Remember(key, old, time.Now())
	feeds.SetCheckpoint(1, map[Key]bool{key: true})
	require.NoError(t, s.SaveFeeds(ctx, feeds))
	_, err = s.db.Exec(`CREATE TRIGGER fail_candidates BEFORE INSERT ON rss_candidates BEGIN SELECT RAISE(ABORT, 'write failed'); END`)
	require.NoError(t, err)
	fresh := prowlarr.Result{Title: "fresh", GUID: "fresh"}
	feeds.Remember(RSSIdentity(1, fresh), fresh, time.Now())
	feeds.SetCheckpoint(1, map[Key]bool{RSSIdentity(1, fresh): true})
	require.Error(t, s.SaveFeeds(ctx, feeds))
	actual, err := s.Feeds(ctx)
	require.NoError(t, err)
	require.Equal(t, map[Key]bool{key: true}, actual.Checkpoint(1))
	require.Equal(t, []prowlarr.Result{old}, actual.Results(1))
}
