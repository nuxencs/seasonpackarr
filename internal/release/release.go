// Copyright (c) 2023 - 2024, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package release

import (
	"github.com/nuxencs/seasonpackarr/internal/domain"
	"github.com/nuxencs/seasonpackarr/internal/utils"

	"github.com/moistari/rls"
)

func CheckCandidates(requestRls, clientRls rls.Release, fuzzyMatching domain.FuzzyMatching) int {
	// check if season pack or no extension
	if !requestRls.Type.Is(rls.Series) || requestRls.Ext != "" {
		// not a season pack
		return domain.StatusNotASeasonPack
	}

	return compareReleases(clientRls, requestRls, fuzzyMatching)
}

func compareReleases(r1, r2 rls.Release, fuzzyMatching domain.FuzzyMatching) int {
	if rls.MustNormalize(r1.Resolution) != rls.MustNormalize(r2.Resolution) {
		return domain.StatusResolutionMismatch
	}

	if rls.MustNormalize(r1.Source) != rls.MustNormalize(r2.Source) {
		return domain.StatusSourceMismatch
	}

	if rls.MustNormalize(r1.Group) != rls.MustNormalize(r2.Group) {
		return domain.StatusRlsGrpMismatch
	}

	if !utils.EqualElements(r1.Cut, r2.Cut) {
		return domain.StatusCutMismatch
	}

	if !utils.EqualElements(r1.Edition, r2.Edition) {
		return domain.StatusEditionMismatch
	}

	// skip comparing repack status when skipRepackCompare is enabled
	if !fuzzyMatching.SkipRepackCompare {
		if !utils.EqualElements(r1.Other, r2.Other) {
			return domain.StatusRepackStatusMismatch
		}
	}

	// normalize any HDR format down to plain HDR when simplifyHdrCompare is enabled
	if fuzzyMatching.SimplifyHdrCompare {
		r1.HDR = utils.SimplifyHDRSlice(r1.HDR)
		r2.HDR = utils.SimplifyHDRSlice(r2.HDR)
	}

	if !utils.EqualElements(r1.HDR, r2.HDR) {
		return domain.StatusHdrMismatch
	}

	if r1.Collection != r2.Collection {
		return domain.StatusStreamingServiceMismatch
	}

	if r1.Episode == r2.Episode {
		return domain.StatusAlreadyInClient
	}

	return domain.StatusSuccessfulMatch
}

func PercentOfTotalEpisodes(totalEps int, foundEps int) float32 {
	if totalEps == 0 {
		return 0
	}

	return float32(foundEps) / float32(totalEps)
}
