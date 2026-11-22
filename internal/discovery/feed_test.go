// Copyright (c) 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package discovery_test

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"testing/synctest"
	"time"

	"github.com/nuxencs/seasonpackarr/internal/discovery"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// poll runs one RSS run that reads the feed of the indexer. It returns the
// titles that the feed yields and the failures of the run.
func (f *sourceFixture) poll(t *testing.T, indexerID int, relevant func(string) bool) ([]string, []discovery.Failure) {
	t.Helper()
	run, err := f.source.Begin(t.Context(), f.settings, discovery.KindRSS)
	require.NoError(t, err)
	defer run.End()
	indexer, ok := usableIndexers(t, run)[indexerID]
	require.True(t, ok, "indexer %d must be usable", indexerID)
	return titles(run.Feed(t.Context(), indexer, relevant)), run.Failures()
}

func TestFeed_ReadsBackToCheckpoint(t *testing.T) {
	f := newSourceFixture(t)
	f.prowlarr.listings = map[int][]string{1: {"Old.S01E01", "Old.S01E02", "Old.S01E03"}}
	_, failures := f.poll(t, 1, accept())
	require.Empty(t, failures)
	require.Len(t, f.requestsTo("/1/api"), 1, "a first poll must read only the newest page")

	f.restart(t)
	f.prowlarr.listings[1] = []string{"Show.S01E05", "Show.S01", "Show.S01E04", "Show.S01E03", "Old.S01E01", "Old.S01E02", "Old.S01E03"}
	got, failures := f.poll(t, 1, accept("Show.S01"))
	assert.Empty(t, failures)
	assert.Equal(t, []string{"Show.S01"}, got)
	assert.Len(t, f.requestsTo("/1/api"), 4, "a later poll must read back to the checkpoint after a restart")

	_, failures = f.poll(t, 1, accept())
	assert.Empty(t, failures)
	assert.Len(t, f.requestsTo("/1/api"), 5, "the first page of the previous poll must be the checkpoint")
}

func TestFeed_YieldsNewThenRetainedListings(t *testing.T) {
	f := newSourceFixture(t)
	relevant := accept("Show.S01", "Other.S01", "Third.S01")
	f.prowlarr.listings = map[int][]string{1: {"Show.S01", "Show.S01E01"}}
	got, _ := f.poll(t, 1, relevant)
	require.Equal(t, []string{"Show.S01"}, got)

	f.prowlarr.listings[1] = []string{"Other.S01", "Show.S01"}
	got, _ = f.poll(t, 1, relevant)
	assert.Equal(t, []string{"Other.S01", "Show.S01"}, got, "a listing must be yielded one time")

	f.restart(t)
	f.prowlarr.listings[1] = []string{"Third.S01", "Other.S01"}
	got, failures := f.poll(t, 1, relevant)
	assert.Empty(t, failures)
	assert.Equal(t, []string{"Third.S01", "Other.S01", "Show.S01"}, got,
		"new listings must come in feed order, then the retained listings, also after a restart")

	got, _ = f.poll(t, 1, accept("Third.S01", "Other.S01"))
	assert.Equal(t, []string{"Third.S01", "Other.S01"}, got, "a retained listing that is no longer relevant must not be yielded")
}

func TestFeed_RetainedListingExpires(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		f := newSourceFixture(t)
		f.prowlarr.listings = map[int][]string{1: {"Show.S01", "Show.S01E01"}}
		f.poll(t, 1, accept("Show.S01"))
		f.prowlarr.listings[1] = []string{"Show.S01E02", "Show.S01E01"}

		time.Sleep(7*24*time.Hour - time.Hour)
		got, _ := f.poll(t, 1, accept("Show.S01"))
		assert.Equal(t, []string{"Show.S01"}, got, "a retained listing must be yielded before its retention time")

		time.Sleep(time.Hour)
		got, _ = f.poll(t, 1, accept("Show.S01"))
		assert.Empty(t, got, "a retained listing must not be yielded after its retention time")
	})
}

