// Copyright (c) 2023 - 2025, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package metadata

import (
	"fmt"
	"sync"
	"time"

	"github.com/nuxencs/seasonpackarr/internal/domain"
	"github.com/nuxencs/seasonpackarr/internal/logger"

	"github.com/moistari/rls"
	"github.com/rs/zerolog"
	"golang.org/x/sync/singleflight"
)

// episodeCacheTTL bounds how long a fetched episode count is reused. It covers
// both announce bursts of the same season pack, which arrive within seconds of
// each other, and cross-seed propagation, where the same pack shows up on other
// trackers over the following hours. Provider episode counts are near-static,
// so staleness is a minor concern next to the saved calls.
const episodeCacheTTL = 6 * time.Hour

type provider interface {
	episodesInSeason(release rls.Release) (int, error)
}

type cacheEntry struct {
	episodes  int
	expiresAt time.Time
}

type Provider struct {
	log          zerolog.Logger
	tvmazeClient provider
	tvdbClient   provider
	cache        map[string]cacheEntry
	cacheMu      sync.Mutex
	group        singleflight.Group
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
		cache:        make(map[string]cacheEntry),
	}
}

// EpisodesInSeason returns the number of episodes in a season of a show,
// caching successful lookups so repeated announces of the same season pack,
// e.g. from cross-seeded trackers, don't trigger duplicate provider calls.
// Concurrent lookups for the same key are collapsed into a single fetch.
func (m *Provider) EpisodesInSeason(release rls.Release) (int, error) {
	key := fmt.Sprintf("%s|%d|%d", rls.MustNormalize(release.Title), release.Year, release.Series)

	m.cacheMu.Lock()
	entry, ok := m.cache[key]
	m.cacheMu.Unlock()

	if ok && time.Now().Before(entry.expiresAt) {
		m.log.Debug().Msgf("using cached episode count for %s S%02d: %d",
			release.Title, release.Series, entry.episodes)
		return entry.episodes, nil
	}

	episodes, err, _ := m.group.Do(key, func() (any, error) {
		episodes, err := m.fetchEpisodesInSeason(release)
		if err != nil {
			return 0, err
		}

		// sweep expired entries while holding the lock for the store, so the
		// cache never grows beyond the keys seen in the last TTL window; the
		// read path stays iteration-free
		m.cacheMu.Lock()
		now := time.Now()
		for k, e := range m.cache {
			if now.After(e.expiresAt) {
				delete(m.cache, k)
			}
		}
		m.cache[key] = cacheEntry{episodes: episodes, expiresAt: now.Add(episodeCacheTTL)}
		m.cacheMu.Unlock()

		return episodes, nil
	})
	if err != nil {
		return 0, err
	}

	return episodes.(int), nil
}

func (m *Provider) fetchEpisodesInSeason(release rls.Release) (int, error) {
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
