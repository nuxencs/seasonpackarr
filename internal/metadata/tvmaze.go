// Copyright (c) 2023 - 2025, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package metadata

import (
	"fmt"

	"github.com/nuxencs/seasonpackarr/pkg/errors"

	"github.com/moistari/rls"
	"github.com/mrobinsn/go-tvmaze/tvmaze"
)

var errNotFound = "received error status code (404): 404 Not Found"

type tvmazeClient struct{}

func newTVMaze() provider {
	return &tvmazeClient{}
}

// episodesInSeason returns the number of episodes in a season of a show.
func (t *tvmazeClient) episodesInSeason(release rls.Release) (int, error) {
	// try finding the show with the parsed title first
	show, showErr := tvmaze.DefaultClient.GetShow(release.Title)
	if showErr != nil {
		// retry with the normalized title if the parsed title fails
		if showErr.Error() != errNotFound {
			return 0, fmt.Errorf("failed to find show %q on tvmaze: %w", release.Title, showErr)

		}

		show, showErr = tvmaze.DefaultClient.GetShow(rls.MustNormalize(release.Title))
		if showErr != nil {
			return 0, fmt.Errorf("failed to find show %q on tvmaze: %w", release.Title, showErr)
		}
	}

	episodes, err := show.GetEpisodes()
	if err != nil {
		return 0, errors.Wrap(err, "failed to get episodes from tvmaze")
	}

	var totalEpisodes int
	for _, episode := range episodes {
		if episode.Season == release.Series {
			totalEpisodes++
		}
	}

	if totalEpisodes == 0 {
		return 0, fmt.Errorf("failed to find episodes in season %d of %q", release.Series, release.Title)
	}

	return totalEpisodes, nil
}
