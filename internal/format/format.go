// Copyright (c) 2023 - 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package format

import (
	"fmt"

	"github.com/nuxencs/seasonpackarr/internal/domain"

	"github.com/moistari/rls"
)

func ComparableTitle(r rls.Release, fuzzyMatching domain.FuzzyMatching) string {
	if fuzzyMatching.SkipYearCompare {
		return fmt.Sprintf("%s%d", rls.MustNormalize(r.Title), r.Series)
	}

	return fmt.Sprintf("%s%d%d", rls.MustNormalize(r.Title), r.Year, r.Series)
}
