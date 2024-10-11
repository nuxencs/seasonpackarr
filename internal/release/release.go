// Copyright (c) 2023 - 2024, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package release

import (
	"path/filepath"

	"github.com/nuxencs/seasonpackarr/internal/domain"
	"github.com/nuxencs/seasonpackarr/internal/utils"

	"github.com/moistari/rls"
)

func CheckCandidates(requestRls, clientRls rls.Release, fuzzyMatching domain.FuzzyMatching) domain.CompareInfo {
	// check if season pack or no extension
	if !requestRls.Type.Is(rls.Series) || requestRls.Ext != "" {
		// not a season pack
		return domain.CompareInfo{StatusCode: domain.StatusNotASeasonPack}
	}

	return compareReleases(requestRls, clientRls, fuzzyMatching)
}

func compareReleases(requestRls, clientRls rls.Release, fuzzyMatching domain.FuzzyMatching) domain.CompareInfo {
	if rls.MustNormalize(requestRls.Resolution) != rls.MustNormalize(clientRls.Resolution) {
		return domain.CompareInfo{
			StatusCode:   domain.StatusResolutionMismatch,
			RejectValueA: requestRls.Resolution,
			RejectValueB: clientRls.Resolution,
		}
	}

	if rls.MustNormalize(requestRls.Source) != rls.MustNormalize(clientRls.Source) {
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

	if !utils.EqualElements(requestRls.Cut, clientRls.Cut) {
		return domain.CompareInfo{
			StatusCode:   domain.StatusCutMismatch,
			RejectValueA: requestRls.Cut,
			RejectValueB: clientRls.Cut,
		}
	}

	if !utils.EqualElements(requestRls.Edition, clientRls.Edition) {
		return domain.CompareInfo{
			StatusCode:   domain.StatusEditionMismatch,
			RejectValueA: requestRls.Edition,
			RejectValueB: clientRls.Edition,
		}
	}

	// skip comparing repack status when skipRepackCompare is enabled
	if !fuzzyMatching.SkipRepackCompare {
		if !utils.EqualElements(requestRls.Other, clientRls.Other) {
			return domain.CompareInfo{
				StatusCode:   domain.StatusRepackStatusMismatch,
				RejectValueA: requestRls.Other,
				RejectValueB: clientRls.Other,
			}
		}
	}

	// normalize any HDR format down to plain HDR when simplifyHdrCompare is enabled
	if fuzzyMatching.SimplifyHdrCompare {
		requestRls.HDR = utils.SimplifyHDRSlice(requestRls.HDR)
		clientRls.HDR = utils.SimplifyHDRSlice(clientRls.HDR)
	}

	if !utils.EqualElements(requestRls.HDR, clientRls.HDR) {
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

	epInClientRls := rls.ParseString(filepath.Base(clientEpPath))
	epInTorrentRls := rls.ParseString(filepath.Base(torrentEpPath))

	switch {
	case epInClientRls.Series != epInTorrentRls.Series:
		return "", domain.CompareInfo{
			StatusCode:   domain.StatusSeasonMismatch,
			RejectValueA: epInClientRls.Series,
			RejectValueB: epInTorrentRls.Series,
		}
	case epInClientRls.Episode != epInTorrentRls.Episode:
		return "", domain.CompareInfo{
			StatusCode:   domain.StatusEpisodeMismatch,
			RejectValueA: epInClientRls.Episode,
			RejectValueB: epInTorrentRls.Episode,
		}
	case epInClientRls.Resolution != epInTorrentRls.Resolution:
		return "", domain.CompareInfo{
			StatusCode:   domain.StatusResolutionMismatch,
			RejectValueA: epInClientRls.Resolution,
			RejectValueB: epInTorrentRls.Resolution,
		}
	case rls.MustNormalize(epInClientRls.Group) != rls.MustNormalize(epInTorrentRls.Group):
		return "", domain.CompareInfo{
			StatusCode:   domain.StatusRlsGrpMismatch,
			RejectValueA: epInClientRls.Group,
			RejectValueB: epInTorrentRls.Group,
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
