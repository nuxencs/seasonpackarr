// Copyright (c) 2023 - 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package http

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
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
func (p *processor) parseTorrent(ctx context.Context) (statusCode domain.StatusCode, err error) {
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

	if err := p.getClient(ctx, clientCfg, clientName); err != nil {
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
		plan, planStatusCode, err = p.buildImportPlan(ctx, clientName, clientCfg, snapshot)
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

	if !plan.meetsPlannedCoverageThreshold(snapshot) {
		return domain.StatusBelowThreshold, domain.StatusBelowThreshold.Error()
	}

	destinationStarted := time.Now()
	importDestination, err := p.req.Client.ImportDestination(ctx)
	destinationEvent := p.log.Info().
		Bool("successful", err == nil).
		Int64("duration_ms", time.Since(destinationStarted).Milliseconds())
	if err != nil {
		destinationEvent.Err(err).Msg("import destination resolved")
		statusCode := torrentclient.ImportStatusCode(err)
		return statusCode, fmt.Errorf("%s: %w", statusCode, err)
	}
	importRoot := importDestination.SavePath()
	destinationEvent.
		Str("import_root", importRoot).
		Msg("import destination resolved")

	hardlinkStarted := time.Now()
	hardlinkResult := p.createPlanHardlinks(plan, importDestination)
	if hardlinkResult.sourceMissing {
		p.log.Warn().Msg("hardlink source disappeared; refreshing client inventory and import plan")
		invalidateImportCaches(clientName, plan.hashes)

		refreshStarted := time.Now()
		refreshedPlan, refreshStatusCode, refreshErr := p.buildImportPlan(ctx, clientName, clientCfg, snapshot)
		refreshEvent := p.log.Info().
			Bool("successful", refreshErr == nil).
			Int64("duration_ms", time.Since(refreshStarted).Milliseconds())
		if refreshErr != nil {
			refreshEvent.Err(refreshErr).Msg("import plan refreshed after missing hardlink source")
			p.logHardlinkStage(plan, hardlinkResult, hardlinkStarted)
			return refreshStatusCode, refreshErr
		}
		refreshEvent.
			Int("planned_links", len(refreshedPlan.links)).
			Int("total_episodes", refreshedPlan.totalEps).
			Msg("import plan refreshed after missing hardlink source")
		if !refreshedPlan.meetsPlannedCoverageThreshold(snapshot) {
			p.logHardlinkStage(plan, hardlinkResult, hardlinkStarted)
			return domain.StatusBelowThreshold, domain.StatusBelowThreshold.Error()
		}

		plan = refreshedPlan
		hardlinkResult = p.createPlanHardlinks(plan, importDestination)
	}

	p.logHardlinkStage(plan, hardlinkResult, hardlinkStarted)
	if hardlinkResult.linkedCount == 0 {
		return domain.StatusFailedHardlink, domain.StatusFailedHardlink.Error()
	}
	if snapshot.SmartMode {
		coverage := release.PercentOfTotalEpisodes(plan.totalEps, hardlinkResult.linkedCount)
		if coverage < snapshot.SmartModeThreshold {
			return domain.StatusBelowThreshold, domain.StatusBelowThreshold.Error()
		}
	}

	clientImportStarted := time.Now()
	importReport, err := p.req.Client.Import(ctx, torrentclient.ImportRequest{
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

	invalidateImportCaches(clientName, plan.hashes)

	return domain.StatusSuccessfulHardlink, nil
}

type hardlinkAttemptResult struct {
	linkedCount   int
	sourceMissing bool
}

func (p *processor) createPlanHardlinks(plan importPlan, importDestination torrentclient.ImportDestination) hardlinkAttemptResult {
	result := hardlinkAttemptResult{}
	for _, link := range plan.links {
		targetEpPath := importDestination.TargetPath(plan.packName, link.torrentEpPath)
		if err := files.CreateHardlink(link.clientEpPath, targetEpPath); err != nil {
			if _, statErr := os.Stat(link.clientEpPath); errors.Is(statErr, fs.ErrNotExist) {
				result.sourceMissing = true
				p.log.Warn().
					Err(err).
					Str("source", link.clientEpPath).
					Msg("hardlink source is missing")
				continue
			}
			p.log.Error().Err(err).Msgf("error creating hardlink: %s", link.clientEpPath)
			continue
		}
		p.log.Info().Msgf("created or reused hardlink: source(%s), target(%s)", link.clientEpPath, targetEpPath)
		result.linkedCount++
	}
	return result
}

func (p *processor) logHardlinkStage(plan importPlan, result hardlinkAttemptResult, started time.Time) {
	p.log.Info().
		Int("linked_episodes", result.linkedCount).
		Int("planned_links", len(plan.links)).
		Int("total_episodes", plan.totalEps).
		Int64("duration_ms", time.Since(started).Milliseconds()).
		Msg("hardlink stage completed")
}
