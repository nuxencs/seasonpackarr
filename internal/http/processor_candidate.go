// Copyright (c) 2023 - 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package http

import (
	"fmt"
	stdslices "slices"
	"sync"
	"time"

	"github.com/nuxencs/seasonpackarr/internal/domain"
	"github.com/nuxencs/seasonpackarr/internal/format"
	"github.com/nuxencs/seasonpackarr/internal/release"
	"github.com/nuxencs/seasonpackarr/internal/torrentclient"
	"github.com/nuxencs/seasonpackarr/pkg/errors"

	"github.com/autobrr/rls"
	"github.com/puzpuzpuz/xsync/v3"
	"github.com/rs/zerolog"
)

type entry struct {
	torrent torrentclient.Torrent
	release rls.Release
}

type entryCache struct {
	entriesMap    map[string][]entry
	rlsMap        map[string]rls.Release
	clientConfig  domain.Client
	fuzzyMatching domain.FuzzyMatching
	expiresAt     time.Time
	mu            sync.Mutex
}

type cachedTorrentClient struct {
	config domain.Client
	client torrentclient.TorrentClient
}

const inventoryCacheTTL = 30 * time.Second

var (
	clientMap = xsync.NewMapOf[string, cachedTorrentClient]()
	entryMap  = xsync.NewMapOf[string, *entryCache]()
)

// candidateSeasonPack is the announce-only prefilter. It establishes that the
// client contains at least one release-compatible episode without requesting
// per-torrent file details or requiring the announced torrent bytes.
func (p *processor) candidateSeasonPack() (domain.StatusCode, error) {
	clientName := p.getClientName()
	snapshot := p.cfg.Snapshot()

	p.log.UpdateContext(func(c zerolog.Context) zerolog.Context {
		return c.Str("release", p.req.Name).Str("clientname", clientName)
	})

	clientCfg, ok := snapshot.Clients[clientName]
	if !ok {
		return domain.StatusClientNotFound, domain.StatusClientNotFound.Error()
	}

	if _, statusCode, err := p.findCandidates(clientName, clientCfg, snapshot); err != nil {
		return statusCode, err
	}

	return domain.StatusSuccessfulMatch, nil
}

func (p *processor) findCandidates(clientName string, clientCfg *domain.Client, cfg domain.Config) ([]entry, domain.StatusCode, error) {
	if len(p.req.Name) == 0 {
		return nil, domain.StatusAnnounceNameError, domain.StatusAnnounceNameError.Error()
	}

	if err := p.getClient(clientCfg, clientName); err != nil {
		return nil, domain.StatusGetClientError, fmt.Errorf("%s: %w", domain.StatusGetClientError, err)
	}

	entries, err := p.getAllTorrents(clientName, clientCfg, cfg.FuzzyMatching)
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
		compareInfo := release.CheckCandidates(requestRls, filteredEntry.release, cfg.FuzzyMatching)
		switch compareInfo.StatusCode {
		case domain.StatusAlreadyInClient, domain.StatusNotASeasonPack:
			return nil, compareInfo.StatusCode, compareInfo.StatusCode.Error()
		case domain.StatusSuccessfulMatch:
			candidates = append(candidates, filteredEntry)
		default:
			p.log.Info().Msgf("%s: request(%s => %v), client(%s => %v)",
				compareInfo.StatusCode, requestRls.String(), compareInfo.RejectValueA,
				filteredEntry.release.String(), compareInfo.RejectValueB)
		}
	}

	if len(candidates) == 0 {
		return nil, domain.StatusNoMatches, domain.StatusNoMatches.Error()
	}

	return candidates, domain.StatusSuccessfulMatch, nil
}

func (p *processor) getClient(client *domain.Client, clientName string) error {
	// allow tests (and repeated calls within a request) to inject/keep a client
	if p.req.Client != nil {
		return nil
	}

	if cached, ok := clientMap.Load(clientName); ok && clientConfigsEqual(cached.config, *client) {
		p.req.Client = cached.client
		return nil
	}

	c, err := torrentclient.New(client)
	if err != nil {
		return errors.Wrap(err, "failed to create torrent client")
	}

	clientMap.Store(clientName, cachedTorrentClient{
		config: cloneClientConfig(*client),
		client: c,
	})
	p.req.Client = c
	return nil
}

func cloneClientConfig(client domain.Client) domain.Client {
	clone := client
	clone.Import.Tags = stdslices.Clone(client.Import.Tags)
	return clone
}

func clientConfigsEqual(a, b domain.Client) bool {
	return a.Type == b.Type &&
		a.Host == b.Host &&
		a.Port == b.Port &&
		a.Username == b.Username &&
		a.Password == b.Password &&
		a.APIKey == b.APIKey &&
		a.Import.SavePath == b.Import.SavePath &&
		stdslices.Equal(a.Import.Tags, b.Import.Tags) &&
		a.Import.Category == b.Import.Category &&
		a.Import.DownloadPath == b.Import.DownloadPath &&
		a.Import.ContentLayout == b.Import.ContentLayout
}

func (p *processor) getAllTorrents(clientName string, clientConfig *domain.Client, fuzzyMatching domain.FuzzyMatching) (map[string][]entry, error) {
	entries, _ := entryMap.Compute(clientName, func(current *entryCache, loaded bool) (*entryCache, bool) {
		if loaded && clientConfigsEqual(current.clientConfig, *clientConfig) && current.fuzzyMatching == fuzzyMatching {
			return current, false
		}
		return &entryCache{
			rlsMap:        make(map[string]rls.Release),
			clientConfig:  cloneClientConfig(*clientConfig),
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
		clientConfigsEqual(latest.clientConfig, *clientConfig) && latest.fuzzyMatching == fuzzyMatching &&
		latest.expiresAt.After(time.Now()) {
		return latest.entriesMap, nil
	}

	ts, err := p.req.Client.GetTorrents()
	if err != nil {
		return nil, err
	}

	refreshed := &entryCache{
		entriesMap:    make(map[string][]entry),
		rlsMap:        make(map[string]rls.Release, len(ts)),
		clientConfig:  cloneClientConfig(*clientConfig),
		fuzzyMatching: fuzzyMatching,
		expiresAt:     time.Now().Add(inventoryCacheTTL),
	}

	for _, t := range ts {
		r, ok := entries.rlsMap[t.Name]
		if !ok {
			r = rls.ParseString(t.Name)
		}
		refreshed.rlsMap[t.Name] = r

		comparableTitle := format.ComparableTitle(r, fuzzyMatching)
		refreshed.entriesMap[comparableTitle] = append(refreshed.entriesMap[comparableTitle], entry{torrent: t, release: r})
	}

	entryMap.Compute(clientName, func(current *entryCache, loaded bool) (*entryCache, bool) {
		if loaded && current != entries {
			return current, false
		}
		return refreshed, false
	})
	return refreshed.entriesMap, nil
}
