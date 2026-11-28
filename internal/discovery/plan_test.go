// Copyright (c) 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package discovery_test

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/nuxencs/seasonpackarr/internal/discovery"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// otherQuery is a second search group of the plan tests.
var otherQuery = discovery.Query{Title: "Other", Season: 2}

// showGroup is the search group of showQuery for client "a".
var showGroup = discovery.Group{Query: showQuery, Clients: []string{"a"}}

// plan is what a caller sees of Plan: the search groups of the sequence and
// the counts of Stats.
type plan struct {
	Groups           []discovery.PlannedGroup
	NotDue, Deferred int
}

// plannedQueries returns the planned queries of plan by query text, each as
// "indexer 2 a,b" for the due clients a and b on indexer 2.
func plannedQueries(plan plan) map[string][]string {
	queries := make(map[string][]string)
	for _, group := range plan.Groups {
		for _, query := range group.Queries {
			text := fmt.Sprintf("indexer %d %s", query.Indexer.ID, strings.Join(query.Clients, ","))
			queries[group.Query.String()] = append(queries[group.Query.String()], text)
		}
	}
	return queries
}

// collect ranges the plan of request. With search, it sends each planned
// query and gets the torrent of each listing, as an import run does.
func collect(ctx context.Context, run *discovery.Run, request discovery.PlanRequest, search bool) plan {
	var result plan
	for group := range run.Plan(ctx, request) {
		result.Groups = append(result.Groups, group)
		if !search {
			continue
		}
		for _, query := range group.Queries {
			for listing := range run.Search(ctx, query.Indexer, group.Query) {
				_, _ = listing.Torrent(ctx)
			}
		}
	}
	stats := run.Stats()
	result.NotDue, result.Deferred = stats.GroupsNotDue, stats.GroupsDeferred
	return result
}

// importRun plans the groups in an import run and sends each planned query.
func (f *sourceFixture) importRun(t *testing.T, groups ...discovery.Group) plan {
	t.Helper()
	run, err := f.source.Begin(t.Context(), f.settings, discovery.KindSearch)
	require.NoError(t, err)
	defer run.End()
	result := collect(t.Context(), run, discovery.PlanRequest{Groups: groups}, true)
	require.NoError(t, run.Err())
	return result
}

// preview plans the groups in a preview and sends no query.
func (f *sourceFixture) preview(t *testing.T, groups ...discovery.Group) plan {
	t.Helper()
	run, err := f.source.Begin(t.Context(), f.settings, discovery.KindSearch)
	require.NoError(t, err)
	defer run.End()
	return collect(t.Context(), run, discovery.PlanRequest{Groups: groups, Preview: true}, false)
}

func TestPlan_GroupsInQueryOrderWithDueClients(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		f := newSourceFixture(t)
		f.importRun(t, discovery.Group{Query: showQuery, Clients: []string{"b"}})

		plan := f.preview(t, discovery.Group{Query: showQuery, Clients: []string{"b", "a"}}, discovery.Group{Query: otherQuery, Clients: []string{"a"}})

		require.Len(t, plan.Groups, 2)
		assert.Equal(t, otherQuery, plan.Groups[0].Query)
		assert.Equal(t, showQuery, plan.Groups[1].Query)
		assert.Equal(t, map[string][]string{
			"Other S02": {"indexer 2 a", "indexer 1 a"},
			"Show S01":  {"indexer 2 a", "indexer 1 a"},
		}, plannedQueries(plan), "client b is not due on either indexer")
		assert.Zero(t, plan.NotDue)
	})
}

func TestPlan_NoGroupsSendsNoRequest(t *testing.T) {
	f := newSourceFixture(t)
	plan := f.preview(t)
	assert.Empty(t, plan.Groups)
	assert.Zero(t, plan.NotDue)
	assert.Zero(t, f.requestCount())
}

func TestPlan_AnsweredQueriesWaitOneDay(t *testing.T) {
	budget := episodeTitles(25)
	for name, tt := range map[string]struct {
		listings map[int][]string
		respond  func(*http.Request) *http.Response
	}{
		"short page":    {listings: map[int][]string{1: {"Show.S01"}}},
		"page budget":   {listings: map[int][]string{1: budget}},
		"repeated page": {listings: map[int][]string{1: {"A", "B"}}, respond: repeatFirstPage},
	} {
		t.Run(name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				f := newSourceFixture(t)
				f.settings.IndexerIDs = []int{1}
				f.prowlarr.listings = tt.listings
				f.prowlarr.respond = tt.respond
				f.importRun(t, showGroup)

				time.Sleep(24*time.Hour - time.Second)
				plan := f.preview(t, showGroup)
				assert.Empty(t, plan.Groups, "an answered query must wait 24 hours")
				assert.Equal(t, 1, plan.NotDue)

				time.Sleep(time.Second)
				plan = f.preview(t, showGroup)
				assert.Equal(t, map[string][]string{"Show S01": {"indexer 1 a"}}, plannedQueries(plan))
				assert.Zero(t, plan.NotDue)
			})
		})
	}
}

