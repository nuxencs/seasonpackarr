// Copyright (c) 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package discovery_test

import (
	"net/http"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/nuxencs/seasonpackarr/internal/discovery"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIndexers_CapabilityOfKind(t *testing.T) {
	for _, tt := range []struct {
		name string
		kind discovery.Kind
		want []int
	}{
		{"search", discovery.KindSearch, []int{2, 1}},
		{"rss", discovery.KindRSS, []int{3, 1}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			f := newSourceFixture(t)

			ids, failures := f.indexers(t, tt.kind)

			assert.Equal(t, tt.want, ids)
			assert.Empty(t, failures)
			assert.Equal(t, 1, f.requestCount())
		})
	}
}

func TestIndexers_Allowlist(t *testing.T) {
	const (
		noSearchable  = "no enabled searchable torrent indexers in Prowlarr"
		missingSearch = "selected indexer is missing, disabled, or not a searchable torrent indexer"
		missingRSS    = "selected indexer is missing, disabled, or not a RSS-capable torrent indexer"
		noRSSCapable  = "no enabled RSS-capable torrent indexers in Prowlarr"
	)
	for _, tt := range []struct {
		name         string
		kind         discovery.Kind
		ids          []int
		want         []int
		wantFailures []discovery.Failure
	}{
		{"selected", discovery.KindSearch, []int{1}, []int{1}, nil},
		{"selected keeps priority order", discovery.KindSearch, []int{1, 2}, []int{2, 1}, nil},
		{"missing", discovery.KindSearch, []int{99}, nil, []discovery.Failure{
			{IndexerID: 99, Reason: missingSearch},
			{Reason: noSearchable},
		}},
		{"partial", discovery.KindSearch, []int{1, 99}, []int{1}, []discovery.Failure{
			{IndexerID: 99, Reason: missingSearch},
		}},
		{"disabled", discovery.KindSearch, []int{4}, nil, []discovery.Failure{
			{IndexerID: 4, Reason: missingSearch},
			{Reason: noSearchable},
		}},
		{"not capable", discovery.KindRSS, []int{2}, nil, []discovery.Failure{
			{IndexerID: 2, Reason: missingRSS},
			{Reason: noRSSCapable},
		}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			f := newSourceFixture(t)
			f.settings.IndexerIDs = tt.ids

			ids, failures := f.indexers(t, tt.kind)

			assert.Equal(t, tt.want, ids)
			assert.Equal(t, tt.wantFailures, failures)
		})
	}
}

func TestIndexers_FailedReadPutsProwlarrInCooldown(t *testing.T) {
	for _, tt := range []struct {
		name        string
		first, next discovery.Kind
	}{
		{"search then rss", discovery.KindSearch, discovery.KindRSS},
		{"rss then search", discovery.KindRSS, discovery.KindSearch},
	} {
		t.Run(tt.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				f := newSourceFixture(t)
				f.prowlarr.respond = func(*http.Request) *http.Response { return cooldownResponse(http.StatusServiceUnavailable, "7200") }
				inCooldown := []discovery.Failure{{Reason: "Prowlarr cooldown is still active"}}

				ids, failures := f.indexers(t, tt.first)
				require.Empty(t, ids)
				require.Equal(t, []discovery.Failure{{Reason: "Prowlarr returned HTTP 503"}}, failures)
				f.prowlarr.respond = nil

				ids, failures = f.indexers(t, tt.next)
				assert.Empty(t, ids)
				assert.Equal(t, inCooldown, failures)
				assert.Equal(t, 1, f.requestCount(), "the cooldown must apply to the next run of each kind")

				f.restart(t)
				time.Sleep(time.Hour)
				ids, failures = f.indexers(t, tt.first)
				assert.Empty(t, ids)
				assert.Equal(t, inCooldown, failures)
				assert.Equal(t, 1, f.requestCount(), "the cooldown must hold across a restart")

				time.Sleep(time.Hour)
				ids, failures = f.indexers(t, tt.first)
				assert.NotEmpty(t, ids)
				assert.Empty(t, failures)
				assert.Equal(t, 2, f.requestCount(), "a run after the deadline must send the request again")
			})
		})
	}
}