func TestFeed_GapFailure(t *testing.T) {
	const gap = "RSS feed did not overlap the previous poll; releases may be missing; use a manual targeted search"
	for _, tt := range []struct {
		name      string
		indexerID int
		feed      []string
		wantPages int
	}{
		{name: "page limit", indexerID: 1, feed: episodeTitles(25), wantPages: 10},
		{name: "end of feed", indexerID: 1, feed: episodeTitles(3), wantPages: 2},
		{name: "no pagination", indexerID: 3, feed: episodeTitles(3), wantPages: 1},
	} {
		t.Run(tt.name, func(t *testing.T) {
			f := newSourceFixture(t)
			path := fmt.Sprintf("/%d/api", tt.indexerID)
			f.prowlarr.listings = map[int][]string{tt.indexerID: {"Old.S01E01"}}
			f.poll(t, tt.indexerID, accept())
			f.prowlarr.listings[tt.indexerID] = tt.feed
			f.prowlarr.requests = nil

			got, failures := f.poll(t, tt.indexerID, accept("Show.S01E02"))

			assert.Equal(t, []string{"Show.S01E02"}, got, "the new listings must be yielded after a gap")
			assert.Equal(t, []discovery.Failure{{IndexerID: tt.indexerID, Reason: gap}}, failures)
			assert.Len(t, f.requestsTo(path), tt.wantPages)

			_, failures = f.poll(t, tt.indexerID, accept())
			assert.Empty(t, failures)
			assert.Len(t, f.requestsTo(path), tt.wantPages+1, "a gap must not cause a second catch-up")
		})
	}
}

func TestFeed_FailedPageKeepsCheckpoint(t *testing.T) {
	f := newSourceFixture(t)
	relevant := accept("Old.S01", "Show.S01")
	f.prowlarr.listings = map[int][]string{1: {"Old.S01", "Old.S01E01"}}
	f.poll(t, 1, relevant)
	f.prowlarr.listings[1] = []string{"Show.S01", "Show.S01E01", "Old.S01", "Old.S01E01"}
	f.prowlarr.respond = func(r *http.Request) *http.Response {
		if r.URL.Path == "/1/api" && r.URL.Query().Get("offset") == "2" {
			return cooldownResponse(http.StatusServiceUnavailable, "0")
		}
		return nil
	}

	got, failures := f.poll(t, 1, relevant)
	assert.Empty(t, got, "a failed page must yield no listing, also no retained listing")
	assert.Equal(t, []discovery.Failure{{IndexerID: 1, Reason: "Prowlarr returned HTTP 503"}}, failures)

	f.restart(t)
	f.prowlarr.respond = nil
	f.prowlarr.requests = nil
	got, failures = f.poll(t, 1, relevant)
	assert.Empty(t, failures)
	assert.Equal(t, []string{"Show.S01", "Old.S01"}, got)
	assert.Len(t, f.requestsTo("/1/api"), 2, "the next poll must read the same pages again")
}

func TestFeed_CanceledContextSavesNoCheckpoint(t *testing.T) {
	f := newSourceFixture(t)
	f.prowlarr.listings = map[int][]string{1: {"Old.S01E01"}}
	f.poll(t, 1, accept())
	f.prowlarr.listings[1] = []string{"Show.S01", "Show.S01E01", "Old.S01E01"}
	ctx, cancel := context.WithCancel(t.Context())
	f.prowlarr.respond = func(r *http.Request) *http.Response {
		if r.URL.Path == "/1/api" && r.URL.Query().Get("offset") == "2" {
			cancel()
		}
		return nil
	}
	run := f.begin(t, discovery.KindRSS)
	indexer := usableIndexers(t, run)[1]

	assert.Empty(t, titles(run.Feed(ctx, indexer, accept("Show.S01"))))
	run.End()
	assert.Empty(t, run.Failures(), "the caller reports a canceled context")
	assert.NoError(t, run.Err())

	f.prowlarr.respond = nil
	f.prowlarr.requests = nil
	got, failures := f.poll(t, 1, accept("Show.S01"))
	assert.Empty(t, failures)
	assert.Equal(t, []string{"Show.S01"}, got)
	assert.Len(t, f.requestsTo("/1/api"), 2, "the next poll must read back to the checkpoint of the poll before")
}

func TestFeed_ConnectionChangeDropsFeedState(t *testing.T) {
	for name, change := range map[string]func(*sourceFixture){
		"url":     func(f *sourceFixture) { f.settings.ProwlarrURL += "/" },
		"api key": func(f *sourceFixture) { f.settings.APIKey = "other-key"; f.prowlarr.apiKey = "other-key" },
	} {
		t.Run(name, func(t *testing.T) {
			f := newSourceFixture(t)
			f.prowlarr.listings = map[int][]string{1: {"Show.S01", "Show.S01E01"}}
			f.poll(t, 1, accept("Show.S01"))

			change(f)
			f.prowlarr.listings[1] = episodeTitles(3)
			f.prowlarr.requests = nil
			got, failures := f.poll(t, 1, accept("Show.S01"))

			assert.Empty(t, got, "a connection change must drop the retained listings")
			assert.Empty(t, failures, "a connection change must drop the checkpoint")
			assert.Len(t, f.requestsTo("/1/api"), 1)
		})
	}
}

