// Copyright (c) 2023 - 2024, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package release

import (
	"seasonpackarr/internal/domain"
	"seasonpackarr/internal/utils"

	"github.com/moistari/rls"
)

func CheckCandidates(requestrls, child *domain.Entry, compareRepackStatus bool) int {
	rlsRelease := requestrls.R
	rlsInClient := child.R

	// check if season pack and no extension
	if rlsRelease.Type.Is(rls.Series) && rlsRelease.Ext == "" {
		// compare releases
		return compareReleases(rlsInClient, rlsRelease, compareRepackStatus)
	}
	// not a season pack
	return 211
}

func compareReleases(r1, r2 rls.Release, compareRepackStatus bool) int {
	if rls.MustNormalize(r1.Resolution) != rls.MustNormalize(r2.Resolution) {
		return 201
	}

	if rls.MustNormalize(r1.Source) != rls.MustNormalize(r2.Source) {
		return 202
	}

	if rls.MustNormalize(r1.Group) != rls.MustNormalize(r2.Group) {
		return 203
	}

	if !utils.CompareStringSlices(r1.Cut, r2.Cut) {
		return 204
	}

	if !utils.CompareStringSlices(r1.Edition, r2.Edition) {
		return 205
	}

	if compareRepackStatus {
		if !utils.CompareStringSlices(r1.Other, r2.Other) {
			return 206
		}
	}

	if !utils.CompareStringSlices(r1.HDR, r2.HDR) {
		return 207
	}

	if r1.Collection != r2.Collection {
		return 208
	}

	if r1.Episode == r2.Episode {
		return 210
	}

	return 250
}

func PercentOfTotalEpisodes(totalEpisodes int, matchedEpisodes []int) float32 {
	if totalEpisodes == 0 {
		return 0
	}
	count := len(matchedEpisodes)
	percent := float32(count) / float32(totalEpisodes)

	return percent
}
