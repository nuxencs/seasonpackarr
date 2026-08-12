// Copyright (c) 2023 - 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package http

import (
	"fmt"
	"time"

	"github.com/nuxencs/seasonpackarr/internal/domain"
	"github.com/nuxencs/seasonpackarr/internal/files"
	"github.com/nuxencs/seasonpackarr/internal/release"
	"github.com/nuxencs/seasonpackarr/internal/torrentclient"
	"github.com/nuxencs/seasonpackarr/internal/torrents"

	"github.com/rs/zerolog"
)

// parseTorrent is the /api/parse path. It reuses an accepted plan or rebuilds it
// on a cache miss, hardlinks the planned episodes, then imports the pack.
func (p *processor) parseTorrent() (statusCode domain.StatusCode, err error) {
	totalStarted := time.Now()
	defer func() {
		event := p.log.Info().
			Bool("successful", err == nil).
			Int("status_code", int(statusCode)).
			Int64("total_duration_ms", time.Since(totalStarted).Milliseconds())
		if err != nil {
			event = event.Err(err)
		}
		event.Msg("season pack import completed")
	}()
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

	planStarted := time.Now()
	planSource := "cache"
	plan, ok := p.loadImportPlan(clientName, *clientCfg, snapshot.FuzzyMatching, hashes)
	if !ok {
		planSource = "rebuilt"
		var planStatusCode domain.StatusCode
		plan, planStatusCode, err = p.buildImportPlan(clientName, clientCfg, snapshot)
		statusCode = planStatusCode
	}
	p.log.Info().
		Str("plan_source", planSource).
		Bool("successful", err == nil).
		Int64("duration_ms", time.Since(planStarted).Milliseconds()).
		Msg("import plan resolved")
	if err != nil {
		return statusCode, err
	}

	if snapshot.SmartMode {
		coverage := release.PercentOfTotalEpisodes(plan.totalEps, len(plan.links))
		if coverage < snapshot.SmartModeThreshold {
			return domain.StatusBelowThreshold, domain.StatusBelowThreshold.Error()
		}
	}

	destinationStarted := time.Now()
	importDestination, err := p.req.Client.ImportDestination()
	if err != nil {
		statusCode := torrentclient.ImportStatusCode(err)
		return statusCode, fmt.Errorf("%s: %w", statusCode, err)
	}
	importRoot := importDestination.SavePath()
	p.log.Debug().
		Str("import_root", importRoot).
		Int64("duration_ms", time.Since(destinationStarted).Milliseconds()).
		Msg("import destination resolved")

	hardlinkStarted := time.Now()
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
	p.log.Info().
		Int("linked_episodes", linkedCount).
		Int("planned_links", len(plan.links)).
		Int("total_episodes", plan.totalEps).
		Int64("duration_ms", time.Since(hardlinkStarted).Milliseconds()).
		Msg("hardlink stage completed")
	if linkedCount == 0 {
		return domain.StatusFailedHardlink, domain.StatusFailedHardlink.Error()
	}
	if snapshot.SmartMode {
		coverage := release.PercentOfTotalEpisodes(plan.totalEps, linkedCount)
		if coverage < snapshot.SmartModeThreshold {
			return domain.StatusBelowThreshold, domain.StatusBelowThreshold.Error()
		}
	}

	clientImportStarted := time.Now()
	importReport, err := p.req.Client.Import(torrentclient.ImportRequest{
		TorrentBytes: plan.torrentBytes,
		SavePath:     importRoot,
		LegacyHash:   plan.hashes.Legacy,
		V2Hash:       plan.hashes.V2,
		HasV1:        plan.hashes.HasV1,
	})
	for _, stage := range importReport.Stages {
		p.log.Info().
			Str("stage", stage.Stage.String()).
			Int64("duration_ms", stage.Duration.Milliseconds()).
			Msg("torrent client import stage finished")
	}
	p.log.Info().
		Bool("successful", err == nil).
		Int64("duration_ms", time.Since(clientImportStarted).Milliseconds()).
		Msg("torrent client import completed")
	if err != nil {
		statusCode := torrentclient.ImportStatusCode(err)
		return statusCode, fmt.Errorf("%s: %w", statusCode, err)
	}

	planMap.Delete(importPlanKey(clientName, plan.hashes))
	entryMap.Delete(clientName)

	return domain.StatusSuccessfulHardlink, nil
}
