// Copyright (c) 2023 - 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package http

import (
	"context"
	"fmt"
	"maps"
	stdslices "slices"
	"sync"
	"sync/atomic"
	"time"

	"github.com/nuxencs/seasonpackarr/internal/config"
	"github.com/nuxencs/seasonpackarr/internal/domain"
	"github.com/nuxencs/seasonpackarr/internal/format"
	"github.com/nuxencs/seasonpackarr/internal/logger"
	"github.com/nuxencs/seasonpackarr/internal/release"
	"github.com/nuxencs/seasonpackarr/internal/torrentclient"

	"github.com/autobrr/rls"
	"github.com/puzpuzpuz/xsync/v3"
	"github.com/rs/zerolog"
)

type entry struct {
	torrent torrentclient.Torrent
	release *rls.Release
}

type cachedRelease struct {
	release         *rls.Release
	comparableTitle string
}

type entryCache struct {
	entriesMap    map[string][]entry
	clientConfig  domain.Client
	fuzzyMatching domain.FuzzyMatching
	expiresAt     time.Time
	mu            sync.Mutex
}

type cachedTorrentClient struct {
	config domain.Client
	client torrentclient.TorrentClient
}

// parsedReleases holds the parsed torrent names of one inventory scope on one
// client endpoint. It outlives entryCache, because an import drops the
// inventories and parsing is the costly part of a refresh. Each scan replaces
// it with the names it saw, so it never keeps the names of removed torrents.
type parsedReleases struct {
	fuzzyMatching domain.FuzzyMatching
	releases      map[string]cachedRelease
}

// parsedNamesKey gives each inventory scope of an endpoint its own parsed
// names, so the scan of one scope cannot drop the names of another.
type parsedNamesKey struct {
	endpoint torrentclient.Endpoint
	scope    string
}

func parsedNamesKeyFor(client domain.Client) parsedNamesKey {
	return parsedNamesKey{endpoint: torrentclient.EndpointOf(client), scope: client.Inventory.Key()}
}

const inventoryCacheTTL = 30 * time.Second

// parsedNamesGeneration counts the clean-ups of parsedReleaseMap after config
// reloads. A scan that overlaps a clean-up adds no key, because the clean-up
// can have removed that key as unused.
var parsedNamesGeneration atomic.Uint64

var (
	clientMap        = xsync.NewMapOf[string, cachedTorrentClient]()
	entryMap         = xsync.NewMapOf[string, *entryCache]()
	parsedReleaseMap = xsync.NewMapOf[parsedNamesKey, parsedReleases]()
)

// candidateSeasonPack is the announce-only prefilter. It establishes that the
// client contains at least one release-compatible episode without requesting
// per-torrent file details or requiring the announced torrent bytes.
func (p *processor) candidateSeasonPack(ctx context.Context) (domain.StatusCode, error) {
	clientName := p.getClientName()
	snapshot := p.cfg.Snapshot()

	p.log.UpdateContext(func(c zerolog.Context) zerolog.Context {
		return c.Str("release", p.req.Name).Str("clientname", clientName)
	})

	clientCfg, ok := snapshot.Clients[clientName]
	if !ok {
		return domain.StatusClientNotFound, domain.StatusClientNotFound.Error()
	}

	candidates, statusCode, err := p.findCandidates(ctx, clientName, clientCfg, snapshot)
	if err != nil {
		return statusCode, err
	}
	for _, candidate := range candidates {
		p.log.Debug().
			Str("client_release", candidate.torrent.Name).
			Msg("client release passed candidate gate")
	}

	return domain.StatusSuccessfulMatch, nil
}