// repeatFirstPage gives the first page of indexer 1 at each offset.
func repeatFirstPage(r *http.Request) *http.Response {
	if r.URL.Path != "/1/api" {
		return nil
	}
	return feedResponse(1, "A", "B")
}

func TestPlan_SearchTimesSurviveRestart(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		f := newSourceFixture(t)
		f.importRun(t, showGroup)
		f.restart(t)

		plan := f.preview(t, showGroup)

		assert.Empty(t, plan.Groups)
		assert.Equal(t, 1, plan.NotDue)
	})
}

func TestPlan_ConnectionChangeClearsSearchTimes(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		f := newSourceFixture(t)
		f.importRun(t, showGroup)
		f.prowlarr.apiKey = "prowlarr-other-key"
		f.settings.APIKey = "prowlarr-other-key"

		plan := f.preview(t, showGroup)

		assert.Equal(t, map[string][]string{"Show S01": {"indexer 2 a", "indexer 1 a"}}, plannedQueries(plan))
	})
}

func TestPlan_FailedQueryStoresNoSearchTime(t *testing.T) {
	for name, respond := range map[string]func(*http.Request) *http.Response{
		// A failed page that is not a cooldown takes the indexer out for the run only.
		"failed page": at("/2/api", func() *http.Response { return response(http.StatusBadRequest, "") }),
		// A cooldown of a download takes the indexer out during the query.
		"download cooldown": atTitle("Show.S01", func() *http.Response { return cooldownResponse(http.StatusTooManyRequests, "60") }),
	} {
		t.Run(name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				f := newSourceFixture(t)
				f.prowlarr.listings = map[int][]string{2: {"Show.S01"}}
				f.prowlarr.respond = respond
				f.importRun(t, showGroup)
				time.Sleep(time.Minute)
				f.prowlarr.respond = nil

				plan := f.preview(t, showGroup)

				assert.Equal(t, map[string][]string{"Show S01": {"indexer 2 a"}}, plannedQueries(plan))
			})
		})
	}
}

func TestPlan_PreviewStoresNoSearchTime(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		f := newSourceFixture(t)
		run := f.begin(t, discovery.KindSearch)
		collect(t.Context(), run, discovery.PlanRequest{Groups: []discovery.Group{showGroup}, Preview: true}, true)
		run.End()

		plan := f.preview(t, showGroup)

		assert.Equal(t, map[string][]string{"Show S01": {"indexer 2 a", "indexer 1 a"}}, plannedQueries(plan))
	})
}

func TestPlan_CanceledRunKeepsFinishedQueries(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		f := newSourceFixture(t)
		f.prowlarr.listings = map[int][]string{1: {"Show.S01"}, 2: {"Show.S01"}}
		otherGroup := discovery.Group{Query: otherQuery, Clients: []string{"a"}}
		run := f.begin(t, discovery.KindSearch)
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		// "Other S02" goes first. The run cancels during the last listing of "Show S01" on indexer 2.
		for group := range run.Plan(ctx, discovery.PlanRequest{Groups: []discovery.Group{showGroup, otherGroup}}) {
			for _, query := range group.Queries {
				for range run.Search(ctx, query.Indexer, group.Query) {
					if group.Query == showQuery {
						cancel()
					}
				}
			}
		}
		assert.Empty(t, run.Failures(), "the caller reports a canceled context")
		assert.NoError(t, run.Err())
		run.End()

		next := f.preview(t, showGroup, otherGroup)

		assert.Equal(t, map[string][]string{"Show S01": {"indexer 2 a", "indexer 1 a"}}, plannedQueries(next),
			"a canceled query must store no search time, and the finished queries must keep theirs")
		assert.Equal(t, 1, next.NotDue)
	})
}

