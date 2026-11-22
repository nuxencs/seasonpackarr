// Copyright (c) 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package discovery

import (
	"context"
	"iter"
	"time"

	"github.com/nuxencs/seasonpackarr/internal/prowlarr"
	"github.com/nuxencs/seasonpackarr/internal/state"
)

// checkpointSize is the number of listings of the first page that a checkpoint keeps.
const checkpointSize = 100

// gapReason is the failure text of a poll that could not read back to its checkpoint.
const gapReason = "RSS feed did not overlap the previous poll; releases may be missing; use a manual targeted search"

// ClientUnreadable tells the run that a client was not readable. The run then
// keeps the feed state in memory and saves nothing, so the client misses no
// listing. Call it before the first Feed.
func (r *Run) ClientUnreadable() { r.skipFeedSave = true }

// Feed returns the listings of the RSS feed of indexer that relevant accepts.
// Before it yields, it reads the feed back to the checkpoint of the previous
// poll, sets the checkpoint from the first page, and saves the feed state.
// A first poll of an indexer reads one page. A poll that cannot reach the
// checkpoint adds a gap failure.
//
// The sequence yields the new listings in feed order, then the retained
// listings, each one time. A failed page keeps the previous checkpoint, and the
// sequence yields nothing. A canceled context saves no checkpoint. The
// sequence yields nothing for an indexer that is out, and it ends when the
// indexer goes out or the database fails.
func (r *Run) Feed(ctx context.Context, indexer Indexer, relevant func(title string) bool) iter.Seq[*Listing] {
	return func(yield func(*Listing) bool) {
		if !r.usable(ctx, indexer.ID) {
			return
		}
		feeds := r.loadFeeds(ctx)
		if feeds == nil {
			return
		}
		fresh, ok := r.readFeed(ctx, indexer, relevant, feeds)
		if !ok || ctx.Err() != nil || !r.saveFeeds(ctx) {
			return
		}
		// Retained listings follow the feed. They include packs that a client
		// could not use before their episodes finished downloading.
		yielded := make(map[state.Key]bool)
		for _, result := range append(fresh, feeds.Results(indexer.ID)...) {
			if !r.usable(ctx, indexer.ID) {
				return
			}
			key := state.RSSIdentity(indexer.ID, result)
			if yielded[key] || !relevant(result.Title) {
				continue
			}
			yielded[key] = true
			if !yield(&Listing{run: r, indexerID: indexer.ID, result: result}) {
				return
			}
		}
	}
}

// readFeed reads the pages of the feed back to the checkpoint, retains the
// relevant listings, and sets the checkpoint from the first page. It returns
// the new relevant listings in feed order. After a failed page, it returns
// false and keeps the previous checkpoint.
func (r *Run) readFeed(ctx context.Context, indexer Indexer, relevant func(string) bool, feeds *state.Feeds) ([]prowlarr.Result, bool) {
	previous := feeds.Checkpoint(indexer.ID)
	firstPage := make(map[state.Key]bool)
	seen := make(map[state.Key]bool)
	var fresh []prowlarr.Result
	offset := 0
	for page := range pageBudget {
		r.stats.FeedPages++
		results, limit, err := r.source.client.RSSPage(ctx, indexer.prowlarr, offset)
		if err != nil {
			r.recordCooldown(ctx, indexer.ID, err)
			r.fail(Failure{IndexerID: indexer.ID, Reason: err.Error()})
			return nil, false
		}
		overlap, added := false, 0
		for _, result := range results {
			key := state.RSSIdentity(indexer.ID, result)
			if page == 0 && len(firstPage) < checkpointSize {
				firstPage[key] = true
			}
			overlap = overlap || previous[key]
			if seen[key] {
				continue
			}
			seen[key] = true
			added++
			if relevant(result.Title) {
				fresh = append(fresh, result)
				feeds.Remember(key, result, time.Now())
			}
		}
		if len(previous) == 0 || overlap {
			break
		}
		if !indexer.prowlarr.SupportsPagination || len(results) < limit || added == 0 || page == pageBudget-1 {
			r.fail(Failure{IndexerID: indexer.ID, Reason: gapReason})
			break
		}
		offset += len(results)
	}
	if ctx.Err() == nil && len(firstPage) > 0 {
		feeds.SetCheckpoint(indexer.ID, firstPage)
	}
	return fresh, true
}

// loadFeeds loads the feed state at its first use in the run, prunes it, and
// saves it. It returns nil after a database failure.
func (r *Run) loadFeeds(ctx context.Context) *state.Feeds {
	if r.feeds != nil {
		return r.feeds
	}
	feeds, err := r.source.store.Feeds(ctx)
	if err != nil {
		r.databaseFailure(err)
		return nil
	}
	// The present indexers include the indexers in cooldown, so their
	// checkpoints and retained listings survive.
	feeds.Prune(r.present, time.Now())
	r.feeds = feeds
	if !r.saveFeeds(ctx) {
		return nil
	}
	return feeds
}

// saveFeeds saves the feed state unless a client was not readable. It
// returns false after a database failure.
func (r *Run) saveFeeds(ctx context.Context) bool {
	if r.skipFeedSave {
		return true
	}
	if err := r.source.store.SaveFeeds(ctx, r.feeds); err != nil {
		r.databaseFailure(err)
		return false
	}
	return true
}
