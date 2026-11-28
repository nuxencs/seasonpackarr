// Copyright (c) 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package http

import (
	"context"
	"maps"
	"slices"

	"github.com/nuxencs/seasonpackarr/internal/discovery"

	"github.com/autobrr/rls"
)

// rssGroup returns the group of the season pack that title names, or nil when
// title names no season pack of a group.
func rssGroup(title string, groups map[seasonSearchKey]*seasonSearch) *seasonSearch {
	parsed := rls.ParseString(title)
	if !parsed.Type.Is(rls.Series) || parsed.Series <= 0 || parsed.Episode != 0 {
		return nil
	}
	key := seasonSearchKey{Title: rls.MustNormalize(parsed.Title), Season: parsed.Series}
	return groups[key]
}

// pollRSS evaluates the relevant listings of the feed of each indexer. The
// source yields them in feed order, then the retained listings. No decision is
// kept, so a retained pack is checked again in each poll.
func (run *discoveryRun) pollRSS(ctx context.Context, indexers []discovery.Indexer, groups map[seasonSearchKey]*seasonSearch) {
	relevant := func(title string) bool { return rssGroup(title, groups) != nil }
	for _, indexer := range indexers {
		for listing := range run.source.Feed(ctx, indexer, relevant) {
			run.evaluateListing(ctx, listing, slices.Sorted(maps.Keys(rssGroup(listing.Title(), groups).clients)))
		}
	}
}
