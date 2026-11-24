// Copyright (c) 2023 - 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package release

import (
	"github.com/nuxencs/seasonpackarr/internal/domain"

	"github.com/autobrr/rls"
)

// SeasonKey identifies one season: the normalized show title, the season
// number, and the year. Two releases are of one season when their keys are
// equal. The key is comparable and works as a map key. Build keys only with SeasonKeyOf.
type SeasonKey struct {
	title  string
	season int
	year   int
}

// SeasonKeyOf returns the season key of r under the fuzzy matching settings.
// With SkipYearCompare, the year is not part of the key.
func SeasonKeyOf(r *rls.Release, fuzzyMatching domain.FuzzyMatching) SeasonKey {
	key := SeasonKey{title: rls.MustNormalize(r.Title), season: r.Series, year: r.Year}
	if fuzzyMatching.SkipYearCompare {
		key.year = 0
	}
	return key
}
