// Copyright (c) 2023 - 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package http

import (
	"fmt"

	"github.com/nuxencs/seasonpackarr/internal/domain"
	"github.com/nuxencs/seasonpackarr/internal/files"
	"github.com/nuxencs/seasonpackarr/internal/release"
	"github.com/nuxencs/seasonpackarr/internal/torrentclient"
	"github.com/nuxencs/seasonpackarr/internal/torrents"

	"github.com/rs/zerolog"
)

// parseTorrent is the /api/parse path. It reuses an accepted plan or rebuilds it
// on a cache miss, hardlinks the planned episodes, then imports the pack.
func (p *processor) parseTorrent() (domain.StatusCode, error) {
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

	if len(p.req.Torrent) == 0 {
		return domain.StatusTorrentBytesError, domain.StatusTorrentBytesError.Error()
	}

	if err := p.getClient(clientCfg, clientName); err != nil {
		return domain.StatusGetClientError, fmt.Errorf("%s: %w", domain.StatusGetClientError, err)
	}

	torrentBytes, err := torrents.DecodeTorrentBytes(p.req.Torrent)
	if err != nil {
		return domain.StatusDecodeTorrentBytesError, fmt.Errorf("%s: %w", domain.StatusDecodeTorrentBytesError, err)
	}
	hashes, err := torrents.InfoHashes(torrentBytes)
	if err != nil {
		return domain.StatusParseTorrentInfoError, fmt.Errorf("%s: %w", domain.StatusParseTorrentInfoError, err)
	}

	plan, ok := p.loadImportPlan(clientName, *clientCfg, snapshot.FuzzyMatching, hashes)
	if !ok {
		var statusCode domain.StatusCode
		plan, statusCode, err = p.buildImportPlan(clientName, clientCfg, snapshot)
		if err != nil {
			return statusCode, err
		}
	}

	if snapshot.SmartMode {
		coverage := release.PercentOfTotalEpisodes(plan.totalEps, len(plan.links))
		if coverage < snapshot.SmartModeThreshold {
			return domain.StatusBelowThreshold, domain.StatusBelowThreshold.Error()
		}
	}

	importDestination, err := p.req.Client.ImportDestination()
	if err != nil {
		statusCode := torrentclient.ImportStatusCode(err)
		return statusCode, fmt.Errorf("%s: %w", statusCode, err)
	}
	importRoot := importDestination.SavePath()
	p.log.Debug().Msgf("resolved import root: %s", importRoot)

	linkedCount := 0
	for _, link := range plan.links {
		targetEpPath := importDestination.TargetPath(plan.packName, link.torrentEpPath)
		if err = files.CreateHardlink(link.clientEpPath, targetEpPath); err != nil {
			p.log.Error().Err(err).Msgf("error creating hardlink: %s", link.clientEpPath)
			continue
		}
		p.log.Info().Msgf("created or reused hardlink: source(%s), target(%s)", link.clientEpPath, targetEpPath)
		linkedCount++
	}

	p.log.Info().Msgf("hardlinked %d/%d episodes from pack", linkedCount, plan.totalEps)
	if linkedCount == 0 {
		return domain.StatusFailedHardlink, domain.StatusFailedHardlink.Error()
	}
	if snapshot.SmartMode {
		coverage := release.PercentOfTotalEpisodes(plan.totalEps, linkedCount)
		if coverage < snapshot.SmartModeThreshold {
			return domain.StatusBelowThreshold, domain.StatusBelowThreshold.Error()
		}
	}

	if err := p.req.Client.Import(torrentclient.ImportRequest{
		TorrentBytes: plan.torrentBytes,
		SavePath:     importRoot,
		LegacyHash:   plan.hashes.Legacy,
		V2Hash:       plan.hashes.V2,
		HasV1:        plan.hashes.HasV1,
	}); err != nil {
		statusCode := torrentclient.ImportStatusCode(err)
		return statusCode, fmt.Errorf("%s: %w", statusCode, err)
	}

	planMap.Delete(importPlanKey(clientName, plan.hashes))
	entryMap.Delete(clientName)

	return domain.StatusSuccessfulHardlink, nil
}
