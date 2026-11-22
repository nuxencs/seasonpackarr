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
		release := prowlarr.Release{Title: fmt.Sprint(i), GUID: fmt.Sprint(i)}
		feeds.Remember(RSSIdentity(1, release), release, now)
	}
	require.Len(t, feeds.entries, rssMaxEntries)
	require.Equal(t, "1", feeds.Retained(1)[0].Title, "oldest entry is evicted")
	large := prowlarr.Release{Title: "large", GUID: "large", Link: strings.Repeat("x", rssMaxBytes-100)}
	feeds.Remember(RSSIdentity(2, large), large, now)
	require.LessOrEqual(t, feeds.bytes, rssMaxBytes)
	require.Less(t, len(feeds.entries), rssMaxEntries)
	feeds.SetCheckpoint(2, map[Key]bool{RSSIdentity(2, large): true})
	feeds.Prune(indexers[:1], now)
	require.Empty(t, feeds.Retained(2))
	require.Empty(t, feeds.Checkpoint(2))
	feeds.Prune(indexers, now.Add(rssRetention))
	require.Empty(t, feeds.entries)
	require.Zero(t, feeds.bytes)
}

func TestFeeds_RefreshKeepsExpiryAndWorkingSetIsIsolated(t *testing.T) {
	s, _ := newTestStore(t)
	ctx := t.Context()
	feeds, err := s.Feeds(ctx)
	require.NoError(t, err)
	now := time.Now()
	release := prowlarr.Release{Title: "pack", GUID: "one", Link: "/old"}
	key := RSSIdentity(1, release)
	feeds.Remember(key, release, now)
	require.NoError(t, s.SaveFeeds(ctx, feeds))
	release.Link = "/fresh"
	feeds.Remember(key, release, now.Add(time.Hour))
	require.Equal(t, now.Add(rssRetention), feeds.entries[key].expires)
	require.Equal(t, "/fresh", feeds.entries[key].release.Link)
	copy, err := s.Feeds(ctx)
	require.NoError(t, err)
	require.Equal(t, "/old", copy.entries[key].release.Link, "uncommitted changes must remain isolated")
	copy.remove(key)
	copy.SetCheckpoint(1, map[Key]bool{key: true})
	require.Contains(t, feeds.entries, key)
	require.Empty(t, feeds.Checkpoint(1))
}

func TestFeeds_FailedSaveRollsBackCheckpointAndRetainedListings(t *testing.T) {
	s, _ := newTestStore(t)
	ctx := t.Context()
	feeds, err := s.Feeds(ctx)
	require.NoError(t, err)
	old := prowlarr.Release{Title: "old", GUID: "old"}
	key := RSSIdentity(1, old)
	feeds.Remember(key, old, time.Now())
	feeds.SetCheckpoint(1, map[Key]bool{key: true})
	require.NoError(t, s.SaveFeeds(ctx, feeds))
	_, err = s.db.Exec(`CREATE TRIGGER fail_retained_listings BEFORE INSERT ON rss_candidates BEGIN SELECT RAISE(ABORT, 'write failed'); END`)
	require.NoError(t, err)
	fresh := prowlarr.Release{Title: "fresh", GUID: "fresh"}
	feeds.Remember(RSSIdentity(1, fresh), fresh, time.Now())
	feeds.SetCheckpoint(1, map[Key]bool{RSSIdentity(1, fresh): true})
	require.Error(t, s.SaveFeeds(ctx, feeds))
	actual, err := s.Feeds(ctx)
	require.NoError(t, err)
	require.Equal(t, map[Key]bool{key: true}, actual.Checkpoint(1))
	require.Equal(t, []prowlarr.Release{old}, actual.Retained(1))
}
