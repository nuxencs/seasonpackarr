// Copyright (c) 2023 - 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package metadata

import (
	"strings"

	"github.com/autobrr/rls"
)

// maxVariantWords caps the longest shortened title variant. Provider searches
// were only ever observed to fail for titles of 7 or more words, so longer
// variants add API calls without improving the chance of a match. Together with
// the two-word minimum this bounds the fallback at 5 extra calls per provider,
// keeping bursts well below the TVMaze rate limit of 20 calls per 10 seconds.
const maxVariantWords = 6

// shortenedTitles returns progressively shortened variants of a normalized title,
// dropping one trailing word at a time down to a minimum of two words. Release
// names sometimes carry a localized subtitle after the actual show title, which
// makes provider searches come up empty; searching with shortened variants
// converges back to the actual show title.
func shortenedTitles(normTitle string) []string {
	words := strings.Fields(normTitle)

	longestVariant := min(len(words)-1, maxVariantWords)

	var titles []string
	for i := longestVariant; i >= 2; i-- {
		titles = append(titles, strings.Join(words[:i], " "))
	}

	return titles
}

// matchesTitle reports whether any of the candidate names matches the normalized
// release title, either exactly or as its leading words. This guards shortened
// title searches against matching an unrelated show. A prefix match requires at
// least two candidate words; a single leading word is too generic to vouch for
// the show.
func matchesTitle(normTitle string, candidates ...string) bool {
	for _, candidate := range candidates {
		normCandidate := rls.MustNormalize(candidate)
		if normCandidate == "" {
			continue
		}

		if normCandidate == normTitle {
			return true
		}

		if strings.Contains(normCandidate, " ") && strings.HasPrefix(normTitle, normCandidate+" ") {
			return true
		}
	}

	return false
}
