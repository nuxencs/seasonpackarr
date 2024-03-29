// Copyright (c) 2023 - 2024, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package utils

import (
	"fmt"
	"github.com/mrobinsn/go-tvmaze/tvmaze"
)

func GetEpisodesPerSeason(title string, season int) (int, error) {
	totalEpisodes := 0

	show, err := tvmaze.DefaultClient.GetShow(title)
	if err != nil {
		return 0, err
	}

	episodes, err := show.GetEpisodes()
	if err != nil {
		return 0, err
	}

	for _, episode := range episodes {
		if episode.Season == season {
			totalEpisodes++
		}
	}

	if totalEpisodes == 0 {
		return 0, fmt.Errorf("couldn't find episodes in season %d of %q", season, title)
	}

	return totalEpisodes, nil
}
