// Copyright (c) 2023 - 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package http

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/nuxencs/seasonpackarr/internal/domain"
	"github.com/nuxencs/seasonpackarr/internal/release"
	"github.com/nuxencs/seasonpackarr/internal/torrentclient"
	"github.com/nuxencs/seasonpackarr/internal/torrents"
	"github.com/nuxencs/seasonpackarr/pkg/errors"

	"github.com/puzpuzpuz/xsync/v3"
	"github.com/rs/zerolog"
)

type plannedLink struct {
	clientEpPath  string
	torrentEpPath string
}

type importPlan struct {
	torrentBytes []byte
	hashes       torrents.Hashes
	packName     string
	links        []plannedLink
	totalEps     int
}

type cachedImportPlan struct {
	plan          importPlan
	releaseName   string
	clientConfig  domain.Client
	fuzzyMatching domain.FuzzyMatching
	expiresAt     time.Time
}

type importPlanCacheKey struct {
	clientName string
	hashes     torrents.Hashes
}

const importPlanCacheTTL = 2 * time.Minute

var planMap = xsync.NewMapOf[importPlanCacheKey, cachedImportPlan]()

// processSeasonPack is the /api/pack gate. It builds an exact torrent-aware
// import plan but has no filesystem or client side effects. Hardlinking and
// importing happen on /api/parse.
func (p *processor) processSeasonPack() (domain.StatusCode, error) {
	clientName := p.getClientName()
	snapshot := p.cfg.Snapshot()

	p.log.UpdateContext(func(c zerolog.Context) zerolog.Context {
		return c.Str("release", p.req.Name).Str("clientname", clientName)
	})

	clientCfg, ok := snapshot.Clients[clientName]
	if !ok {
		return domain.StatusClientNotFound, domain.StatusClientNotFound.Error()
	}
	p.log.Info().Msgf("using %s client", clientName)

	plan, statusCode, err := p.buildImportPlan(clientName, clientCfg, snapshot)
	if err != nil {
		return statusCode, err
	}

	if snapshot.SmartMode {
		coverage := release.PercentOfTotalEpisodes(plan.totalEps, len(plan.links))
		p.log.Info().Msgf("found %d/%d (%.2f%%) reusable episodes in announced torrent", len(plan.links), plan.totalEps, coverage*100)
		if coverage < snapshot.SmartModeThreshold {
			return domain.StatusBelowThreshold, domain.StatusBelowThreshold.Error()
		}
	}

	p.storeImportPlan(clientName, *clientCfg, snapshot.FuzzyMatching, plan)

	return domain.StatusSuccessfulMatch, nil
}

