// Copyright (c) 2023 - 2024, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package utils

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/moistari/rls"
)

func GetFormattedTitle(r rls.Release) string {
	s := fmt.Sprintf("%s%d%d", rls.MustNormalize(r.Title), r.Year, r.Series)

	return s
}

func FormatSeasonPackTitle(packName string) string {
	// regex for groups that don't need the folder name to be adjusted
	reIgnoredRlsGrps := regexp.MustCompile(`(?i)^(ZR)$`)

	reIllegal := regexp.MustCompile(`(?i)[\\/:"*?<>|]`)
	reAudio := regexp.MustCompile(`(?i)(AAC|DDP)\.(\d\.\d)`)
	reDots := regexp.MustCompile(`(?i)\.+`)

	r := rls.ParseString(packName)

	// check if RlsGrp of release is in ignore regex
	if !reIgnoredRlsGrps.MatchString(r.Group) {
		// remove illegal characters
		packName = reIllegal.ReplaceAllString(packName, "")
		// replace spaces with periods
		packName = strings.ReplaceAll(packName, " ", ".")
		// replace wrong audio naming
		packName = reAudio.ReplaceAllString(packName, "$1$2")
		// replace multiple dots with only one
		packName = reDots.ReplaceAllString(packName, ".")
	}
	return packName
}

func MatchEpToSeasonPackEp(clientEpPath string, clientEpSize int64, torrentEpPath string, torrentEpSize int64) (string, error) {
	epInClientRls := rls.ParseString(filepath.Base(clientEpPath))
	epInTorrentRls := rls.ParseString(filepath.Base(torrentEpPath))

	err := compareEpisodes(epInClientRls, epInTorrentRls)
	if err != nil {
		return "", err
	}

	if clientEpSize != torrentEpSize {
		return "", fmt.Errorf("size mismatch")
	}

	return torrentEpPath, nil
}

func compareEpisodes(episodeRls, torrentEpRls rls.Release) error {
	if episodeRls.Series != torrentEpRls.Series {
		return fmt.Errorf("series mismatch")
	}

	if episodeRls.Episode != torrentEpRls.Episode {
		return fmt.Errorf("episode mismatch")
	}

	if episodeRls.Resolution != torrentEpRls.Resolution {
		return fmt.Errorf("resolution mismatch")
	}

	if rls.MustNormalize(episodeRls.Group) != rls.MustNormalize(torrentEpRls.Group) {
		return fmt.Errorf("group mismatch")
	}

	return nil
}
