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

// plannedQueries returns the planned queries of plan by query text, each as
// "indexer 2 a,b" for the due clients a and b on indexer 2.
func plannedQueries(plan discovery.Plan) map[string][]string {
	queries := make(map[string][]string)
	for _, group := range plan.Groups {
		for _, query := range group.Queries {
			text := fmt.Sprintf("indexer %d %s", query.Indexer.ID, strings.Join(query.Clients, ","))
			queries[group.Query.String()] = append(queries[group.Query.String()], text)
		}
	}
	return queries
}

// searchPlan sends each planned query of plan and gets the torrent of each
// listing, as an import run does.
func searchPlan(ctx context.Context, run *discovery.Run, plan discovery.Plan) {
	for _, group := range plan.Groups {
		for _, query := range group.Queries {
			for listing := range run.Search(ctx, query.Indexer, group.Query) {
				_, _ = listing.Torrent(ctx)
			}
		}
	}
}

// importRun plans the groups in an import run and sends each planned query.
func (f *sourceFixture) importRun(t *testing.T, groups ...discovery.Group) discovery.Plan {
	t.Helper()
	run, err := f.source.Begin(t.Context(), f.settings, discovery.KindSearch)
	require.NoError(t, err)
	defer run.End()
	plan := run.Plan(t.Context(), discovery.PlanRequest{Groups: groups})
	searchPlan(t.Context(), run, plan)
	require.NoError(t, run.Err())
	return plan
}

// preview plans the groups in a preview and sends no query.
func (f *sourceFixture) preview(t *testing.T, groups ...discovery.Group) discovery.Plan {
	t.Helper()
	run, err := f.source.Begin(t.Context(), f.settings, discovery.KindSearch)
	require.NoError(t, err)
	defer run.End()
	return run.Plan(t.Context(), discovery.PlanRequest{Groups: groups, Preview: true})
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
		searchPlan(t.Context(), run, run.Plan(t.Context(), discovery.PlanRequest{Groups: []discovery.Group{showGroup}, Preview: true}))
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
		plan := run.Plan(t.Context(), discovery.PlanRequest{Groups: []discovery.Group{showGroup, otherGroup}})
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		// "Other S02" goes first. The run cancels during the last listing of "Show S01" on indexer 2.
		for _, group := range plan.Groups {
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

	plan := run.Plan(t.Context(), discovery.PlanRequest{Groups: []discovery.Group{showGroup}})

	assert.Empty(t, plan.Groups)
	assert.ErrorIs(t, run.Err(), discovery.ErrDatabase)
	assert.Empty(t, f.requestsTo("/1/api"))
}

// groupOrder returns the query text of each search group of plan, in order.
func groupOrder(plan discovery.Plan) []string {
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
			plan := run.Plan(t.Context(), discovery.PlanRequest{Groups: groups, Limit: tt.limit, Preview: true})
			run.End()

			assert.Equal(t, tt.order, groupOrder(plan), "limit %d", tt.limit)
			assert.Equal(t, tt.deferred, plan.Deferred, "limit %d", tt.limit)
			assert.Equal(t, 1, plan.NotDue, "a group that is not due must not count against limit %d", tt.limit)
		}
	})
}
