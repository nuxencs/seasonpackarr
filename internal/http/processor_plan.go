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
	unmatched    []release.EpisodeUnmatched
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
	coverage := float32(0)
	if plan.totalEps > 0 {
		coverage = p.logImportPlan(plan, snapshot)
	}
	if err != nil {
		return statusCode, err
	}

	if snapshot.SmartMode {
		if coverage < snapshot.SmartModeThreshold {
			return domain.StatusBelowThreshold, domain.StatusBelowThreshold.Error()
		}
	}

	p.storeImportPlan(clientName, *clientCfg, snapshot.FuzzyMatching, plan)

	return domain.StatusSuccessfulMatch, nil
}

func (p *processor) logImportPlan(plan importPlan, cfg domain.Config) float32 {
	for _, unmatched := range plan.unmatched {
		event := p.log.Info().
			Str("torrent_path", unmatched.TorrentPath).
			Str("reason", string(unmatched.Reason))
		if unmatched.ClosestClientPath != "" {
			event = event.Str("closest_client_path", unmatched.ClosestClientPath)
		}
		if len(unmatched.Mismatches) > 0 {
			mismatches := make([]map[string]any, len(unmatched.Mismatches))
			for index, mismatch := range unmatched.Mismatches {
				mismatches[index] = map[string]any{
					"field": string(mismatch.Field),
					"want":  mismatch.Want,
					"got":   mismatch.Got,
				}
			}
			event = event.Interface("mismatches", mismatches)
		}
		event.Msg("torrent episode is not reusable")
	}

	coverage := release.PercentOfTotalEpisodes(plan.totalEps, len(plan.links))
	summary := p.log.Info().
		Int("reusable_episodes", len(plan.links)).
		Int("total_episodes", plan.totalEps).
		Int("unmatched_episodes", len(plan.unmatched)).
		Float32("coverage_percent", coverage*100)
	if cfg.SmartMode {
		summary = summary.Float32("threshold_percent", cfg.SmartModeThreshold*100)
	}
	summary.Msg("season pack import plan built")
	return coverage
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
	eligibleEps := make([]release.EpisodeFile, 0, len(torrentEps))
	for _, torrentEp := range torrentEps {
		if episode, ok := release.ParseEpisodeFile(torrentEp.Path, torrentEp.Size); ok {
			eligibleEps = append(eligibleEps, episode)
		}
	}
	if len(eligibleEps) == 0 {
		return importPlan{}, domain.StatusFailedMatchToTorrentEps, domain.StatusFailedMatchToTorrentEps.Error()
	}

	hashes, err := torrents.InfoHashes(torrentBytes)
	if err != nil {
		return importPlan{}, domain.StatusParseTorrentInfoError, fmt.Errorf("%s: %w", domain.StatusParseTorrentInfoError, err)
	}

	matcher := release.NewEpisodeMatcher(eligibleEps)
	matchResult := matcher.Match(p.getEpisodeFiles(candidates))

	links := make([]plannedLink, 0, len(matchResult.Matches))
	for _, match := range matchResult.Matches {
		links = append(links, plannedLink{
			clientEpPath:  match.ClientPath,
			torrentEpPath: match.TorrentPath,
		})
	}

	plan := importPlan{
		torrentBytes: torrentBytes,
		hashes:       hashes,
		packName:     torrentInfo.BestName(),
		links:        links,
		unmatched:    matchResult.Unmatched,
		totalEps:     len(eligibleEps),
	}
	if len(matchResult.Matches) == 0 {
		return plan, domain.StatusFailedMatchToTorrentEps, domain.StatusFailedMatchToTorrentEps.Error()
	}
	return plan, domain.StatusSuccessfulMatch, nil
}

func (p *processor) getEpisodeFiles(candidates []entry) []release.EpisodeFile {
	hashes := make([]string, len(candidates))
	for index, candidate := range candidates {
		hashes[index] = candidate.torrent.Hash
	}

	results := p.req.Client.GetFiles(hashes)
	episodes := make([]release.EpisodeFile, 0, len(results))
	for index, candidate := range candidates {
		if index >= len(results) {
			p.log.Error().Msgf("torrent client omitted file result for %s", candidate.torrent.Name)
			continue
		}

		result := results[index]
		if !strings.EqualFold(result.Hash, candidate.torrent.Hash) {
			p.log.Error().Msgf("torrent client returned out-of-order file result for %s", candidate.torrent.Name)
			continue
		}
		if result.Err != nil {
			p.log.Error().Err(result.Err).Msgf("error getting file info: %s", candidate.torrent.Name)
			continue
		}

		episode, err := episodeFileFromFiles(result.Files, candidate.torrent.SavePath)
		if err != nil {
			p.log.Error().Err(err).Msgf("error getting file info: %s", candidate.torrent.Name)
			continue
		}
		episodes = append(episodes, episode)
	}
	return episodes
}

func episodeFileFromFiles(files []torrentclient.File, savePath string) (release.EpisodeFile, error) {
	for _, f := range files {
		episode, ok := release.ParseEpisodeFile(filepath.Join(savePath, f.Name), f.Size)
		if !ok {
			continue
		}
		if f.Size == 0 {
			return release.EpisodeFile{}, errors.New("file size is empty")
		}
		return episode, nil
	}

	return release.EpisodeFile{}, errors.New("file name is empty")
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
