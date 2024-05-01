// Copyright (c) 2023 - 2024, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package utils

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"seasonpackarr/internal/torrents"

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

func ReplaceParentFolder(path, newFolder string) string {
	path = filepath.Clean(path)
	if filepath.Dir(path) == string(filepath.Separator) ||
		filepath.Dir(path) == "." {
		return path
	}
	newPath := filepath.Join(filepath.Dir(filepath.Dir(path)), newFolder, filepath.Base(path))
	return newPath
}

func MatchEpToSeasonPackEp(epInClientPath string, epInClientSize int64, torrentEp torrents.Episode) (string, error) {
	episodeRls := rls.ParseString(filepath.Base(epInClientPath))
	torrentEpRls := rls.ParseString(filepath.Base(torrentEp.Name))

	err := compareEpisodes(episodeRls, torrentEpRls)
	if err != nil {
		return epInClientPath, err
	}

	if epInClientSize != torrentEp.Size {
		return epInClientPath, fmt.Errorf("size mismatch")
	}

	return filepath.Join(filepath.Dir(epInClientPath), filepath.Base(torrentEp.Name)), nil
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
