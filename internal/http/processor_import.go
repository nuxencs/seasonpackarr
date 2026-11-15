// Copyright (c) 2023 - 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package http

import (
	"context"
	"fmt"
	"time"

	"github.com/nuxencs/seasonpackarr/internal/domain"
	"github.com/nuxencs/seasonpackarr/internal/placement"
	"github.com/nuxencs/seasonpackarr/internal/release"
	"github.com/nuxencs/seasonpackarr/internal/torrentclient"
	"github.com/nuxencs/seasonpackarr/internal/torrents"

	"github.com/rs/zerolog"
)

// importSeasonPack is the /api/import path. It names the release and the client
// on the logger of this request, then runs the import.
func (p *processor) importSeasonPack(ctx context.Context) (domain.StatusCode, error) {
	clientName := p.getClientName()

	p.log.UpdateContext(func(c zerolog.Context) zerolog.Context {
		return c.Str("release", p.req.Name).Str("clientname", clientName)
	})

	return p.runImport(ctx, clientName)
}

// runImport reuses an accepted plan or rebuilds it on a cache miss, hardlinks
// the planned episodes, then imports the pack. It adds no log fields: discovery
// calls it on a logger that names the release already, see forResult.
func (p *processor) runImport(ctx context.Context, clientName string) (statusCode domain.StatusCode, err error) {
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
	snapshot := p.cfg.Snapshot()

	clientCfg, ok := snapshot.Clients[clientName]
	if !ok {
		return domain.StatusClientNotFound, domain.StatusClientNotFound.Error()
	}
	unlock, lockErr := lockImport(ctx, *clientCfg)
	if lockErr != nil {
		return domain.StatusGetClientError, lockErr
	}
	defer unlock()
	p.log.Info().Msgf("using %s client", clientName)

	if len(p.req.Torrent) == 0 {
		return domain.StatusTorrentBytesError, domain.StatusTorrentBytesError.Error()
	}

	client, err := p.connect(ctx, clientName, snapshot)
	if err != nil {
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

	if _, gateStatus, gateErr := p.findCandidates(ctx, clientName, snapshot); gateErr != nil {
		return gateStatus, gateErr
	}

	planStarted := time.Now()
	planSource := "cache"
	plan, ok := p.loadImportPlan(clientName, *clientCfg, snapshot.FuzzyMatching, hashes)
	if !ok {
		planSource = "rebuilt"
		var planStatusCode domain.StatusCode
		plan, planStatusCode, err = p.buildImportPlan(ctx, clientName, snapshot)
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
	importDestination, err := client.Adapter().ImportDestination(ctx)
	destinationEvent := p.log.Info().
		Bool("successful", err == nil).
		Int64("duration_ms", time.Since(destinationStarted).Milliseconds())
	if err != nil {
		destinationEvent.Err(err).Msg("import destination resolved")
		statusCode := torrentclient.ImportStatusCode(err)
		return statusCode, fmt.Errorf("%s: %w", statusCode, err)
	}
	importRoot := importDestination.Root()
	destinationEvent.
		Str("import_root", importRoot).
		Msg("import destination resolved")

	hardlinkStarted := time.Now()
	hardlinkResult := p.createPlanHardlinks(plan, importDestination)
	if hardlinkResult.sourceMissing {
		p.log.Warn().Msg("hardlink source disappeared; refreshing client inventory and import plan")
		p.invalidateImportCaches(clientName, plan.hashes)

		refreshStarted := time.Now()
		refreshedPlan, refreshStatusCode, refreshErr := p.buildImportPlan(ctx, clientName, snapshot)
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

	defer p.invalidateClientImports(*clientCfg)
	dataComplete := placement.New(plan.info, importDestination).Complete()
	clientImportStarted := time.Now()
	// A caller disconnect must not interrupt a started client mutation and
	// leave the torrent stopped. Each adapter bounds its own client calls.
	importReport, err := client.Adapter().Import(context.WithoutCancel(ctx), torrentclient.ImportRequest{
		TorrentBytes: plan.torrentBytes,
		SavePath:     importRoot,
		LegacyHash:   plan.hashes.Legacy,
		V2Hash:       plan.hashes.V2,
		HasV1:        plan.hashes.HasV1,
		DataComplete: dataComplete,
	})
	for _, stage := range importReport.Stages {
		p.log.Info().
			Str("stage", stage.Stage.String()).
			Int64("duration_ms", stage.Duration.Milliseconds()).
			Msg("torrent client import stage finished")
	}
	p.log.Info().
		Bool("successful", err == nil).
		Bool("data_complete", dataComplete).
		Int64("duration_ms", time.Since(clientImportStarted).Milliseconds()).
		Msg("torrent client import completed")
	if err != nil {
		statusCode := torrentclient.ImportStatusCode(err)
		return statusCode, fmt.Errorf("%s: %w", statusCode, err)
	}

	p.invalidateImportCaches(clientName, plan.hashes)

	return domain.StatusSuccessfulHardlink, nil
}

type hardlinkAttemptResult struct {
	linkedCount   int
	sourceMissing bool
}

func (p *processor) createPlanHardlinks(plan importPlan, importDestination placement.Destination) hardlinkAttemptResult {
	result := hardlinkAttemptResult{}
	outcomes := placement.New(plan.info, importDestination).Link(plan.links)
	for index, outcome := range outcomes {
		source := plan.links[index].Source
		switch outcome.Status {
		case placement.Linked:
			p.log.Info().Msgf("created or reused hardlink: source(%s), target(%s)", source, outcome.Target)
			result.linkedCount++
		case placement.SourceMissing:
			result.sourceMissing = true
			p.log.Warn().
				Err(outcome.Err).
				Str("source", source).
				Msg("hardlink source is missing")
		default:
			p.log.Error().Err(outcome.Err).Msgf("error creating hardlink: %s", source)
		}
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
