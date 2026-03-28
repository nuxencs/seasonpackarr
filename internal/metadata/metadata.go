// Copyright (c) 2023 - 2025, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package metadata

import (
	"fmt"
	"sync"

	"github.com/nuxencs/seasonpackarr/internal/domain"
	"github.com/nuxencs/seasonpackarr/internal/logger"

	"github.com/moistari/rls"
	"github.com/rs/zerolog"
)

type provider interface {
	episodesInSeason(release rls.Release) (int, error)
}

type Provider struct {
	log          zerolog.Logger
	tvmazeClient provider
	tvdbClient   provider
}

func NewMetadataProvider(log logger.Logger, metadata domain.Metadata) *Provider {
	tvmaze := newTVMaze()
	var tvdb provider

	if metadata.TVDBAPIKey != "" {
		tvdb = newTVDB(metadata.TVDBAPIKey, metadata.TVDBPIN)
	}

	return &Provider{
		log:          log.With().Logger(),
		tvmazeClient: tvmaze,
		tvdbClient:   tvdb,
	}
}

func (m *Provider) EpisodesInSeason(release rls.Release) (int, error) {
	if m.tvdbClient == nil {
		return m.tvmazeClient.episodesInSeason(release)
	}

	type result struct {
		episodes int
		err      error
	}

	var wg sync.WaitGroup
	wg.Add(2)

	var tvdbResult, tvmazeResult result
	go func() {
		defer wg.Done()
		episodes, err := m.tvdbClient.episodesInSeason(release)
		tvdbResult = result{episodes, err}
	}()

	go func() {
		defer wg.Done()
		episodes, err := m.tvmazeClient.episodesInSeason(release)
		tvmazeResult = result{episodes, err}
	}()

	wg.Wait()

	if tvdbResult.err == nil && tvmazeResult.err == nil {
		if tvdbResult.episodes != tvmazeResult.episodes {
			m.log.Debug().Msgf("episode count differs for %s S%02d: TVDB=%d, TVMaze=%d, using TVDB",
				release.Title, release.Series, tvdbResult.episodes, tvmazeResult.episodes)
		}

		return tvdbResult.episodes, nil
	}

	if tvdbResult.err == nil {
		m.log.Debug().Msgf("TVMaze query failed with error: %v, using TVDB", tvmazeResult.err)
		return tvdbResult.episodes, nil
	}

	if tvmazeResult.err == nil {
		m.log.Debug().Msgf("TVDB query failed with error: %v, using TVMaze", tvdbResult.err)
		return tvmazeResult.episodes, nil
	}

	return 0, fmt.Errorf("failed to get episodes: TVDB error: %w, TVMaze error: %v", tvdbResult.err, tvmazeResult.err)
}
