// Copyright (c) 2023 - 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package release

import (
	"path/filepath"
	"strings"

	"github.com/nuxencs/seasonpackarr/internal/domain"
	"github.com/nuxencs/seasonpackarr/internal/slices"

	"github.com/moistari/rls"
)

func CheckCandidates(requestRls, clientRls rls.Release, fuzzyMatching domain.FuzzyMatching) domain.CompareInfo {
	// check if season pack or no extension
	if !requestRls.Type.Is(rls.Series) || requestRls.Ext != "" {
		// not a season pack
		return domain.CompareInfo{Reason: domain.ReasonNotSeasonPack}
	}

	return compare(requestRls, clientRls, fuzzyMatching)
}

func compare(requestRls, clientRls rls.Release, fuzzyMatching domain.FuzzyMatching) domain.CompareInfo {
	if requestRls.Resolution != clientRls.Resolution {
		return domain.CompareInfo{
			Reason:       domain.ReasonResolutionMismatch,
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
			Reason:       domain.ReasonSourceMismatch,
			RejectValueA: requestRls.Source,
			RejectValueB: clientRls.Source,
		}
	}

	if rls.MustNormalize(requestRls.Group) != rls.MustNormalize(clientRls.Group) {
		return domain.CompareInfo{
			Reason:       domain.ReasonReleaseGroupMismatch,
			RejectValueA: requestRls.Group,
			RejectValueB: clientRls.Group,
		}
	}

	if !slices.EqualElements(requestRls.Cut, clientRls.Cut) {
		return domain.CompareInfo{
			Reason:       domain.ReasonCutMismatch,
			RejectValueA: requestRls.Cut,
			RejectValueB: clientRls.Cut,
		}
	}

	if !slices.EqualElements(requestRls.Edition, clientRls.Edition) {
		return domain.CompareInfo{
			Reason:       domain.ReasonEditionMismatch,
			RejectValueA: requestRls.Edition,
			RejectValueB: clientRls.Edition,
		}
	}

	// skip comparing repack status when skipRepackCompare is enabled
	if !fuzzyMatching.SkipRepackCompare {
		if !slices.EqualElements(requestRls.Other, clientRls.Other) {
			return domain.CompareInfo{
				Reason:       domain.ReasonRepackMismatch,
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
			Reason:       domain.ReasonHDRMismatch,
			RejectValueA: requestRls.HDR,
			RejectValueB: clientRls.HDR,
		}
	}

	if requestRls.Collection != clientRls.Collection {
		return domain.CompareInfo{
			Reason:       domain.ReasonStreamingServiceMismatch,
			RejectValueA: requestRls.Collection,
			RejectValueB: clientRls.Collection,
		}
	}

	if requestRls.Episode == clientRls.Episode {
		return domain.CompareInfo{Reason: domain.ReasonAlreadyInClient}
	}

	return domain.CompareInfo{Reason: domain.ReasonMatched}
}

func MatchEpToSeasonPackEp(clientEpPath string, clientEpSize int64, torrentEpPath string, torrentEpSize int64) (string, domain.CompareInfo) {
	clientEpRls := parseEpisodeFile(clientEpPath)
	torrentEpRls := parseEpisodeFile(torrentEpPath)

	switch {
	case !strings.EqualFold(clientEpRls.Ext, torrentEpRls.Ext):
		return "", domain.CompareInfo{
			Reason:       domain.ReasonContainerMismatch,
			RejectValueA: clientEpRls.Ext,
			RejectValueB: torrentEpRls.Ext,
		}
	case clientEpSize != torrentEpSize:
		return "", domain.CompareInfo{
			Reason:       domain.ReasonSizeMismatch,
			RejectValueA: clientEpSize,
			RejectValueB: torrentEpSize,
		}
	case clientEpRls.Series != torrentEpRls.Series:
		return "", domain.CompareInfo{
			Reason:       domain.ReasonSeasonMismatch,
			RejectValueA: clientEpRls.Series,
			RejectValueB: torrentEpRls.Series,
		}
	case clientEpRls.Episode != torrentEpRls.Episode:
		return "", domain.CompareInfo{
			Reason:       domain.ReasonEpisodeMismatch,
			RejectValueA: clientEpRls.Episode,
			RejectValueB: torrentEpRls.Episode,
		}
	case clientEpRls.Resolution != torrentEpRls.Resolution:
		return "", domain.CompareInfo{
			Reason:       domain.ReasonResolutionMismatch,
			RejectValueA: clientEpRls.Resolution,
			RejectValueB: torrentEpRls.Resolution,
		}
	case rls.MustNormalize(clientEpRls.Group) != rls.MustNormalize(torrentEpRls.Group):
		return "", domain.CompareInfo{
			Reason:       domain.ReasonReleaseGroupMismatch,
			RejectValueA: clientEpRls.Group,
			RejectValueB: torrentEpRls.Group,
		}
	}

	return torrentEpPath, domain.CompareInfo{Reason: domain.ReasonMatched}
}

func PercentOfTotalEpisodes(totalEps int, foundEps int) float32 {
	if totalEps == 0 {
		return 0
	}

	return float32(foundEps) / float32(totalEps)
}

func IsValidEpisodeFile(torrentFileName string) bool {
	torrentFileRls := parseEpisodeFile(torrentFileName)

	// ignore non video files
	if !strings.EqualFold(torrentFileRls.Ext, "mkv") && !strings.EqualFold(torrentFileRls.Ext, "mp4") {
		return false
	}
	if torrentFileRls.Type != rls.Episode && len(torrentFileRls.SeriesEpisodes()) == 0 {
		return false
	}

	// ignore sample files
	if rls.MustNormalize(torrentFileRls.Group) == "sample" {
		return false
	}

	return true
}

func parseEpisodeFile(path string) rls.Release {
	base := filepath.Base(path)
	ext := filepath.Ext(base)
	return rls.ParseString(strings.TrimSuffix(base, ext) + strings.ToLower(ext))
}

func simplifyWEB(source string) string {
	if source == "WEB-DL" {
		return "WEB"
	}

	return source
}
