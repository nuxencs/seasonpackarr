// Copyright (c) 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package discovery

import (
	"cmp"
	"context"
	"slices"
	"time"

	"github.com/nuxencs/seasonpackarr/internal/state"

	"github.com/autobrr/rls"
)

// waitTime is the time after an answered query of a search group to one
// indexer for one client, during which no search sends that query again.
const waitTime = 24 * time.Hour

// Group is one search group of a search: its query and the clients whose
// inventories hold its episodes.
type Group struct {
	Query   Query
	Clients []string
}

// PlanRequest is the input of Plan.
type PlanRequest struct {
	Groups []Group
	// Limit is the maximum count of due search groups that the run queries.
	// 0 means no cap.
	Limit int
	// Preview is true for a dry run, with or without verify. The run then
	// obeys the wait time but stores no search time.
	Preview bool
}

// Plan is the work of a search run.
type Plan struct {
	// Groups holds the due search groups in query order.
	Groups []PlannedGroup
	// NotDue counts the search groups with no due query on a usable indexer.
	NotDue int
	// Deferred counts the due search groups that the limit left for a later run.
	Deferred int
}

// PlannedGroup is one due search group and its due queries, in indexer order.
type PlannedGroup struct {
	Query   Query
	Queries []PlannedQuery
}

// PlannedQuery is the query of a search group to one indexer. Clients holds
// the clients that are due on the indexer, by name. Only they evaluate the
// listings of the query.
type PlannedQuery struct {
	Indexer Indexer
	Clients []string
}

// queryKey identifies a query of the plan of a run.
type queryKey struct {
	group     state.SearchGroup
	indexerID int
}

// searchGroup is the key of the search group of query. The search code groups
// by the normalized title, so each title of a group gives the same key.
func searchGroup(query Query) state.SearchGroup {
	return state.SearchGroup{Title: rls.MustNormalize(query.Title), Season: query.Season}
}

// Plan returns the work of a search run. It calls Indexers, so its failures
// are the failures of the run. A query of a search group to an indexer is due
// when at least one client of the group has no search time on the indexer
// or a search time that is waitTime old or older. A group with no due query
// on a usable indexer counts as not due. Plan sends no request without groups.
//
// The due groups that waited longest go first: a group with a due client that
// was never searched, then the group with the oldest search time of a due
// client. The query text breaks a tie. The groups after the limit count as
// deferred.
//
// The run then stores the search times of the planned queries that Search
// answers, unless the request is a preview.
func (r *Run) Plan(ctx context.Context, request PlanRequest) Plan {
	r.preview = request.Preview
	r.planned = make(map[queryKey][]string)
	if len(request.Groups) == 0 {
		return Plan{}
	}
	indexers := r.Indexers(ctx)
	now := time.Now()
	var times map[state.SearchTarget]time.Time
	if len(indexers) > 0 {
		var err error
		if times, err = r.source.store.SearchTimes(ctx, now); err != nil {
			r.databaseFailure(err)
			return Plan{}
		}
	}
	type dueGroup struct {
		PlannedGroup
		// oldest is the oldest search time of a due client. It is zero when a due client was never searched.
		oldest time.Time
	}
	var (
		plan Plan
		due  []dueGroup
	)
	for _, group := range request.Groups {
		key := searchGroup(group.Query)
		clients := slices.Sorted(slices.Values(group.Clients))
		planned := dueGroup{Query: group.Query, oldest: now}
		for _, indexer := range indexers {
			var dueClients []string
			for _, client := range clients {
				searched, ok := times[state.SearchTarget{Group: key, Client: client, IndexerID: indexer.ID}]
				if !ok || now.Sub(searched) >= waitTime {
					dueClients = append(dueClients, client)
					// A missing time is the zero time, so it sorts first.
					if searched.Before(planned.oldest) {
						planned.oldest = searched
					}
				}
			}
			if len(dueClients) > 0 {
				planned.Queries = append(planned.Queries, PlannedQuery{Indexer: indexer, Clients: dueClients})
			}
		}
		if len(planned.Queries) == 0 {
			plan.NotDue++
			continue
		}
		due = append(due, planned)
	}
	slices.SortFunc(due, func(a, b dueGroup) int {
		return cmp.Or(a.oldest.Compare(b.oldest), cmp.Compare(a.Query.String(), b.Query.String()))
	})
	if request.Limit > 0 && len(due) > request.Limit {
		plan.Deferred = len(due) - request.Limit
		due = due[:request.Limit]
	}
	for _, group := range due {
		// Only the queries of the run are in the plan, so Search stores no time for a deferred group.
		key := searchGroup(group.Query)
		for _, query := range group.Queries {
			r.planned[queryKey{key, query.Indexer.ID}] = query.Clients
		}
		plan.Groups = append(plan.Groups, group.PlannedGroup)
	}
	return plan
}

// searched stores the search time of each due client of a planned query that
// ended with an answer. A preview, a query outside the plan, a canceled
// context, and an indexer that went out store nothing.
func (r *Run) searched(ctx context.Context, indexerID int, query Query) {
	key := queryKey{searchGroup(query), indexerID}
	clients := r.planned[key]
	if r.preview || len(clients) == 0 || !r.usable(ctx, indexerID) {
		return
	}
	delete(r.planned, key)
	if err := r.source.store.SetSearchTimes(ctx, key.group, indexerID, clients, time.Now()); err != nil {
		r.databaseFailure(err)
	}
}
