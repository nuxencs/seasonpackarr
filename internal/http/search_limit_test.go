// Copyright (c) 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package http

import (
	"encoding/json"
	"fmt"
	"slices"
	"testing"
	"testing/synctest"
	"time"

	"github.com/nuxencs/seasonpackarr/internal/torrentclient"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// addSearchGroups adds count search groups, "Aaa001 S01" and up, each with one
// episode in the client. They sort before "Lifecycle S01" of the fixture.
// It also limits the search to indexer 1, so each search group gets one query.
func (f *searchFixture) addSearchGroups(count int) {
	for i := range count {
		name := fmt.Sprintf("Aaa%03d.S01E01.1080p.WEB-DL.H.264-RlsGrp", i+1)
		f.torrentClient.Torrents = append(f.torrentClient.Torrents, torrentclient.Torrent{Name: name, Hash: fmt.Sprintf("group%d", i+1)})
	}
	cfg := f.config.Snapshot()
	cfg.Search.IndexerIDs = []int{1}
	f.config.Store(cfg)
}

func TestSearch_DefaultLimitReachesEachGroupInThreeRuns(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		log := newTraceLogger(t)
		f := newSearchFixture(t, fixtureOptions{packEpisodes: 1, clientEpisodes: 1, threshold: 1, log: log})
		f.titles = nil
		f.addSearchGroups(249)

		queried := make(map[string]int)
		for _, want := range []struct{ queries, notDue, deferred int }{
			{queries: 100, notDue: 0, deferred: 150},
			{queries: 100, notDue: 100, deferred: 50},
			{queries: 50, notDue: 200, deferred: 0},
		} {
			f.queries = nil
			log.Reset()
			report := f.searchFor(t, SearchRequest{})
			require.Empty(t, report.Failures)
			require.Len(t, f.queries, want.queries)
			require.Equal(t, 250, report.Groups)
			require.Equal(t, 100, report.Limit)
			require.Equal(t, want.notDue, report.GroupsNotDue, "a group that is not due must not count against the limit")
			require.Equal(t, want.deferred, report.GroupsDeferred)
			completed := log.Events(t).Require(t, "Prowlarr run completed")
			require.EqualValues(t, 100, completed["limit"])
			require.EqualValues(t, want.deferred, completed["groups_deferred"])
			for _, query := range f.queries {
				queried[query]++
			}
		}
		require.Len(t, queried, 250)
		for query, count := range queried {
			require.Equal(t, 1, count, "three import runs must query %q one time", query)
		}
		require.Equal(t, "Lifecycle S01", f.queries[len(f.queries)-1], "the last run queries in query text order")
	})
}

func TestSearch_LimitSetsTheCap(t *testing.T) {
	for name, tt := range map[string]struct {
		body     string
		used     int
		queries  int
		deferred int
	}{
		"custom":  {body: `{"limit":2}`, used: 2, queries: 2, deferred: 1},
		"no cap":  {body: `{"limit":0}`, used: 0, queries: 3},
		"omitted": {body: `{}`, used: 100, queries: 3},
	} {
		t.Run(name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				f := newSearchFixture(t, fixtureOptions{packEpisodes: 1, clientEpisodes: 1, threshold: 1})
				f.titles = nil
				f.addSearchGroups(2)

				response := f.postRaw(t, "/api/search", []byte(tt.body), processorTestToken)
				require.Equal(t, 200, response.Code, response.Body.String())
				var report searchReport
				require.NoError(t, json.Unmarshal(response.Body.Bytes(), &report))

				assert.Len(t, f.queries, tt.queries)
				assert.Equal(t, tt.deferred, report.GroupsDeferred)
				assert.Equal(t, tt.used, report.Limit)
			})
		})
	}
}

func TestSearch_InvalidLimitIsBadRequest(t *testing.T) {
	f := newSearchFixture(t, fixtureOptions{packEpisodes: 1, clientEpisodes: 1, threshold: 1})
	for _, body := range []string{`{"limit":-1}`, `{"limit":1.5}`, `{"limit":"1"}`} {
		response := f.postRaw(t, "/api/search", []byte(body), processorTestToken)
		assert.Equal(t, 400, response.Code, body)
	}
	require.Empty(t, f.queries)
	require.Zero(t, f.discoveryCalls)
}

func TestSearch_PreviewTakesTheGroupsOfTheNextImportRun(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		f := newSearchFixture(t, fixtureOptions{packEpisodes: 1, clientEpisodes: 1, threshold: 1})
		f.titles = nil
		f.addSearchGroups(3)
		f.searchFor(t, SearchRequest{Limit: new(1)})
		time.Sleep(time.Hour)

		var previews [][]string
		for _, verify := range []bool{false, true} {
			f.queries = nil
			report := f.searchFor(t, SearchRequest{DryRun: true, Verify: verify, Limit: new(2)})
			require.Equal(t, 1, report.GroupsNotDue)
			require.Equal(t, 1, report.GroupsDeferred)
			previews = append(previews, slices.Clone(f.queries))
		}
		f.queries = nil
		f.searchFor(t, SearchRequest{Limit: new(2)})

		require.Equal(t, []string{"Aaa002 S01", "Aaa003 S01"}, f.queries)
		require.Equal(t, [][]string{f.queries, f.queries}, previews)
	})
}

func TestSearch_GroupWithNoQueryUsesNoCap(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		f := newSearchFixture(t, fixtureOptions{packEpisodes: 1, clientEpisodes: 1, threshold: 1})
		f.titles = nil
		f.addSearchGroups(2)
		// The first query gets a 429, so indexer 1, the only indexer, is in a cooldown for the rest of the run.
		f.failFirst = true

		report := f.searchFor(t, SearchRequest{Limit: new(2)})

		require.Equal(t, []string{"Aaa001 S01"}, f.queries)
		require.Len(t, report.Failures, 1)
		require.Equal(t, 2, report.GroupsNotDue, "a group whose indexers went out sends no query and uses no cap")
		require.Zero(t, report.GroupsDeferred)
	})
}