func (p *processor) buildImportPlan(clientName string, clientCfg *domain.Client, cfg domain.Config) (importPlan, domain.StatusCode, error) {
	if len(p.req.Torrent) == 0 {
		return importPlan{}, domain.StatusTorrentBytesError, domain.StatusTorrentBytesError.Error()
	}

	candidates, statusCode, err := p.findCandidates(clientName, clientCfg, cfg)
	if err != nil {
		return importPlan{}, statusCode, err
	}

	torrentBytes, err := torrents.DecodeTorrentBytes(p.req.Torrent)
	if err != nil {
		return importPlan{}, domain.StatusDecodeTorrentBytesError, fmt.Errorf("%s: %w", domain.StatusDecodeTorrentBytesError, err)
	}

	torrentInfo, err := torrents.Info(torrentBytes)
	if err != nil {
		return importPlan{}, domain.StatusParseTorrentInfoError, fmt.Errorf("%s: %w", domain.StatusParseTorrentInfoError, err)
	}

	torrentEps, err := torrents.Episodes(torrentInfo)
	if err != nil {
		return importPlan{}, domain.StatusGetEpisodesError, fmt.Errorf("%s: %w", domain.StatusGetEpisodesError, err)
	}
	eligibleEps := torrentEps[:0]
	for _, torrentEp := range torrentEps {
		if release.IsValidEpisodeFile(torrentEp.Path) {
			eligibleEps = append(eligibleEps, torrentEp)
		}
	}
	if len(eligibleEps) == 0 {
		return importPlan{}, domain.StatusFailedMatchToTorrentEps, domain.StatusFailedMatchToTorrentEps.Error()
	}

	hashes, err := torrents.InfoHashes(torrentBytes)
	if err != nil {
		return importPlan{}, domain.StatusParseTorrentInfoError, fmt.Errorf("%s: %w", domain.StatusParseTorrentInfoError, err)
	}

	fileResults := p.getFiles(candidates)
	linksByTarget := make(map[string]plannedLink)
	for index, candidate := range candidates {
		if index >= len(fileResults) {
			p.log.Error().Msgf("torrent client omitted file result for %s", candidate.torrent.Name)
			continue
		}

		result := fileResults[index]
		if !strings.EqualFold(result.Hash, candidate.torrent.Hash) {
			p.log.Error().Msgf("torrent client returned out-of-order file result for %s", candidate.torrent.Name)
			continue
		}
		if result.Err != nil {
			p.log.Error().Err(result.Err).Msgf("error getting file info: %s", candidate.torrent.Name)
			continue
		}

		fileName, size, err := episodeFileFromFiles(result.Files)
		if err != nil {
			p.log.Error().Err(err).Msgf("error getting file info: %s", candidate.torrent.Name)
			continue
		}
		clientEpPath := filepath.Join(candidate.torrent.SavePath, fileName)

		for _, torrentEp := range eligibleEps {
			matchedEpPath, _ := release.MatchEpToSeasonPackEp(clientEpPath, size, torrentEp.Path, torrentEp.Size)
			if matchedEpPath == "" {
				continue
			}
			if _, exists := linksByTarget[matchedEpPath]; !exists {
				linksByTarget[matchedEpPath] = plannedLink{clientEpPath: clientEpPath, torrentEpPath: matchedEpPath}
			}
			break
		}
	}

	if len(linksByTarget) == 0 {
		return importPlan{}, domain.StatusFailedMatchToTorrentEps, domain.StatusFailedMatchToTorrentEps.Error()
	}

	links := make([]plannedLink, 0, len(linksByTarget))
	for _, torrentEp := range eligibleEps {
		if link, ok := linksByTarget[torrentEp.Path]; ok {
			links = append(links, link)
		}
	}

	return importPlan{
		torrentBytes: torrentBytes,
		hashes:       hashes,
		packName:     torrentInfo.BestName(),
		links:        links,
		totalEps:     len(eligibleEps),
	}, domain.StatusSuccessfulMatch, nil
}

func (p *processor) getFiles(candidates []entry) []torrentclient.FileResult {
	hashes := make([]string, len(candidates))
	for index, candidate := range candidates {
		hashes[index] = candidate.torrent.Hash
	}
	return p.req.Client.GetFiles(hashes)
}

func episodeFileFromFiles(torrentFiles []torrentclient.File) (string, int64, error) {
	var fileName string
	var size int64
	for _, f := range torrentFiles {
		if !release.IsValidEpisodeFile(f.Name) {
			continue
		}

		fileName = f.Name
		size = f.Size
		break
	}
	switch {
	case len(fileName) == 0:
		return "", 0, errors.New("file name is empty")
	case size == 0:
		return "", 0, errors.New("file size is empty")
	}

	return fileName, size, nil
}

func importPlanKey(clientName string, hashes torrents.Hashes) importPlanCacheKey {
	return importPlanCacheKey{clientName: clientName, hashes: hashes}
}

func (p *processor) storeImportPlan(clientName string, clientCfg domain.Client, fuzzyMatching domain.FuzzyMatching, plan importPlan) {
	now := time.Now()
	planMap.Range(func(key importPlanCacheKey, cached cachedImportPlan) bool {
		if now.After(cached.expiresAt) {
			planMap.Delete(key)
		}
		return true
	})
	planMap.Store(importPlanKey(clientName, plan.hashes), cachedImportPlan{
		plan:          plan,
		releaseName:   p.req.Name,
		clientConfig:  cloneClientConfig(clientCfg),
		fuzzyMatching: fuzzyMatching,
		expiresAt:     now.Add(importPlanCacheTTL),
	})
}

func (p *processor) loadImportPlan(clientName string, clientCfg domain.Client, fuzzyMatching domain.FuzzyMatching, hashes torrents.Hashes) (importPlan, bool) {
	key := importPlanKey(clientName, hashes)
	cached, ok := planMap.Load(key)
	if !ok {
		return importPlan{}, false
	}
	if time.Now().After(cached.expiresAt) || cached.releaseName != p.req.Name || !clientConfigsEqual(cached.clientConfig, clientCfg) || cached.fuzzyMatching != fuzzyMatching {
		planMap.Delete(key)
		return importPlan{}, false
	}
	return cached.plan, true
}
