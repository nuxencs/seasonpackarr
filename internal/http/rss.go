// Copyright (c) 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package http

import (
	"context"
	"time"

	"github.com/autobrr/rls"
	"github.com/nuxencs/seasonpackarr/internal/prowlarr"
	"github.com/nuxencs/seasonpackarr/internal/state"
)

func rssGroup(result prowlarr.Result, groups map[seasonSearchKey]*seasonSearch) *seasonSearch {
	parsed := rls.ParseString(result.Title)
	if !parsed.Type.Is(rls.Series) || parsed.Series <= 0 || parsed.Episode != 0 {
		return nil
	}
	key := seasonSearchKey{Title: rls.MustNormalize(parsed.Title), Season: parsed.Series}
	return groups[key]
}

func (run *discoveryRun) pollRSS(ctx context.Context, indexer prowlarr.Indexer, groups map[seasonSearchKey]*seasonSearch, feeds *state.Feeds, persist bool) {
	previous := feeds.Checkpoint(indexer.ID)
	firstPage := make(map[state.Key]bool)
	seen := make(map[state.Key]bool)
	var fresh []prowlarr.Result
	offset := 0
	for page := range 10 {
		run.report.Requests++
		results, limit, err := run.runner.provider.RSSPage(ctx, indexer, offset)
		if err != nil {
			if stateErr := run.runner.recordCooldown(ctx, indexer.ID, err); stateErr != nil {
				run.storageFailure(stateErr)
			}
			run.report.Failures = append(run.report.Failures, searchFailure{IndexerID: indexer.ID, Reason: err.Error()})
			// Preserve the checkpoint after a partial fetch so the next poll can retry.
			return
		}
		overlap, added := false, 0
		for _, result := range results {
			key := state.RSSIdentity(indexer.ID, result)
			if page == 0 && len(firstPage) < 100 {
				firstPage[key] = true
			}
			overlap = overlap || previous[key]
			if seen[key] {
				continue
			}
			seen[key] = true
			added++
			if rssGroup(result, groups) != nil {
				fresh = append(fresh, result)
				feeds.Remember(key, result, time.Now())
			}
		}
		if len(previous) == 0 || overlap {
			break
		}
		if !indexer.SupportsPagination || len(results) < limit || added == 0 || page == 9 {
			run.report.Failures = append(run.report.Failures, searchFailure{IndexerID: indexer.ID, Reason: "RSS feed did not overlap the previous poll; releases may be missing; use a manual targeted search"})
			break
		}
		offset += len(results)
	}
	if ctx.Err() != nil {
		return
	}
	if len(firstPage) > 0 {
		feeds.SetCheckpoint(indexer.ID, firstPage)
	}
	if !run.saveFeeds(ctx, feeds, persist) {
		return
	}
	// Feed order wins. Retained releases follow, including packs that failed
	// coverage before their episodes finished downloading. Never cache decisions.
	evaluated := make(map[state.Key]bool)
	for _, result := range append(fresh, feeds.Results(indexer.ID)...) {
		if ctx.Err() != nil || run.storageFailed {
			return
		}
		key := state.RSSIdentity(indexer.ID, result)
		if evaluated[key] {
			continue
		}
		evaluated[key] = true
		group := rssGroup(result, groups)
		if group == nil {
			continue
		}
		run.evaluateResult(ctx, indexer, result, group)
		if _, failed := run.runner.cooldowns[indexer.ID]; failed {
			return
		}
	}
}

func (run *discoveryRun) saveFeeds(ctx context.Context, feeds *state.Feeds, persist bool) bool {
	if persist {
		if err := run.runner.state.SaveFeeds(ctx, feeds); err != nil {
			run.storageFailure(err)
			return false
		}
	}
	return true
}
