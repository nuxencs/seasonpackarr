// Copyright (c) 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package discovery_test

import (
	"fmt"
	"net/http"
	"testing"
	"testing/synctest"
	"time"

	"github.com/nuxencs/seasonpackarr/internal/discovery"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSearch_RequestsPagesOnDemand(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		f := newSourceFixture(t)
		f.prowlarr.listings = map[int][]string{1: {"Show.S01E01", "Show.S01E02", "Show.S01", "Show.S01E03", "Show.S01E04"}}
		run := f.begin(t, discovery.KindSearch)
		indexer := usableIndexers(t, run)[1]

		var seen []string
		for listing := range run.Search(t.Context(), indexer, showQuery) {
			seen = append(seen, listing.Title())
			assert.Equal(t, 1, listing.IndexerID())
			if listing.Title() == "Show.S01" {
				break
			}
		}

		assert.Equal(t, []string{"Show.S01E01", "Show.S01E02", "Show.S01"}, seen)
		pages := f.requestsTo("/1/api")
		if assert.Len(t, pages, 2, "the search must request no page after the caller stops") {
			assert.Equal(t, "0", pages[0].Query.Get("offset"))
			assert.Equal(t, "2", pages[1].Query.Get("offset"))
			assert.Equal(t, "Show S01", pages[0].Query.Get("q"))
		}
		assert.Empty(t, run.Failures())
	})
}

func TestSearch_PageStops(t *testing.T) {
	const (
		budget   = "search reached the 10-page limit; remaining results may be incomplete"
		repeated = "indexer repeated a page; remaining results may be incomplete"
	)
	many := episodeTitles(25)
	for _, tt := range []struct {
		name         string
		indexerID    int
		listings     []string
		respond      func(*http.Request) *http.Response
		wantTitles   []string
		wantPages    int
		wantFailures []discovery.Failure
	}{
		{
			name: "short page", indexerID: 1, listings: []string{"A", "B", "C"},
			wantTitles: []string{"A", "B", "C"}, wantPages: 2,
		},
		{
			name: "full last page", indexerID: 1, listings: []string{"A", "B", "C", "D"},
			wantTitles: []string{"A", "B", "C", "D"}, wantPages: 3,
		},
		{
			name: "no pagination", indexerID: 2, listings: []string{"A", "B", "C"},
			wantTitles: []string{"A", "B", "C"}, wantPages: 1,
		},
		{
			name: "duplicate listing", indexerID: 2, listings: []string{"A", "B", "A"},
			wantTitles: []string{"A", "B"}, wantPages: 1,
		},
		{
			name: "page budget", indexerID: 1, listings: many,
			wantTitles: many[:20], wantPages: 10,
			wantFailures: []discovery.Failure{{IndexerID: 1, Query: "Show S01", Reason: budget}},
		},
		{
			name: "repeated page", indexerID: 1,
			respond: func(r *http.Request) *http.Response {
				if r.URL.Path != "/1/api" {
					return nil
				}
				return feedResponse(1, "A", "B")
			},
			wantTitles: []string{"A", "B"}, wantPages: 2,
			wantFailures: []discovery.Failure{{IndexerID: 1, Query: "Show S01", Reason: repeated}},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				f := newSourceFixture(t)
				f.prowlarr.listings = map[int][]string{tt.indexerID: tt.listings}
				f.prowlarr.respond = tt.respond
				run := f.begin(t, discovery.KindSearch)
				indexer := usableIndexers(t, run)[tt.indexerID]

				got := titles(run.Search(t.Context(), indexer, showQuery))

				assert.Equal(t, tt.wantTitles, got)
				assert.Len(t, f.requestsTo(fmt.Sprintf("/%d/api", tt.indexerID)), tt.wantPages)
				assert.Equal(t, tt.wantFailures, run.Failures())
				assert.Equal(t, tt.wantPages, run.Stats().FeedPages)
			})
		})
	}
}

