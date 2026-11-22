// Copyright (c) 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package discovery

import (
	"context"
	"errors"
	"slices"
	"time"

	"github.com/nuxencs/seasonpackarr/internal/prowlarr"
	"github.com/nuxencs/seasonpackarr/internal/state"
)

// Indexer is one Prowlarr indexer that a run can use.
type Indexer struct {
	ID       int
	prowlarr prowlarr.Indexer
}

// Query is the title and the season of one search.
type Query struct {
	Title  string
	Season int
}

// Failure is one failure of a run. IndexerID is 0 and Query is empty when the
// failure is not about one indexer or one query.
type Failure struct {
	IndexerID int
	Query     string
	Reason    string
}

// Run is one discovery run. It is the one place that knows which indexers are
// out and whether the database failed. A run is for one goroutine.
type Run struct {
	source     *Source
	kind       Kind
	indexerIDs []int
	// cooldowns holds the stored deadlines and the deadlines of this run, by
	// indexer id or state.ProwlarrCooldownID.
	cooldowns map[int]time.Time
	failures  []Failure
	err       error
	ended     bool
}

// Failures returns the failures of the run in time order.
func (r *Run) Failures() []Failure { return r.failures }

// Err returns ErrDatabase after a database failure of the run, the context
// error when a canceled context stopped a database operation, or nil.
func (r *Run) Err() error { return r.err }

// Indexers returns the indexers that are usable now, by priority and then by
// id. It applies the capability of the kind of the run, the indexer
// allowlist, and the cooldowns. It sends no request while Prowlarr as a whole
// is in cooldown. A failed read of the indexer list that is a cooldown error
// puts Prowlarr as a whole in cooldown.
func (r *Run) Indexers(ctx context.Context) []Indexer {
	if until := r.cooldowns[state.ProwlarrCooldownID]; time.Now().Before(until) {
		r.fail(Failure{Reason: "Prowlarr cooldown is still active"})
		return nil
	}
	all, err := r.source.client.Indexers(ctx)
	if err != nil {
		r.recordCooldown(ctx, state.ProwlarrCooldownID, err)
		r.fail(Failure{Reason: err.Error()})
		return nil
	}
	capability := r.kind.capability()
	capable := slices.DeleteFunc(all, func(indexer prowlarr.Indexer) bool { return !r.kind.supportedBy(indexer) })
	if len(r.indexerIDs) > 0 {
		for _, id := range r.indexerIDs {
			if !slices.ContainsFunc(capable, func(indexer prowlarr.Indexer) bool { return indexer.ID == id }) {
				r.fail(Failure{IndexerID: id, Reason: "selected indexer is missing, disabled, or not a " + capability + " torrent indexer"})
			}
		}
		capable = slices.DeleteFunc(capable, func(indexer prowlarr.Indexer) bool { return !slices.Contains(r.indexerIDs, indexer.ID) })
	}
	if len(capable) == 0 {
		r.fail(Failure{Reason: "no enabled " + capability + " torrent indexers in Prowlarr"})
		return nil
	}
	var usable []Indexer
	for _, indexer := range capable {
		if until := r.cooldowns[indexer.ID]; time.Now().Before(until) {
			r.fail(Failure{IndexerID: indexer.ID, Reason: "indexer cooldown or Retry-After deadline has not elapsed"})
			continue
		}
		usable = append(usable, Indexer{ID: indexer.ID, prowlarr: indexer})
	}
	return usable
}

func (r *Run) fail(failure Failure) { r.failures = append(r.failures, failure) }

// recordCooldown stores the deadline of a cooldown error. A deadline never
// moves to an earlier time. The run keeps a deadline also after it elapsed.
func (r *Run) recordCooldown(ctx context.Context, id int, err error) {
	cooldown, ok := errors.AsType[*prowlarr.CooldownError](err)
	if !ok || !cooldown.Until.After(r.cooldowns[id]) {
		return
	}
	r.cooldowns[id] = cooldown.Until
	if err := r.source.store.SetCooldown(ctx, id, cooldown.Until); err != nil {
		r.databaseFailure(err)
	}
}

// databaseFailure stops the run: Err reports the failure from now on.
func (r *Run) databaseFailure(err error) {
	r.err = r.source.stateError(err)
	r.fail(Failure{Reason: r.err.Error()})
}
