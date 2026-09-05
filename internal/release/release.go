// Copyright (c) 2023 - 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package release

import (
	"path/filepath"

	"github.com/nuxencs/seasonpackarr/internal/domain"
	"github.com/nuxencs/seasonpackarr/internal/slices"

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

	// normalize WEB-DL down to plain WEB when simplifyWebCompare is enabled
	if fuzzyMatching.SimplifyWebCompare {
		requestRls.Source = simplifyWEB(requestRls.Source)
		clientRls.Source = simplifyWEB(clientRls.Source)
	}

	if requestRls.Source != clientRls.Source {
		return domain.CompareInfo{
			StatusCode:   domain.StatusSourceMismatch,
			RejectValueA: requestRls.Source,
			RejectValueB: clientRls.Source,
		}
	}

	if rls.MustNormalize(requestRls.Group) != rls.MustNormalize(clientRls.Group) {
		return domain.CompareInfo{
			StatusCode:   domain.StatusRlsGrpMismatch,
			RejectValueA: requestRls.Group,
			RejectValueB: clientRls.Group,
		}
	}

	if !slices.EqualElements(requestRls.Cut, clientRls.Cut) {
		return domain.CompareInfo{
			StatusCode:   domain.StatusCutMismatch,
			RejectValueA: requestRls.Cut,
			RejectValueB: clientRls.Cut,
		}
	}

	if !slices.EqualElements(requestRls.Edition, clientRls.Edition) {
		return domain.CompareInfo{
			StatusCode:   domain.StatusEditionMismatch,
			RejectValueA: requestRls.Edition,
			RejectValueB: clientRls.Edition,
		}
	}

	// skip comparing repack status when skipRepackCompare is enabled
	if !fuzzyMatching.SkipRepackCompare {
		if !slices.EqualElements(requestRls.Other, clientRls.Other) {
			return domain.CompareInfo{
				StatusCode:   domain.StatusRepackStatusMismatch,
				RejectValueA: requestRls.Other,
				RejectValueB: clientRls.Other,
			}
		}
	}

	// normalize any HDR format down to plain HDR when simplifyHdrCompare is enabled
	if fuzzyMatching.SimplifyHdrCompare {
		requestRls.HDR = slices.SimplifyHDR(requestRls.HDR)
		clientRls.HDR = slices.SimplifyHDR(clientRls.HDR)
	}

	if !slices.EqualElements(requestRls.HDR, clientRls.HDR) {
		return domain.CompareInfo{
			StatusCode:   domain.StatusHdrMismatch,
			RejectValueA: requestRls.HDR,
			RejectValueB: clientRls.HDR,
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

func MatchEpToSeasonPackEp(clientEpPath string, clientEpSize int64, torrentEpPath string, torrentEpSize int64) (string, domain.CompareInfo) {
	if clientEpSize != torrentEpSize {
		return "", domain.CompareInfo{
			StatusCode:   domain.StatusSizeMismatch,
			RejectValueA: clientEpSize,
			RejectValueB: torrentEpSize,
		}
	}

	clientEpRls := rls.ParseString(filepath.Base(clientEpPath))
	torrentEpRls := rls.ParseString(filepath.Base(torrentEpPath))

	switch {
	case clientEpRls.Series != torrentEpRls.Series:
		return "", domain.CompareInfo{
			StatusCode:   domain.StatusSeasonMismatch,
			RejectValueA: clientEpRls.Series,
			RejectValueB: torrentEpRls.Series,
		}
	case clientEpRls.Episode != torrentEpRls.Episode:
		return "", domain.CompareInfo{
			StatusCode:   domain.StatusEpisodeMismatch,
			RejectValueA: clientEpRls.Episode,
			RejectValueB: torrentEpRls.Episode,
		}
	case clientEpRls.Resolution != torrentEpRls.Resolution:
		return "", domain.CompareInfo{
			StatusCode:   domain.StatusResolutionMismatch,
			RejectValueA: clientEpRls.Resolution,
			RejectValueB: torrentEpRls.Resolution,
		}
	case rls.MustNormalize(clientEpRls.Group) != rls.MustNormalize(torrentEpRls.Group):
		return "", domain.CompareInfo{
			StatusCode:   domain.StatusRlsGrpMismatch,
			RejectValueA: clientEpRls.Group,
			RejectValueB: torrentEpRls.Group,
		}
	}

	return torrentEpPath, domain.CompareInfo{}
}

func PercentOfTotalEpisodes(totalEps int, foundEps int) float32 {
	if totalEps == 0 {
		return 0
	}

	return float32(foundEps) / float32(totalEps)
}

func IsValidEpisodeFile(torrentFileName string) bool {
	torrentFileRls := rls.ParseString(filepath.Base(torrentFileName))

	// ignore non video files
	if torrentFileRls.Ext != "mkv" {
		return false
	}

	// ignore sample files
	if rls.MustNormalize(torrentFileRls.Group) == "sample" {
		return false
	}

	return true
}

func simplifyWEB(source string) string {
	if source == "WEB-DL" {
		return "WEB"
	}

	return source
}