func TestPlan_DatabaseFailure(t *testing.T) {
	f := newSourceFixture(t)
	run := f.begin(t, discovery.KindSearch)
	require.NoError(t, f.store.Close())

	plan := collect(t.Context(), run, discovery.PlanRequest{Groups: []discovery.Group{showGroup}}, true)

	assert.Empty(t, plan.Groups)
	assert.ErrorIs(t, run.Err(), discovery.ErrDatabase)
	assert.Empty(t, f.requestsTo("/1/api"))
}

// groupOrder returns the query text of each search group of plan, in order.
func groupOrder(plan plan) []string {
	var order []string
	for _, group := range plan.Groups {
		order = append(order, group.Query.String())
	}
	return order
}

func TestPlan_NeverSearchedGroupsFirstThenOldestSearchTime(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		f := newSourceFixture(t)
		alpha := discovery.Group{Query: discovery.Query{Title: "Alpha", Season: 1}, Clients: []string{"a"}}
		beta := discovery.Group{Query: discovery.Query{Title: "Beta", Season: 1}, Clients: []string{"a"}}
		gamma := discovery.Group{Query: discovery.Query{Title: "Gamma", Season: 1}, Clients: []string{"a"}}
		delta := discovery.Group{Query: discovery.Query{Title: "Delta", Season: 1}, Clients: []string{"a"}}
		f.importRun(t, gamma)
		time.Sleep(time.Hour)
		f.importRun(t, beta, delta)
		time.Sleep(24 * time.Hour)
		// Client b was never searched for delta, so the oldest due triple of delta has no search time.
		delta.Clients = []string{"a", "b"}

		plan := f.preview(t, alpha, beta, gamma, delta)

		assert.Equal(t, []string{"Alpha S01", "Delta S01", "Gamma S01", "Beta S01"}, groupOrder(plan))
	})
}

func TestPlan_LimitDefersDueGroups(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		f := newSourceFixture(t)
		groups := []discovery.Group{showGroup}
		for _, title := range []string{"Alpha", "Beta", "Gamma"} {
			groups = append(groups, discovery.Group{Query: discovery.Query{Title: title, Season: 1}, Clients: []string{"a"}})
		}
		f.importRun(t, showGroup)

		for _, tt := range []struct {
			limit    int
			order    []string
			deferred int
		}{
			{limit: 2, order: []string{"Alpha S01", "Beta S01"}, deferred: 1},
			{limit: 3, order: []string{"Alpha S01", "Beta S01", "Gamma S01"}},
			{limit: 0, order: []string{"Alpha S01", "Beta S01", "Gamma S01"}},
		} {
			run := f.begin(t, discovery.KindSearch)
			plan := collect(t.Context(), run, discovery.PlanRequest{Groups: groups, Limit: tt.limit, Preview: true}, true)
			run.End()

			assert.Equal(t, tt.order, groupOrder(plan), "limit %d", tt.limit)
			assert.Equal(t, tt.deferred, plan.Deferred, "limit %d", tt.limit)
			assert.Equal(t, 1, plan.NotDue, "a group that is not due must not count against limit %d", tt.limit)
		}
	})
}

func TestPlan_GroupWithNoQueryUsesNoLimit(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		f := newSourceFixture(t)
		group := func(title string) discovery.Group {
			return discovery.Group{Query: discovery.Query{Title: title, Season: 1}, Clients: []string{"a"}}
		}
		// Beta is due on indexer 2 only.
		f.settings.IndexerIDs = []int{1}
		f.importRun(t, group("Beta"))
		f.settings.IndexerIDs = nil
		// The query of Alpha on indexer 2 fails, so indexer 2 is out for the rest of the run.
		f.prowlarr.respond = at("/2/api", func() *http.Response { return response(http.StatusBadRequest, "") })

		run := f.begin(t, discovery.KindSearch)
		plan := collect(t.Context(), run, discovery.PlanRequest{Groups: []discovery.Group{group("Alpha"), group("Beta"), group("Delta"), group("Gamma")}, Limit: 2}, true)
		run.End()

		assert.Equal(t, []string{"Alpha S01", "Delta S01"}, groupOrder(plan))
		assert.Equal(t, map[string][]string{"Alpha S01": {"indexer 2 a", "indexer 1 a"}, "Delta S01": {"indexer 1 a"}}, plannedQueries(plan),
			"a group goes out with the queries on the indexers that are still usable")
		assert.Equal(t, 1, plan.NotDue, "beta sends no query, so it uses no limit")
		assert.Equal(t, 1, plan.Deferred)
	})
}
