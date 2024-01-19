package release

import (
	"regexp"

	"seasonpackarr/internal/domain"
	"seasonpackarr/internal/utils"

	"github.com/moistari/rls"
	"github.com/rs/zerolog/log"
)

func CheckCandidates(requestrls, child *domain.Entry, compareRepackStatus bool) int {
	rlsRelease := requestrls.R
	rlsInClient := child.R

	// check if season pack and no extension
	if rlsRelease.Type.Is(rls.Series) && rlsRelease.Ext == "" {
		// compare formatted titles
		if compareReleases(rlsInClient, rlsRelease, compareRepackStatus) {
			// check if same episode
			if rlsInClient.Episode == rlsRelease.Episode {
				// release is already in client
				return 210
			}
			// season pack with matching episodes
			return 250
		}
	}
	// not a season pack
	return 211
}

func PercentOfTotalEpisodes(totalEpisodes int, matchedEpisodes []int) float32 {
	if totalEpisodes == 0 {
		return 0
	}
	count := len(matchedEpisodes)
	percent := float32(count) / float32(totalEpisodes)

	return percent
}

func compareReleases(r1, r2 rls.Release, compareRepackStatus bool) bool {
	if rls.MustNormalize(r1.Title) != rls.MustNormalize(r2.Title) {
		log.Info().Msgf("title did not match: %q vs %q", r1.Title, r2.Title)
		return false
	}

	if r1.Year != r2.Year {
		log.Info().Msgf("year did not match: %q vs %q", r1.Year, r2.Year)
		return false
	}

	if r1.Series != r2.Series {
		log.Info().Msgf("season did not match: %q vs %q", r1.Series, r2.Series)
		return false
	}

	if rls.MustNormalize(r1.Resolution) != rls.MustNormalize(r2.Resolution) {
		log.Info().Msgf("resolution did not match: %q vs %q", r1.Resolution, r2.Resolution)
		return false
	}

	if rls.MustNormalize(r1.Source) != rls.MustNormalize(r2.Source) {
		log.Info().Msgf("source did not match: %q vs %q", r1.Source, r2.Source)
		return false
	}

	if rls.MustNormalize(r1.Group) != rls.MustNormalize(r2.Group) {
		log.Info().Msgf("release group did not match: %q vs %q", r1.Group, r2.Group)
		return false
	}

	if !utils.CompareStringSlices(r1.Cut, r2.Cut) {
		log.Info().Msgf("cut did not match: %q vs %q", r1.Cut, r2.Cut)
		return false
	}

	if !utils.CompareStringSlices(r1.Edition, r2.Edition) {
		log.Info().Msgf("edition did not match: %q vs %q", r1.Edition, r2.Edition)
		return false
	}

	if compareRepackStatus {
		if !utils.CompareStringSlices(r1.Other, r2.Other) {
			log.Info().Msgf("repack status did not match: %q vs %q", r1.Other, r2.Other)
			return false
		}
	}

	if !utils.CompareStringSlices(r1.HDR, r2.HDR) {
		log.Info().Msgf("hdr metadata did not match: %q vs %q", r1.HDR, r2.HDR)
		return false
	}

	s1 := getStreamingService(r1)
	s2 := getStreamingService(r2)
	if s1 != s2 {
		log.Info().Msgf("streaming service did not match: %q vs %q", s1, s2)
		return false
	}

	return true
}

func getStreamingService(r rls.Release) string {
	re := regexp.MustCompile(`(?i)(?:\d{3,4}p|Repack\d?|Proper\d?|Real)[-_. ](\w+)[-_. ]WEB`)
	s := re.FindStringSubmatch(r.String())

	if len(s) <= 1 {
		return ""
	}

	return s[1]
}