func TestSearch_CooldownsAcrossRuns(t *testing.T) {
	const inCooldown = "indexer cooldown or Retry-After deadline has not elapsed"
	for _, endpoint := range []struct{ name, path string }{{"search page", "/1/api"}, {"download", "/1/download"}} {
		for _, status := range []int{http.StatusTooManyRequests, http.StatusServiceUnavailable} {
			t.Run(fmt.Sprintf("%s %d", endpoint.name, status), func(t *testing.T) {
				synctest.Test(t, func(t *testing.T) {
					f := newSourceFixture(t)
					f.settings.IndexerIDs = []int{1}
					// A second listing must not cause a second request after the failure.
					f.prowlarr.listings = map[int][]string{1: {"Show.S01", "Show.S01.REPACK"}}
					f.prowlarr.respond = at(endpoint.path, func() *http.Response { return cooldownResponse(status, "7200") })

					run := f.begin(t, discovery.KindSearch)
					indexer := usableIndexers(t, run)[1]
					got := searchAll(t, run, indexer)
					assert.Empty(t, titles(run.Search(t.Context(), indexer, showQuery)), "the indexer must be out for the rest of the run")
					run.End()
					assert.Len(t, f.requestsTo(endpoint.path), 1)
					if endpoint.path == "/1/download" {
						assert.Equal(t, []string{"Show.S01"}, got, "the search must end when a download takes its indexer out")
						assert.Empty(t, run.Failures())
					} else {
						assert.Empty(t, got)
						assert.Len(t, run.Failures(), 1)
					}

					ids, failures := f.indexers(t, discovery.KindSearch)
					assert.Empty(t, ids)
					assert.Equal(t, []discovery.Failure{{IndexerID: 1, Reason: inCooldown}}, failures, "the cooldown must apply to the next run")

					f.restart(t)
					time.Sleep(time.Hour)
					ids, failures = f.indexers(t, discovery.KindSearch)
					assert.Empty(t, ids)
					assert.Equal(t, []discovery.Failure{{IndexerID: 1, Reason: inCooldown}}, failures, "the cooldown must hold across a restart")

					time.Sleep(time.Hour)
					run = f.begin(t, discovery.KindSearch)
					indexers := usableIndexers(t, run)
					require.Contains(t, indexers, 1, "the cooldown must end after its deadline")
					searchAll(t, run, indexers[1])
					assert.Len(t, f.requestsTo(endpoint.path), 2, "a run after the deadline must send the request again")
				})
			})
		}
	}
}

// TestSearch_ElapsedRetryAfterSkipsRestOfRun covers rule 2 for a download.
func TestSearch_ElapsedRetryAfterSkipsRestOfRun(t *testing.T) {
	// The header is made in the bubble, because the virtual clock starts at its own time.
	for name, header := range map[string]func() string{
		"seconds":   func() string { return "0" },
		"http date": func() string { return time.Now().Add(-time.Hour).UTC().Format(http.TimeFormat) },
	} {
		t.Run(name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				header := header()
				f := newSourceFixture(t)
				f.prowlarr.listings = map[int][]string{1: {"Show.S01", "Show.S01.REPACK"}}
				f.prowlarr.respond = at("/1/download", func() *http.Response { return cooldownResponse(http.StatusTooManyRequests, header) })

				run := f.begin(t, discovery.KindSearch)
				indexer := usableIndexers(t, run)[1]
				assert.Equal(t, []string{"Show.S01"}, searchAll(t, run, indexer))
				assert.Empty(t, titles(run.Search(t.Context(), indexer, showQuery)))
				run.End()
				require.Len(t, f.requestsTo("/1/download"), 1, "do not download more listings after a failure")
				require.Len(t, f.requestsTo("/1/api"), 1)

				run = f.begin(t, discovery.KindSearch)
				indexers := usableIndexers(t, run)
				assert.Empty(t, run.Failures())
				require.Contains(t, indexers, 1)
				searchAll(t, run, indexers[1])
				assert.Len(t, f.requestsTo("/1/download"), 2, "an elapsed deadline must not block a later run")
			})
		})
	}
}

// TestSearch_FailedPageTakesIndexerOutForRun covers rule 3.
func TestSearch_FailedPageTakesIndexerOutForRun(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		f := newSourceFixture(t)
		f.prowlarr.listings = map[int][]string{1: {"Show.S01"}, 2: {"Show.S01"}}
		f.prowlarr.respond = at("/1/api", func() *http.Response { return response(http.StatusBadRequest, "") })

		run := f.begin(t, discovery.KindSearch)
		indexers := usableIndexers(t, run)
		assert.Empty(t, titles(run.Search(t.Context(), indexers[1], showQuery)))
		assert.Empty(t, titles(run.Search(t.Context(), indexers[1], discovery.Query{Title: "Other", Season: 2})))
		assert.Equal(t, []string{"Show.S01"}, titles(run.Search(t.Context(), indexers[2], showQuery)), "other indexers must stay usable")
		run.End()
		assert.Equal(t, []discovery.Failure{{IndexerID: 1, Query: "Show S01", Reason: "Prowlarr returned HTTP 400"}}, run.Failures())
		assert.Len(t, f.requestsTo("/1/api"), 1)

		f.prowlarr.respond = nil
		run = f.begin(t, discovery.KindSearch)
		indexers = usableIndexers(t, run)
		assert.Empty(t, run.Failures(), "nothing must be stored")
		assert.Equal(t, []string{"Show.S01"}, titles(run.Search(t.Context(), indexers[1], showQuery)), "the next run must use the indexer again")
		assert.Len(t, f.requestsTo("/1/api"), 2)
	})
}

func TestSearch_DownloadCooldownOnLastPageAddsNoBudgetFailure(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		f := newSourceFixture(t)
		many := episodeTitles(25)
		f.prowlarr.listings = map[int][]string{1: many}
		// The last listing of page 10.
		f.prowlarr.respond = atTitle(many[19], func() *http.Response { return cooldownResponse(http.StatusTooManyRequests, "7200") })
		run := f.begin(t, discovery.KindSearch)

		got := searchAll(t, run, usableIndexers(t, run)[1])

		assert.Equal(t, many[:20], got)
		assert.Empty(t, run.Failures())
	})
}