func TestIndexers_ElapsedRetryAfterDoesNotBlockNextRun(t *testing.T) {
	f := newSourceFixture(t)
	f.prowlarr.respond = func(*http.Request) *http.Response { return cooldownResponse(http.StatusTooManyRequests, "0") }
	_, failures := f.indexers(t, discovery.KindSearch)
	require.Equal(t, []discovery.Failure{{Reason: "Prowlarr returned HTTP 429; indexer is rate limited"}}, failures)
	f.prowlarr.respond = nil

	ids, failures := f.indexers(t, discovery.KindSearch)

	assert.Equal(t, []int{2, 1}, ids)
	assert.Empty(t, failures)
	assert.Equal(t, 2, f.requestCount())
}

func TestIndexers_DatabaseFailure(t *testing.T) {
	f := newSourceFixture(t)
	run := f.begin(t, discovery.KindSearch)
	require.NoError(t, run.Err())
	require.NoError(t, f.store.Close())
	f.prowlarr.respond = func(*http.Request) *http.Response { return cooldownResponse(http.StatusServiceUnavailable, "7200") }

	ids := run.Indexers(t.Context())

	assert.Empty(t, ids)
	assert.ErrorIs(t, run.Err(), discovery.ErrDatabase)
	assert.Equal(t, []discovery.Failure{
		{Reason: "could not access database; check service logs"},
		{Reason: "Prowlarr returned HTTP 503"},
	}, run.Failures())
	event := f.log.Events(t).Require(t, "database operation failed")
	assert.Equal(t, "sql: database is closed", event["error"])
}

func TestIndexers_KeepsAPIKeySecret(t *testing.T) {
	const key = "prowlarr-key-sentinel-3c1f"
	f := newSourceFixture(t)
	f.settings.APIKey = key
	f.prowlarr.apiKey = key
	run := f.begin(t, discovery.KindSearch)
	require.NoError(t, f.store.Close())
	// The body echoes the key, as a proxy error page can.
	f.prowlarr.respond = func(r *http.Request) *http.Response {
		resp := cooldownResponse(http.StatusInternalServerError, "7200")
		resp.Body = response(0, "upstream failed for apikey="+r.Header.Get("X-Api-Key")).Body
		return resp
	}

	run.Indexers(t.Context())

	require.Len(t, run.Failures(), 2)
	for _, failure := range run.Failures() {
		assert.NotContains(t, failure.Reason, key)
	}
	f.log.Events(t).Require(t, "database operation failed")
	f.log.AssertNoSecrets(t, map[string]string{"Prowlarr API key": key})
}

