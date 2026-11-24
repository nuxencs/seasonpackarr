// Copyright (c) 2023 - 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package release

import (
	"slices"
	"strings"

	"github.com/nuxencs/seasonpackarr/internal/domain"

	"github.com/autobrr/rls"
)

func CheckCandidates(requestRls, clientRls rls.Release, fuzzyMatching domain.FuzzyMatching) domain.CompareInfo {
	// check if season pack or no extension
	if !requestRls.Type.Is(rls.Series) || requestRls.Ext != "" {
		// not a season pack
		return domain.CompareInfo{StatusCode: domain.StatusNotASeasonPack}
	}

	return compare(requestRls, clientRls, fuzzyMatching)
}

func compare(requestRls, clientRls rls.Release, fuzzyMatching domain.FuzzyMatching) domain.CompareInfo {
	if requestRls.Resolution != clientRls.Resolution {
		return domain.CompareInfo{
			StatusCode:   domain.StatusResolutionMismatch,
			RejectValueA: requestRls.Resolution,
			RejectValueB: clientRls.Resolution,
		}
	}

	requestSource := requestRls.Source
	clientSource := clientRls.Source
	// normalize WEB-DL down to plain WEB when simplifyWebCompare is enabled
	if fuzzyMatching.SimplifyWebCompare {
		requestRls.Source = simplifyWEB(requestRls.Source)
		clientRls.Source = simplifyWEB(clientRls.Source)
	}

	if requestRls.Source != clientRls.Source {
		return domain.CompareInfo{
			StatusCode:   domain.StatusSourceMismatch,
			RejectValueA: requestSource,
			RejectValueB: clientSource,
		}
	}

	if rls.MustNormalize(requestRls.Group) != rls.MustNormalize(clientRls.Group) {
		return domain.CompareInfo{
			StatusCode:   domain.StatusRlsGrpMismatch,
			RejectValueA: requestRls.Group,
			RejectValueB: clientRls.Group,
		}
	}

	if !equalElements(requestRls.Cut, clientRls.Cut) {
		return domain.CompareInfo{
			StatusCode:   domain.StatusCutMismatch,
			RejectValueA: requestRls.Cut,
			RejectValueB: clientRls.Cut,
		}
	}

	if !equalElements(requestRls.Edition, clientRls.Edition) {
		return domain.CompareInfo{
			StatusCode:   domain.StatusEditionMismatch,
			RejectValueA: requestRls.Edition,
			RejectValueB: clientRls.Edition,
		}
	}

	// skip comparing repack status when skipRepackCompare is enabled
	if !fuzzyMatching.SkipRepackCompare {
		if !equalElements(requestRls.Other, clientRls.Other) {
			return domain.CompareInfo{
				StatusCode:   domain.StatusRepackStatusMismatch,
				RejectValueA: requestRls.Other,
				RejectValueB: clientRls.Other,
			}
		}
	}

	requestHDR := slices.Clone(requestRls.HDR)
	clientHDR := slices.Clone(clientRls.HDR)
	// normalize any HDR format down to plain HDR when simplifyHdrCompare is enabled
	if fuzzyMatching.SimplifyHdrCompare {
		requestRls.HDR = simplifyHDR(slices.Clone(requestHDR))
		clientRls.HDR = simplifyHDR(slices.Clone(clientHDR))
	}

	if !equalElements(requestRls.HDR, clientRls.HDR) {
		return domain.CompareInfo{
			StatusCode:   domain.StatusHdrMismatch,
			RejectValueA: requestHDR,
			RejectValueB: clientHDR,
		}
	}

	if requestRls.Collection != clientRls.Collection {
		return domain.CompareInfo{
			StatusCode:   domain.StatusStreamingServiceMismatch,
			RejectValueA: requestRls.Collection,
			RejectValueB: clientRls.Collection,
		}
	}

	if requestRls.Episode == clientRls.Episode {
		return domain.CompareInfo{StatusCode: domain.StatusAlreadyInClient}
	}

	return domain.CompareInfo{StatusCode: domain.StatusSuccessfulMatch}
}

func simplifyWEB(source string) string {
	if source == "WEB-DL" {
		return "WEB"
	}

	return source
}

// equalElements reports whether x and y hold the same values with the same
// count of each value, in any order.
func equalElements[T comparable](x, y []T) bool {
	if len(x) != len(y) {
		return false
	}

	freqMap := make(map[T]int, len(x))
	for _, i := range x {
		freqMap[i]++
	}

	for _, i := range y {
		if freqMap[i] == 0 {
			return false
		}
		freqMap[i]--
	}

	return true
}

// simplifyHDR replaces each HDR format in hdr with "HDR". It changes hdr in place.
func simplifyHDR(hdr []string) []string {
	for i := range hdr {
		if strings.Contains(hdr[i], "HDR") {
			hdr[i] = "HDR"
		}
	}

	return hdr
}
