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
			StatusCode:         domain.StatusResolutionMismatch,
			RequestRejectField: requestRls.Resolution,
			ClientRejectField:  clientRls.Resolution,
		}
	}

	if rls.MustNormalize(requestRls.Source) != rls.MustNormalize(clientRls.Source) {
		return domain.CompareInfo{
			StatusCode:         domain.StatusSourceMismatch,
			RequestRejectField: requestRls.Source,
			ClientRejectField:  clientRls.Source,
		}
	}

	if rls.MustNormalize(requestRls.Group) != rls.MustNormalize(clientRls.Group) {
		return domain.CompareInfo{
			StatusCode:         domain.StatusRlsGrpMismatch,
			RequestRejectField: requestRls.Group,
			ClientRejectField:  clientRls.Group,
		}
	}

	if !utils.EqualElements(requestRls.Cut, clientRls.Cut) {
		return domain.CompareInfo{
			StatusCode:         domain.StatusCutMismatch,
			RequestRejectField: requestRls.Cut,
			ClientRejectField:  clientRls.Cut,
		}
	}

	if !utils.EqualElements(requestRls.Edition, clientRls.Edition) {
		return domain.CompareInfo{
			StatusCode:         domain.StatusEditionMismatch,
			RequestRejectField: requestRls.Edition,
			ClientRejectField:  clientRls.Edition,
		}
	}

	// skip comparing repack status when skipRepackCompare is enabled
	if !fuzzyMatching.SkipRepackCompare {
		if !utils.EqualElements(requestRls.Other, clientRls.Other) {
			return domain.CompareInfo{
				StatusCode:         domain.StatusRepackStatusMismatch,
				RequestRejectField: requestRls.Other,
				ClientRejectField:  clientRls.Other,
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
			StatusCode:         domain.StatusHdrMismatch,
			RequestRejectField: requestRls.HDR,
			ClientRejectField:  clientRls.HDR,
		}
	}

	if requestRls.Collection != clientRls.Collection {
		return domain.CompareInfo{
			StatusCode:         domain.StatusStreamingServiceMismatch,
			RequestRejectField: requestRls.Collection,
			ClientRejectField:  clientRls.Collection,
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
			StatusCode:         domain.StatusSizeMismatch,
			ClientRejectField:  clientEpSize,
			TorrentRejectField: torrentEpSize,
		}
	}

	epInClientRls := rls.ParseString(filepath.Base(clientEpPath))
	epInTorrentRls := rls.ParseString(filepath.Base(torrentEpPath))

	switch {
	case epInClientRls.Series != epInTorrentRls.Series:
		return "", domain.CompareInfo{
			StatusCode:         domain.StatusSeasonMismatch,
			ClientRejectField:  epInClientRls.Series,
			TorrentRejectField: epInTorrentRls.Series,
		}
	case epInClientRls.Episode != epInTorrentRls.Episode:
		return "", domain.CompareInfo{
			StatusCode:         domain.StatusEpisodeMismatch,
			ClientRejectField:  epInClientRls.Episode,
			TorrentRejectField: epInTorrentRls.Episode,
		}
	case epInClientRls.Resolution != epInTorrentRls.Resolution:
		return "", domain.CompareInfo{
			StatusCode:         domain.StatusResolutionMismatch,
			ClientRejectField:  epInClientRls.Resolution,
			TorrentRejectField: epInTorrentRls.Resolution,
		}
	case rls.MustNormalize(epInClientRls.Group) != rls.MustNormalize(epInTorrentRls.Group):
		return "", domain.CompareInfo{
			StatusCode:         domain.StatusRlsGrpMismatch,
			ClientRejectField:  epInClientRls.Group,
			TorrentRejectField: epInTorrentRls.Group,
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
