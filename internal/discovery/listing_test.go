// Copyright (c) 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package discovery_test

import (
	"context"
	"net/http"
	"testing"
	"testing/synctest"

	"github.com/nuxencs/seasonpackarr/internal/discovery"

	"github.com/autobrr/go-torrent/metainfo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestListingTorrent_DownloadsOnce(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		f := newSourceFixture(t)
		f.prowlarr.listings = map[int][]string{2: {"Show.S01"}}
		run := f.begin(t, discovery.KindSearch)
		listing := firstListing(t, run)

		first, err := listing.Torrent(t.Context())
		require.NoError(t, err)
		second, err := listing.Torrent(t.Context())
		require.NoError(t, err)

		assert.Equal(t, multiFileTorrent("Show.S01"), first)
		assert.Equal(t, first, second)
		assert.Len(t, f.requestsTo("/2/download"), 1)
		assert.Equal(t, discovery.Stats{FeedPages: 1, Downloads: 1}, run.Stats())
	})
	t.Run("failure", func(t *testing.T) {
		f := newSourceFixture(t)
		f.prowlarr.listings = map[int][]string{2: {"Show.S01"}}
		f.prowlarr.respond = at("/2/download", func() *http.Response { return response(http.StatusNotFound, "") })
		run := f.begin(t, discovery.KindSearch)
		listing := firstListing(t, run)

		_, first := listing.Torrent(t.Context())
		_, second := listing.Torrent(t.Context())

		require.ErrorIs(t, first, discovery.ErrDownload)
		assert.EqualError(t, first, "Prowlarr returned HTTP 404")
		assert.Equal(t, first, second)
		assert.Len(t, f.requestsTo("/2/download"), 1)
		assert.Equal(t, discovery.Stats{FeedPages: 1}, run.Stats())
		assert.Empty(t, run.Failures(), "a download failure is the failure of one listing, not of the run")
	})
}

func singleFileTorrent(name string) []byte {
	return torrentBytes(metainfo.Info{Name: name + ".mkv", PieceLength: 16384, Length: 1})
}

func TestListingTorrent_TorrentCache(t *testing.T) {
	downloaded := discovery.Stats{FeedPages: 1, Downloads: 1}
	cached := discovery.Stats{FeedPages: 1, CacheHits: 1}
	for _, tt := range []struct {
		name    string
		torrent []byte
		later   discovery.Stats
	}{
		{"multi-file torrent", multiFileTorrent("Show.S01"), cached},
		{"single-file torrent", singleFileTorrent("Show.S01"), cached},
		{"bytes that do not parse", []byte("invalid torrent"), downloaded},
	} {
		t.Run(tt.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				f := newSourceFixture(t)
				f.prowlarr.listings = map[int][]string{2: {"Show.S01"}}
				f.prowlarr.torrents = map[string][]byte{"Show.S01": tt.torrent}

				data, stats := f.fetchFirst(t)
				assert.Equal(t, tt.torrent, data)
				assert.Equal(t, downloaded, stats)

				data, stats = f.fetchFirst(t)
				assert.Equal(t, tt.torrent, data)
				assert.Equal(t, tt.later, stats, "a later run")

				f.restart(t)
				data, stats = f.fetchFirst(t)
				assert.Equal(t, tt.torrent, data)
				assert.Equal(t, tt.later, stats, "a run after a restart")
				assert.Len(t, f.requestsTo("/2/download"), 1+2*tt.later.Downloads)
			})
		})
	}
}

func TestListingTorrent_ConnectionChangeEmptiesTorrentCache(t *testing.T) {
	for name, change := range map[string]func(f *sourceFixture){
		"url": func(f *sourceFixture) { f.settings.ProwlarrURL += "/" },
		"api key": func(f *sourceFixture) {
			f.settings.APIKey = "prowlarr-other-key"
			f.prowlarr.apiKey = "prowlarr-other-key"
		},
	} {
		t.Run(name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				f := newSourceFixture(t)
				f.prowlarr.listings = map[int][]string{2: {"Show.S01"}}
				f.fetchFirst(t)
				change(f)

				_, stats := f.fetchFirst(t)

				assert.Equal(t, discovery.Stats{FeedPages: 1, Downloads: 1}, stats)
				assert.Len(t, f.requestsTo("/2/download"), 2)
			})
		})
	}
}

// TestListingTorrent_FailedDownloadLeavesIndexerUsable covers rule 4.
func TestListingTorrent_FailedDownloadLeavesIndexerUsable(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		f := newSourceFixture(t)
		f.prowlarr.listings = map[int][]string{2: {"Show.S01", "Show.S01.REPACK"}}
		f.prowlarr.respond = atTitle("Show.S01", func() *http.Response { return response(http.StatusNotFound, "") })
		run := f.begin(t, discovery.KindSearch)

		var got []string
		var errs []error
		for listing := range run.Search(t.Context(), usableIndexers(t, run)[2], showQuery) {
			got = append(got, listing.Title())
			_, err := listing.Torrent(t.Context())
			errs = append(errs, err)
		}

		assert.Equal(t, []string{"Show.S01", "Show.S01.REPACK"}, got)
		require.Len(t, errs, 2)
		assert.ErrorIs(t, errs[0], discovery.ErrDownload)
		assert.NoError(t, errs[1])
		assert.Empty(t, run.Failures())
		assert.Equal(t, []string{"Show.S01", "Show.S01.REPACK"}, titles(run.Search(t.Context(), usableIndexers(t, run)[2], showQuery)))
	})
}

func TestListingTorrent_CanceledContext(t *testing.T) {
	f := newSourceFixture(t)
	f.prowlarr.listings = map[int][]string{2: {"Show.S01"}}
	run := f.begin(t, discovery.KindSearch)
	listing := firstListing(t, run)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	_, err := listing.Torrent(ctx)

	require.ErrorIs(t, err, context.Canceled)
	assert.NotErrorIs(t, err, discovery.ErrDownload)
	assert.NotErrorIs(t, err, discovery.ErrDatabase)
	assert.Empty(t, f.requestsTo("/2/download"))
}
