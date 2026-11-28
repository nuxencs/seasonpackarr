// Copyright (c) 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package http

import (
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/require"
)

func TestSearch_WaitTimeSkipsQueriedGroupsFor24Hours(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		log := newTraceLogger(t)
		f := newSearchFixture(t, fixtureOptions{packEpisodes: 1, clientEpisodes: 1, threshold: 1, log: log})
		// The feed has no pack, so the import run imports nothing and the group stays.
		f.titles = nil

		first := f.searchFor(t, SearchRequest{})
		require.Empty(t, first.Failures)
		require.Equal(t, []string{"/1/api", "/2/api"}, f.queryPaths)
		require.Zero(t, first.GroupsNotDue)

		f.queryPaths = nil
		log.Reset()
		time.Sleep(24*time.Hour - time.Second)
		second := f.searchFor(t, SearchRequest{})
		require.Empty(t, second.Failures)
		require.Empty(t, f.queryPaths, "a group that an import run queried must wait 24 hours")
		require.Zero(t, second.Requests)
		require.Equal(t, 1, second.Groups)
		require.Equal(t, 1, second.GroupsNotDue)
		require.EqualValues(t, 1, log.Events(t).Require(t, "Prowlarr run completed")["groups_not_due"])

		time.Sleep(time.Second)
		third := f.searchFor(t, SearchRequest{})
		require.Empty(t, third.Failures)
		require.Equal(t, []string{"/1/api", "/2/api"}, f.queryPaths, "the group must be due again after 24 hours")
		require.Zero(t, third.GroupsNotDue)
	})
}

func TestSearch_PreviewObeysWaitTimeAndStoresNothing(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		f := newSearchFixture(t, fixtureOptions{packEpisodes: 1, clientEpisodes: 1, threshold: 1})
		f.titles = nil
		for _, mode := range []struct {
			name           string
			dryRun, verify bool
		}{{"dry run", true, false}, {"verify", true, true}, {"import", false, false}} {
			f.queryPaths = nil
			report := f.searchFor(t, SearchRequest{DryRun: mode.dryRun, Verify: mode.verify})
			require.Equal(t, []string{"/1/api", "/2/api"}, f.queryPaths, "%s after a preview must send the queries of the preview", mode.name)
			require.Zero(t, report.GroupsNotDue, mode.name)
		}
		for _, verify := range []bool{false, true} {
			f.queryPaths = nil
			report := f.searchFor(t, SearchRequest{DryRun: true, Verify: verify})
			require.Empty(t, f.queryPaths, "a preview with verify %t must skip what an import run skips", verify)
			require.Equal(t, 1, report.GroupsNotDue)
		}
	})
}

func TestSearch_WaitTimeIsPerClient(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		f := newSearchFixture(t, fixtureOptions{packEpisodes: 1, clientEpisodes: 1, threshold: 1})
		cfg := f.config.Snapshot()
		alias := cfg.Clients["default"].Clone()
		alias.Import.Category = "other-category"
		cfg.Clients["alias"] = &alias
		f.config.Store(cfg)
		f.titles = nil
		f.searchFor(t, SearchRequest{ClientName: "default"})

		f.titles = []string{f.releaseName}
		f.queryPaths = nil
		preview := f.searchFor(t, SearchRequest{DryRun: true})
		require.Equal(t, []string{"/1/api", "/2/api"}, f.queryPaths, "a run for one client must not make another client skip")
		require.NotEmpty(t, preview.Outcomes)
		for _, outcome := range preview.Outcomes {
			require.Equal(t, "alias", outcome.ClientName, "only the due client evaluates the listings")
		}

		f.titles = nil
		f.searchFor(t, SearchRequest{ClientName: "alias"})
		f.queryPaths = nil
		report := f.searchFor(t, SearchRequest{})
		require.Empty(t, f.queryPaths)
		require.Equal(t, 1, report.GroupsNotDue)
	})
}

func TestSearch_NewIndexerIsDue(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		f := newSearchFixture(t, fixtureOptions{packEpisodes: 1, clientEpisodes: 1, threshold: 1})
		f.titles = nil
		cfg := f.config.Snapshot()
		cfg.Search.IndexerIDs = []int{1}
		f.config.Store(cfg)
		f.searchFor(t, SearchRequest{})
		require.Equal(t, []string{"/1/api"}, f.queryPaths)

		cfg.Search.IndexerIDs = nil
		f.config.Store(cfg)
		f.queryPaths = nil
		report := f.searchFor(t, SearchRequest{})
		require.Equal(t, []string{"/2/api"}, f.queryPaths, "a new indexer must get the query at once")
		require.Zero(t, report.GroupsNotDue)
	})
}

func TestSearch_FailedQueryStoresNoSearchTime(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		f := newSearchFixture(t, fixtureOptions{packEpisodes: 1, clientEpisodes: 1, threshold: 1})
		f.titles = nil
		f.failFirst = true
		first := f.searchFor(t, SearchRequest{})
		require.Equal(t, []string{"/1/api", "/2/api"}, f.queryPaths)
		require.Len(t, first.Failures, 1)

		f.queryPaths = nil
		inCooldown := f.searchFor(t, SearchRequest{})
		require.Empty(t, f.queryPaths, "indexer 1 is in a cooldown, and indexer 2 is not due")
		require.Equal(t, 1, inCooldown.GroupsNotDue)

		// The fallback cooldown of a 429 without Retry-After is ten minutes.
		time.Sleep(10 * time.Minute)
		f.failFirst = false
		f.queryPaths = nil
		report := f.searchFor(t, SearchRequest{})
		require.Empty(t, report.Failures)
		require.Equal(t, []string{"/1/api"}, f.queryPaths, "a failed query must store no search time")
		require.Zero(t, report.GroupsNotDue)
	})
}

func TestRSS_IgnoresWaitTime(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		f := newSearchFixture(t, fixtureOptions{packEpisodes: 1, clientEpisodes: 1, threshold: 1})
		f.titles = nil
		poll := f.runRSS(t)
		require.Equal(t, 2, poll.Requests)

		f.queryPaths = nil
		search := f.searchFor(t, SearchRequest{})
		require.Equal(t, []string{"/1/api", "/2/api"}, f.queryPaths, "an RSS poll must store no search time")
		require.Zero(t, search.GroupsNotDue)

		next := f.runRSS(t)
		require.Equal(t, 2, next.Requests, "an RSS poll must skip no group that is not due")
		require.Zero(t, next.GroupsNotDue)
	})
}
