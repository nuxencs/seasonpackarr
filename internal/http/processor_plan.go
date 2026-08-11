// Copyright (c) 2023 - 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package http

import (
	"fmt"
	"path/filepath"
	"time"

	"github.com/nuxencs/seasonpackarr/internal/domain"
	"github.com/nuxencs/seasonpackarr/internal/release"
	"github.com/nuxencs/seasonpackarr/internal/torrentclient"
	"github.com/nuxencs/seasonpackarr/internal/torrents"

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
func (p *processor) processSeasonPack() domain.Outcome {
	clientName := p.getClientName()
	snapshot := p.cfg.Snapshot()

	p.log.UpdateContext(func(c zerolog.Context) zerolog.Context {
		return c.Str("release", p.req.Name).Str("clientname", clientName)
	})

	clientCfg, ok := snapshot.Clients[clientName]
	if !ok {
		return domain.Failed(domain.ReasonClientNotFound, domain.FaultRequest)
	}
	p.log.Info().Msgf("using %s client", clientName)

	plan, outcome := p.buildImportPlan(clientName, clientCfg, snapshot)
	if outcome.Kind() != domain.OutcomeSuccess {
		return outcome
	}

	if snapshot.SmartMode {
		coverage := release.PercentOfTotalEpisodes(plan.totalEps, len(plan.links))
		p.log.Info().Msgf("found %d/%d (%.2f%%) reusable episodes in announced torrent", len(plan.links), plan.totalEps, coverage*100)
		if coverage < snapshot.SmartModeThreshold {
			return domain.Rejected(domain.ReasonBelowThreshold)
		}
	}

	p.storeImportPlan(clientName, *clientCfg, snapshot.FuzzyMatching, plan)

	return domain.Successful(domain.ReasonMatched)
}

func (p *processor) buildImportPlan(clientName string, clientCfg *domain.Client, cfg domain.Config) (importPlan, domain.Outcome) {
	if len(p.req.Torrent) == 0 {
		return importPlan{}, domain.Failed(domain.ReasonMissingTorrent, domain.FaultRequest)
	}

	candidates, outcome := p.findCandidates(clientName, clientCfg, cfg)
	if outcome.Kind() != domain.OutcomeSuccess {
		return importPlan{}, outcome
	}

	torrentBytes, err := torrents.DecodeTorrentBytes(p.req.Torrent)
	if err != nil {
		return importPlan{}, domain.FailedBecause(domain.ReasonTorrentDecodeFailed, domain.FaultRequest, err)
	}

	torrentInfo, err := torrents.Info(torrentBytes)
	if err != nil {
		return importPlan{}, domain.FailedBecause(domain.ReasonTorrentParseFailed, domain.FaultRequest, err)
	}

	torrentEps, err := torrents.Episodes(torrentInfo)
	if err != nil {
		return importPlan{}, domain.Rejected(domain.ReasonNoEligibleEpisodes)
	}
	eligibleEps := torrentEps[:0]
	for _, torrentEp := range torrentEps {
		if release.IsValidEpisodeFile(torrentEp.Path) {
			eligibleEps = append(eligibleEps, torrentEp)
		}
	}
	if len(eligibleEps) == 0 {
		return importPlan{}, domain.Rejected(domain.ReasonNoEligibleEpisodes)
	}

	hashes, err := torrents.InfoHashes(torrentBytes)
	if err != nil {
		return importPlan{}, domain.FailedBecause(domain.ReasonTorrentParseFailed, domain.FaultRequest, err)
	}

	linksByTarget := make(map[string]plannedLink)
	var firstFileErr error
	for _, candidate := range candidates {
		episodeFile, found, err := p.findEpisodeFile(candidate.torrent.Hash)
		if err != nil {
			p.log.Warn().Err(err).Msgf("could not inspect candidate file: %s", candidate.torrent.Name)
			if firstFileErr == nil {
				firstFileErr = fmt.Errorf("could not inspect candidate %q: %w", candidate.torrent.Name, err)
			}
			continue
		}
		if !found {
			continue
		}
		clientEpPath := filepath.Join(candidate.torrent.SavePath, episodeFile.Name)

		for _, torrentEp := range eligibleEps {
			matchedEpPath, _ := release.MatchEpToSeasonPackEp(clientEpPath, episodeFile.Size, torrentEp.Path, torrentEp.Size)
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
		if firstFileErr != nil {
			return importPlan{}, domain.FailedBecause(domain.ReasonClientFileInspectFailed, domain.FaultDependency, firstFileErr)
		}
		return importPlan{}, domain.Rejected(domain.ReasonTorrentMismatch)
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
	}, domain.Successful(domain.ReasonMatched)
}

func (p *processor) findEpisodeFile(hash string) (torrentclient.File, bool, error) {
	torrentFiles, err := p.req.Client.GetFiles(hash)
	if err != nil {
		return torrentclient.File{}, false, err
	}

	for _, f := range torrentFiles {
		if !release.IsValidEpisodeFile(f.Name) {
			continue
		}

		if f.Size <= 0 {
			continue
		}

		return f, true, nil
	}

	return torrentclient.File{}, false, nil
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