func (p *processor) findCandidates(ctx context.Context, clientName string, clientCfg *domain.Client, cfg domain.Config) ([]entry, domain.StatusCode, error) {
	if len(p.req.Name) == 0 {
		return nil, domain.StatusAnnounceNameError, domain.StatusAnnounceNameError.Error()
	}

	if err := p.getClient(ctx, clientCfg, clientName); err != nil {
		return nil, domain.StatusGetClientError, fmt.Errorf("%s: %w", domain.StatusGetClientError, err)
	}

	entries, err := p.getAllTorrents(ctx, clientName, clientCfg, cfg.FuzzyMatching)
	if err != nil {
		return nil, domain.StatusGetTorrentsError, fmt.Errorf("%s: %w", domain.StatusGetTorrentsError, err)
	}

	requestRls := rls.ParseString(p.req.Name)
	filteredEntries, ok := entries[format.ComparableTitle(requestRls, cfg.FuzzyMatching)]
	if !ok {
		return nil, domain.StatusNoMatches, domain.StatusNoMatches.Error()
	}

	candidates := make([]entry, 0, len(filteredEntries))
	for _, filteredEntry := range filteredEntries {
		compareInfo := release.CheckCandidates(requestRls, *filteredEntry.release, cfg.FuzzyMatching)
		switch compareInfo.StatusCode {
		case domain.StatusAlreadyInClient, domain.StatusNotASeasonPack:
			return nil, compareInfo.StatusCode, compareInfo.StatusCode.Error()
		case domain.StatusSuccessfulMatch:
			candidates = append(candidates, filteredEntry)
		default:
			p.log.Info().
				Str("reason", "compatibility_mismatch").
				Str("field", candidateMismatchField(compareInfo.StatusCode)).
				Interface("want", compareInfo.RejectValueA).
				Interface("got", compareInfo.RejectValueB).
				Str("client_release", filteredEntry.release.String()).
				Msg("client release is not compatible")
		}
	}

	if len(candidates) == 0 {
		return nil, domain.StatusNoMatches, domain.StatusNoMatches.Error()
	}

	return candidates, domain.StatusSuccessfulMatch, nil
}

func candidateMismatchField(statusCode domain.StatusCode) string {
	switch statusCode {
	case domain.StatusResolutionMismatch:
		return "resolution"
	case domain.StatusSourceMismatch:
		return "source"
	case domain.StatusRlsGrpMismatch:
		return "release_group"
	case domain.StatusCutMismatch:
		return "cut"
	case domain.StatusEditionMismatch:
		return "edition"
	case domain.StatusRepackStatusMismatch:
		return "repack_status"
	case domain.StatusHdrMismatch:
		return "hdr"
	case domain.StatusStreamingServiceMismatch:
		return "streaming_service"
	default:
		return "unknown"
	}
}

func (p *processor) getClient(ctx context.Context, client *domain.Client, clientName string) error {
	// allow tests (and repeated calls within a request) to inject/keep a client
	if p.req.Client != nil {
		return nil
	}

	if cached, ok := clientMap.Load(clientName); ok && cached.config.Equal(*client) {
		p.req.Client = cached.client
		return nil
	}

	c, err := torrentclient.New(ctx, client)
	if err != nil {
		return fmt.Errorf("failed to create torrent client: %w", err)
	}

	clientMap.Store(clientName, cachedTorrentClient{
		config: client.Clone(),
		client: c,
	})
	p.req.Client = c
	return nil
}

