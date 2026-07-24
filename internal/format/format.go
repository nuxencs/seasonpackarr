// Copyright (c) 2023 - 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package format

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/nuxencs/seasonpackarr/internal/domain"

	"github.com/moistari/rls"
)

var (
	// regex for groups that don't need the folder name to be adjusted
	ignoredRlsGrps = regexp.MustCompile(`(?i)^(ZR)$`)

	illegal = regexp.MustCompile(`(?i)[\\/:"*?<>|]`)
	audio   = regexp.MustCompile(`(?i)(AAC|DDP)\.(\d\.\d)`)
	dots    = regexp.MustCompile(`(?i)\.+`)
)

func ComparableTitle(r rls.Release, fuzzyMatching domain.FuzzyMatching) string {
	if fuzzyMatching.SkipYearCompare {
		return fmt.Sprintf("%s%d", rls.MustNormalize(r.Title), r.Series)
	}

	return fmt.Sprintf("%s%d%d", rls.MustNormalize(r.Title), r.Year, r.Series)
}

func CleanAnnounceTitle(release rls.Release) string {
	packName := release.String()

	// check if RlsGrp of release is in ignore regex
	if !ignoredRlsGrps.MatchString(release.Group) {
		// remove illegal characters
		packName = illegal.ReplaceAllString(packName, "")
		// replace spaces with periods
		packName = strings.ReplaceAll(packName, " ", ".")
		// replace wrong audio naming
		packName = audio.ReplaceAllString(packName, "$1$2")
		// replace multiple dots with only one
		packName = dots.ReplaceAllString(packName, ".")
	}

	return packName
}
