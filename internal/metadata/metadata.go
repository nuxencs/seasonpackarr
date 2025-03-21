// Copyright (c) 2023 - 2025, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package metadata

import (
	"fmt"

	"github.com/nuxencs/seasonpackarr/internal/domain"

	"github.com/moistari/rls"
)

type metadataProvider interface {
	episodesInSeason(release rls.Release) (int, error)
}

type MetadataProvider struct {
	tvmazeClient metadataProvider
	tvdbClient   metadataProvider
}

func NewMetadataProvider(metadata domain.Metadata) *MetadataProvider {
	tvmazeClient := newTVMaze()
	var tvdbClient metadataProvider

	if metadata.TVDBAPIKey != "" {
		tvdbClient = newTVDB(metadata.TVDBAPIKey, metadata.TVDBPIN)
	}

	return &MetadataProvider{
		tvmazeClient: tvmazeClient,
		tvdbClient:   tvdbClient,
	}
}

func (m *MetadataProvider) EpisodesInSeason(release rls.Release) (int, error) {
	if episodesTVMaze, err := m.tvmazeClient.episodesInSeason(release); err == nil {
		return episodesTVMaze, nil
	}

	if m.tvdbClient == nil {
		return 0, fmt.Errorf("TVMaze search failed and TVDB client is not available")
	}

	episodesTVDB, err := m.tvdbClient.episodesInSeason(release)
	if err != nil {
		return 0, fmt.Errorf("error fetching episodes from TVDB: %w", err)
	}

	return episodesTVDB, nil
}