func TestRun_DatabaseFailureEndsSequences(t *testing.T) {
	const databaseFailure = "could not access database; check service logs"
	t.Run("torrent cache read", func(t *testing.T) {
		f := newSourceFixture(t)
		f.prowlarr.listings = map[int][]string{2: {"Show.S01", "Show.S01.REPACK"}}
		run := f.begin(t, discovery.KindSearch)
		indexers := usableIndexers(t, run)

		var got []string
		var errs []error
		for listing := range run.Search(t.Context(), indexers[2], showQuery) {
			got = append(got, listing.Title())
			require.NoError(t, f.store.Close())
			_, err := listing.Torrent(t.Context())
			errs = append(errs, err)
			_, err = listing.Torrent(t.Context())
			errs = append(errs, err)
		}

		assert.Equal(t, []string{"Show.S01"}, got)
		for _, err := range errs {
			assert.ErrorIs(t, err, discovery.ErrDatabase)
		}
		assert.ErrorIs(t, run.Err(), discovery.ErrDatabase)
		assert.Equal(t, []discovery.Failure{{Reason: databaseFailure}}, run.Failures())
		pages := f.requestCount()
		assert.Empty(t, titles(run.Search(t.Context(), indexers[1], showQuery)))
		assert.Equal(t, pages, f.requestCount(), "a sequence after a database failure must send no request")
		assert.Empty(t, f.requestsTo("/2/download"))
		event := f.log.Events(t).Require(t, "database operation failed")
		assert.Equal(t, "sql: database is closed", event["error"])
	})
	t.Run("cooldown write", func(t *testing.T) {
		f := newSourceFixture(t)
		f.prowlarr.listings = map[int][]string{2: {"Show.S01"}}
		f.prowlarr.respond = at("/1/api", func() *http.Response { return cooldownResponse(http.StatusServiceUnavailable, "7200") })
		run := f.begin(t, discovery.KindSearch)
		indexers := usableIndexers(t, run)
		require.NoError(t, f.store.Close())

		assert.Empty(t, titles(run.Search(t.Context(), indexers[1], showQuery)))
		assert.Empty(t, titles(run.Search(t.Context(), indexers[2], showQuery)))

		assert.ErrorIs(t, run.Err(), discovery.ErrDatabase)
		assert.Equal(t, []discovery.Failure{
			{IndexerID: 1, Query: "Show S01", Reason: "Prowlarr returned HTTP 503"},
			{Reason: databaseFailure},
		}, run.Failures())
		assert.Empty(t, f.requestsTo("/2/api"))
	})
	t.Run("torrent cache write", func(t *testing.T) {
		f := newSourceFixture(t)
		f.prowlarr.listings = map[int][]string{2: {"Show.S01", "Show.S01.REPACK"}}
		// The store closes after the cache read, so only the write fails.
		f.prowlarr.respond = atTitle("Show.S01", func() *http.Response {
			require.NoError(t, f.store.Close())
			return nil
		})
		run := f.begin(t, discovery.KindSearch)

		got := searchAll(t, run, usableIndexers(t, run)[2])

		assert.Equal(t, []string{"Show.S01"}, got)
		assert.ErrorIs(t, run.Err(), discovery.ErrDatabase)
		assert.Equal(t, []discovery.Failure{{Reason: databaseFailure}}, run.Failures())
		assert.Len(t, f.requestsTo("/2/download"), 1)
	})
	for name, closeStore := range map[string]func(*testing.T, *sourceFixture){
		"feed state load": func(t *testing.T, f *sourceFixture) { require.NoError(t, f.store.Close()) },
		// The store closes after the first save, so only the save after the read fails.
		"feed state save": func(t *testing.T, f *sourceFixture) {
			f.prowlarr.respond = at("/1/api", func() *http.Response {
				require.NoError(t, f.store.Close())
				return nil
			})
		},
	} {
		t.Run(name, func(t *testing.T) {
			f := newSourceFixture(t)
			f.prowlarr.listings = map[int][]string{1: {"Show.S01"}, 3: {"Show.S01"}}
			run := f.begin(t, discovery.KindRSS)
			indexers := usableIndexers(t, run)
			closeStore(t, f)

			assert.Empty(t, titles(run.Feed(t.Context(), indexers[1], accept("Show.S01"))), "a database failure must end the sequence before a listing")
			assert.Empty(t, titles(run.Feed(t.Context(), indexers[3], accept("Show.S01"))))

			assert.ErrorIs(t, run.Err(), discovery.ErrDatabase)
			assert.Equal(t, []discovery.Failure{{Reason: databaseFailure}}, run.Failures())
			assert.Empty(t, f.requestsTo("/3/api"), "a sequence after a database failure must send no request")
		})
	}
}

func TestStats_MatchesProwlarrRequests(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		f := newSourceFixture(t)
		f.prowlarr.listings = map[int][]string{1: {"A", "B", "C"}, 2: {"Show.S01", "Show.S01.REPACK"}}
		f.fetchFirst(t)
		f.prowlarr.requests = nil
		run := f.begin(t, discovery.KindSearch)
		indexers := usableIndexers(t, run)

		searchAll(t, run, indexers[2])
		searchAll(t, run, indexers[1])

		var pages, downloads int
		for _, request := range f.prowlarr.requests {
			switch {
			case strings.HasSuffix(request.Path, "/download"):
				downloads++
			case request.Path != "/api/v1/indexer":
				pages++
			}
		}
		want := discovery.Stats{FeedPages: 3, Downloads: 4, CacheHits: 1}
		assert.Equal(t, want, run.Stats())
		assert.Equal(t, discovery.Stats{FeedPages: pages, Downloads: downloads, CacheHits: 1}, run.Stats())
	})
}
