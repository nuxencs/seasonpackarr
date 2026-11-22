// Copyright (c) 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package discovery

import (
	"cmp"
	"context"
	"iter"

	"github.com/nuxencs/seasonpackarr/internal/prowlarr"
)

// pageBudget is the number of pages that one search or one poll reads at most.
const pageBudget = 10

// Search returns the listings of query on indexer. It requests a page only
// when the caller asks for more listings, and yields each listing one time.
// It stops on a short page, on an indexer without pagination, on a repeated
// page, and at the page budget. A failed page takes the indexer out for the
// run. The sequence yields nothing for an indexer that is out, and it ends
// when the indexer goes out or the database fails.
func (r *Run) Search(ctx context.Context, indexer Indexer, query Query) iter.Seq[*Listing] {
	fail := func(reason string) { r.fail(Failure{IndexerID: indexer.ID, Query: query.String(), Reason: reason}) }
	return func(yield func(*Listing) bool) {
		seen := make(map[string]bool)
		offset := 0
		for range pageBudget {
			if !r.usable(ctx, indexer.ID) {
				return
			}
			r.stats.FeedPages++
			results, limit, err := r.source.client.SearchPage(ctx, indexer.prowlarr, prowlarr.Query(query), offset)
			if err != nil {
				fail(err.Error())
				// Rule 3: do not send a request to this indexer for each query.
				r.out[indexer.ID] = true
				r.recordCooldown(ctx, indexer.ID, err)
				return
			}
			fresh := 0
			for _, result := range results {
				if !r.usable(ctx, indexer.ID) {
					return
				}
				id := cmp.Or(result.GUID, result.Link, result.Enclosure.URL, result.Title)
				if seen[id] {
					continue
				}
				seen[id] = true
				fresh++
				if !yield(&Listing{run: r, indexerID: indexer.ID, result: result}) {
					return
				}
				// A download of the last listing of the last page can take the
				// indexer out. Then the page budget failure does not apply.
				if r.out[indexer.ID] {
					return
				}
			}
			if !indexer.prowlarr.SupportsPagination || len(results) < limit {
				return
			}
			if fresh == 0 {
				fail("indexer repeated a page; remaining results may be incomplete")
				return
			}
			offset += len(results)
		}
		fail("search reached the 10-page limit; remaining results may be incomplete")
	}
}

// usable reports whether a sequence of the run can continue on the indexer.
func (r *Run) usable(ctx context.Context, indexerID int) bool {
	return ctx.Err() == nil && r.err == nil && !r.out[indexerID]
}