func TestFeed_UnreadableClientSavesNoFeedState(t *testing.T) {
	f := newSourceFixture(t)
	f.prowlarr.listings = map[int][]string{1: {"Old.S01E01"}}
	f.poll(t, 1, accept())
	f.prowlarr.listings[1] = []string{"Show.S01", "Show.S01E01", "Old.S01E01"}
	run := f.begin(t, discovery.KindRSS)
	run.ClientUnreadable()
	indexer := usableIndexers(t, run)[1]
	assert.Equal(t, []string{"Show.S01"}, titles(run.Feed(t.Context(), indexer, accept("Show.S01"))))
	run.End()

	f.restart(t)
	f.prowlarr.requests = nil
	got, failures := f.poll(t, 1, accept("Show.S01"))
	assert.Empty(t, failures)
	assert.Equal(t, []string{"Show.S01"}, got)
	assert.Len(t, f.requestsTo("/1/api"), 2, "the next run must read back to the checkpoint of the run before")
}

func TestFeed_EndsWhenDownloadTakesIndexerOut(t *testing.T) {
	f := newSourceFixture(t)
	f.prowlarr.listings = map[int][]string{1: {"Show.S01", "Other.S01"}}
	f.prowlarr.respond = atTitle("Show.S01", func() *http.Response { return cooldownResponse(http.StatusTooManyRequests, "7200") })
	run := f.begin(t, discovery.KindRSS)

	var got []string
	for listing := range run.Feed(t.Context(), usableIndexers(t, run)[1], accept("Show.S01", "Other.S01")) {
		got = append(got, listing.Title())
		_, _ = listing.Torrent(t.Context())
	}

	assert.Equal(t, []string{"Show.S01"}, got)
	assert.Len(t, f.requestsTo("/1/download"), 1)
}

func TestFeed_CooldownKeepsFeedState(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		f := newSourceFixture(t)
		f.prowlarr.listings = map[int][]string{1: {"Show.S01", "Show.S01E01"}, 3: {"Other.S01E01"}}
		f.prowlarr.respond = atTitle("Show.S01", func() *http.Response { return cooldownResponse(http.StatusTooManyRequests, "7200") })
		run := f.begin(t, discovery.KindRSS)
		for listing := range run.Feed(t.Context(), usableIndexers(t, run)[1], accept("Show.S01")) {
			_, err := listing.Torrent(t.Context())
			require.ErrorIs(t, err, discovery.ErrDownload)
		}
		run.End()
		f.prowlarr.respond = nil

		// The prune of this run sees indexer 1 only in cooldown.
		ids, failures := f.indexers(t, discovery.KindRSS)
		require.Equal(t, []int{3}, ids)
		require.Len(t, failures, 1)
		f.poll(t, 3, accept())

		time.Sleep(3 * time.Hour)
		f.prowlarr.listings[1] = []string{"A.S01E01", "B.S01E01", "Show.S01E01"}
		f.prowlarr.requests = nil
		got, failures := f.poll(t, 1, accept("Show.S01"))
		assert.Empty(t, failures)
		assert.Equal(t, []string{"Show.S01"}, got, "an indexer in cooldown must keep its retained listings")
		assert.Len(t, f.requestsTo("/1/api"), 2, "an indexer in cooldown must keep its checkpoint")
	})
}

// TestFeed_CooldownSharedWithSearch covers the feed part of the cooldown rule:
// a cooldown from a feed page applies to a search run, and the reverse.
func TestFeed_CooldownSharedWithSearch(t *testing.T) {
	const inCooldown = "indexer cooldown or Retry-After deadline has not elapsed"
	for name, kinds := range map[string][2]discovery.Kind{
		"feed then search": {discovery.KindRSS, discovery.KindSearch},
		"search then feed": {discovery.KindSearch, discovery.KindRSS},
	} {
		t.Run(name, func(t *testing.T) {
			f := newSourceFixture(t)
			f.prowlarr.listings = map[int][]string{1: {"Show.S01"}}
			f.prowlarr.respond = at("/1/api", func() *http.Response { return cooldownResponse(http.StatusTooManyRequests, "7200") })
			run := f.begin(t, kinds[0])
			indexer := usableIndexers(t, run)[1]
			if kinds[0] == discovery.KindRSS {
				assert.Empty(t, titles(run.Feed(t.Context(), indexer, accept("Show.S01"))))
			} else {
				assert.Empty(t, titles(run.Search(t.Context(), indexer, showQuery)))
			}
			run.End()
			require.Len(t, run.Failures(), 1)

			ids, failures := f.indexers(t, kinds[1])
			assert.NotContains(t, ids, 1)
			assert.Equal(t, []discovery.Failure{{IndexerID: 1, Reason: inCooldown}}, failures)
			assert.Len(t, f.requestsTo("/1/api"), 1)
		})
	}
}