func (p *processor) getAllTorrents(ctx context.Context, clientName string, clientConfig *domain.Client, fuzzyMatching domain.FuzzyMatching) (map[string][]entry, error) {
	entries, _ := entryMap.Compute(clientName, func(current *entryCache, loaded bool) (*entryCache, bool) {
		if loaded && current.clientConfig.Equal(*clientConfig) && current.fuzzyMatching == fuzzyMatching {
			return current, false
		}
		return &entryCache{
			clientConfig:  clientConfig.Clone(),
			fuzzyMatching: fuzzyMatching,
		}, false
	})
	if entries.expiresAt.After(time.Now()) {
		return entries.entriesMap, nil
	}

	entries.mu.Lock()
	defer entries.mu.Unlock()

	if entries.expiresAt.After(time.Now()) {
		return entries.entriesMap, nil
	}
	if latest, ok := entryMap.Load(clientName); ok && latest != entries &&
		latest.clientConfig.Equal(*clientConfig) && latest.fuzzyMatching == fuzzyMatching &&
		latest.expiresAt.After(time.Now()) {
		return latest.entriesMap, nil
	}

	generation := parsedNamesGeneration.Load()
	ts, err := p.req.Client.GetTorrents(ctx)
	if err != nil {
		return nil, err
	}

	refreshed := &entryCache{
		entriesMap:    make(map[string][]entry),
		clientConfig:  clientConfig.Clone(),
		fuzzyMatching: fuzzyMatching,
		expiresAt:     time.Now().Add(inventoryCacheTTL),
	}

	key := parsedNamesKeyFor(*clientConfig)
	// The comparable title depends on the fuzzy matching settings.
	var previous []map[string]cachedRelease
	if own, ok := parsedReleaseMap.Load(key); ok && own.fuzzyMatching == fuzzyMatching {
		previous = append(previous, own.releases)
	}
	// The other scopes of the endpoint follow, so a name in two scopes is parsed once.
	parsedReleaseMap.Range(func(other parsedNamesKey, names parsedReleases) bool {
		if other != key && other.endpoint == key.endpoint && names.fuzzyMatching == fuzzyMatching {
			previous = append(previous, names.releases)
		}
		return true
	})
	releases := make(map[string]cachedRelease, len(ts))
	for _, t := range ts {
		// Cross-seeds repeat a name in the same scan.
		cached, ok := releases[t.Name]
		for _, names := range previous {
			if ok {
				break
			}
			cached, ok = names[t.Name]
		}
		if !ok {
			parsed := rls.ParseString(t.Name)
			cached = cachedRelease{
				release:         &parsed,
				comparableTitle: format.ComparableTitle(parsed, fuzzyMatching),
			}
		}
		releases[t.Name] = cached

		refreshed.entriesMap[cached.comparableTitle] = append(
			refreshed.entriesMap[cached.comparableTitle],
			entry{torrent: t, release: cached.release},
		)
	}

	// Parsed names stay valid after an import invalidation, so publish them then too.
	parsedReleaseMap.Compute(key, func(_ parsedReleases, loaded bool) (parsedReleases, bool) {
		if !loaded && parsedNamesGeneration.Load() != generation {
			return parsedReleases{}, true
		}
		return parsedReleases{fuzzyMatching: fuzzyMatching, releases: releases}, false
	})

	entryMap.Compute(clientName, func(current *entryCache, loaded bool) (*entryCache, bool) {
		// An import can invalidate this scan while GetTorrents is in flight.
		// Do not publish its pre-import inventory after that invalidation.
		if !loaded {
			return nil, true
		}
		if current != entries {
			return current, false
		}
		return refreshed, false
	})
	return refreshed.entriesMap, nil
}

const (
	// inventoryWarmAttempts covers clients that start after seasonpackarr, for example after a host restart.
	inventoryWarmAttempts   = 4
	inventoryWarmRetryDelay = 10 * time.Second
)

// inventoryWarmer reads each client inventory once at startup, so the first
// announce does not log in or parse every torrent name.
type inventoryWarmer struct {
	log        logger.Logger
	cfg        config.Provider
	retryDelay time.Duration
}

// run warms each endpoint in its own goroutine, so a client that times out
// does not delay the others. A failure only logs.
func (w inventoryWarmer) run(ctx context.Context) {
	endpoints := make(map[torrentclient.Endpoint][]string)
	snapshot := w.cfg.Snapshot()
	for _, name := range stdslices.Sorted(maps.Keys(snapshot.Clients)) {
		endpoint := torrentclient.EndpointOf(*snapshot.Clients[name])
		endpoints[endpoint] = append(endpoints[endpoint], name)
	}

	var wg sync.WaitGroup
	for _, names := range endpoints {
		wg.Go(func() {
			// One client at a time, so the first parses the names and the others reuse them.
			for _, name := range names {
				w.warmClient(ctx, name)
			}
		})
	}
	wg.Wait()
}

func (w inventoryWarmer) warmClient(ctx context.Context, name string) {
	for attempt := 1; ; attempt++ {
		// A config reload during the retries applies to the next attempt.
		snapshot := w.cfg.Snapshot()
		client, ok := snapshot.Clients[name]
		if !ok {
			return
		}

		started := time.Now()
		// getClient and getAllTorrents send no notification.
		p := newProcessor(w.log, w.cfg, nil, nil)
		p.req = &request{ClientName: name}
		err := p.getClient(ctx, client, name)
		var entries map[string][]entry
		if err == nil {
			entries, err = p.getAllTorrents(ctx, name, client, snapshot.FuzzyMatching)
		}
		if ctx.Err() != nil {
			return
		}
		if err == nil {
			w.log.Info().
				Str("clientname", name).
				Int("titles", len(entries)).
				Int64("duration_ms", time.Since(started).Milliseconds()).
				Msg("client inventory ready")
			return
		}
		if attempt == inventoryWarmAttempts {
			w.log.Warn().Err(err).Str("clientname", name).Msg("could not read client inventory at startup")
			return
		}
		w.log.Debug().Err(err).Str("clientname", name).Int("attempt", attempt).Msg("client inventory not ready at startup")

		select {
		case <-ctx.Done():
			return
		case <-time.After(w.retryDelay):
		}
	}
}
