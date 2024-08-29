// Copyright (c) 2023 - 2024, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package utils

import (
	"fmt"

	"seasonpackarr/pkg/errors"

	"github.com/mrobinsn/go-tvmaze/tvmaze"
)

func GetEpisodesPerSeason(title string, season int) (int, error) {
	totalEpisodes := 0

	show, err := tvmaze.DefaultClient.GetShow(normalizeTitle(title))
	if err != nil {
		return 0, errors.Wrap(err, "failed to find show on tvmaze")
	}

	episodes, err := show.GetEpisodes()
	if err != nil {
		return 0, errors.Wrap(err, "failed to get episodes from tvmaze")
	}

	for _, episode := range episodes {
		if episode.Season == season {
			totalEpisodes++
		}
	}

	if totalEpisodes == 0 {
		return 0, fmt.Errorf("failed to find episodes in season %d of %q", season, title)
	}

	return totalEpisodes, nil
}
