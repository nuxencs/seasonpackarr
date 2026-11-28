// Copyright (c) 2023 - 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package seasonpack

import (
	"context"
	"time"

	"github.com/nuxencs/seasonpackarr/internal/domain"
	"github.com/nuxencs/seasonpackarr/internal/inventory"
	"github.com/nuxencs/seasonpackarr/internal/placement"
	"github.com/nuxencs/seasonpackarr/internal/torrentclient"
	"github.com/nuxencs/seasonpackarr/internal/torrents"

	"github.com/rs/zerolog"
)

// Import makes the hardlinks of the import plan and imports the season pack
// into the client. It uses the plan of the match, or builds the plan again
// when it has no valid one. Imports to one client endpoint run one at a time.
func (p *Processor) Import(ctx context.Context, client *inventory.Client, releaseName string, torrentBytes []byte) (outcome Outcome) {
	c := p.newCall(client, releaseName)
	totalStarted := time.Now()
	defer func() {
		event := c.log.Info().
			Bool("successful", outcome.Err == nil).
			Int("status_code", int(outcome.Status)).
			Int64("total_duration_ms", time.Since(totalStarted).Milliseconds())
		if outcome.Err != nil {
			event = event.Err(outcome.Err)
		}
		event.Msg("season pack import completed")
	}()

	// The request checks come before the endpoint import lock, so a bad request
	// does not wait for a running import.
	if len(torrentBytes) == 0 {
		return outcomeOf(domain.StatusTorrentBytesError)
	}
	if len(releaseName) == 0 {
		return outcomeOf(domain.StatusAnnounceNameError)
	}

	endpoint := torrentclient.EndpointOf(c.clientConfig())
	unlock, err := p.locks.lock(ctx, endpoint)
	if err != nil {
		return Outcome{Status: domain.StatusGetClientError, Err: err}
	}
	// One deferred call holds both steps, so the drops always come before the
	// release of the lock. A client can add a torrent before it reports a
	// verification or resume failure, so each import attempt drops.
	defer func() {
		if c.clientImportStarted {
			p.inv.DiscardEndpoint(endpoint)
			p.plans.dropEndpoint(endpoint)
		}
		unlock()
	}()

	return c.importLocked(ctx, torrentBytes)
}

// importLocked is the import under the endpoint import lock.
func (c *call) importLocked(ctx context.Context, torrentBytes []byte) Outcome {
	clientName := c.client.Name()
	c.log.Info().Msgf("using %s client", clientName)

	hashes, err := torrents.InfoHashes(torrentBytes)
	if err != nil {
		return outcomeOfError(domain.StatusParseTorrentInfoError, err)
	}

	// Another import can have added the season pack since the match.
	if _, outcome := c.findCandidates(ctx); outcome.Err != nil {
		return outcome
	}

	planStarted := time.Now()
	planSource := "cache"
	outcome := Outcome{Status: domain.StatusSuccessfulMatch}
	plan, ok := c.p.plans.load(c.planScope(), hashes)
	if !ok {
		planSource = "rebuilt"
		plan, _, outcome = c.buildPlan(ctx, TorrentBytes(torrentBytes))
	}
	c.log.Info().
		Str("plan_source", planSource).
		Bool("successful", outcome.Err == nil).
		Int64("duration_ms", time.Since(planStarted).Milliseconds()).
		Msg("import plan resolved")
	if outcome.Err != nil {
		return outcome
	}
	// The smart mode settings can differ from those of the match that stored the plan.
	if planned := plan.coverage(); !planned.meetsThreshold(c.cfg) {
		return outcomeOf(domain.StatusBelowThreshold).withCoverage(planned)
	}

	adapter := c.client.Adapter()
	destinationStarted := time.Now()
	importDestination, err := adapter.ImportDestination(ctx)
	destinationEvent := c.log.Info().
		Bool("successful", err == nil).
		Int64("duration_ms", time.Since(destinationStarted).Milliseconds())
	if err != nil {
		destinationEvent.Err(err).Msg("import destination resolved")
		return outcomeOfError(torrentclient.ImportStatusCode(err), err)
	}
	importRoot := importDestination.Root()
	destinationEvent.
		Str("import_root", importRoot).
		Msg("import destination resolved")

	hardlinkStarted := time.Now()
	hardlinkResult := c.createPlanHardlinks(plan, importDestination)
	if hardlinkResult.sourceMissing {
		c.log.Warn().Msg("hardlink source disappeared; refreshing client inventory and import plan")
		c.p.plans.drop(clientName, plan.hashes)
		c.p.inv.DiscardClient(clientName)

		refreshStarted := time.Now()
		refreshedPlan, _, refreshOutcome := c.buildPlan(ctx, TorrentBytes(plan.torrentBytes))
		refreshEvent := c.log.Info().
			Bool("successful", refreshOutcome.Err == nil).
			Int64("duration_ms", time.Since(refreshStarted).Milliseconds())
		if refreshOutcome.Err != nil {
			refreshEvent.Err(refreshOutcome.Err).Msg("import plan refreshed after missing hardlink source")
			c.logHardlinkStage(plan, hardlinkResult, hardlinkStarted)
			return refreshOutcome
		}
		refreshEvent.
			Int("planned_links", len(refreshedPlan.links)).
			Int("total_episodes", refreshedPlan.totalEps).
			Msg("import plan refreshed after missing hardlink source")

		plan = refreshedPlan
		hardlinkResult = c.createPlanHardlinks(plan, importDestination)
	}

	c.logHardlinkStage(plan, hardlinkResult, hardlinkStarted)
	achieved := Coverage{Reusable: hardlinkResult.linkedCount, Total: plan.totalEps}
	if hardlinkResult.linkedCount == 0 {
		return outcomeOf(domain.StatusFailedHardlink).withCoverage(achieved)
	}
	if !achieved.meetsThreshold(c.cfg) {
		return outcomeOf(domain.StatusBelowThreshold).withCoverage(achieved)
	}

	dataComplete := placement.New(plan.info, importDestination).Complete()
	clientImportStarted := time.Now()
	c.clientImportStarted = true
	// A caller disconnect must not interrupt a started client mutation and
	// leave the torrent stopped. Each adapter bounds its own client calls.
	importReport, err := adapter.Import(context.WithoutCancel(ctx), torrentclient.ImportRequest{
		TorrentBytes: plan.torrentBytes,
		SavePath:     importRoot,
		LegacyHash:   plan.hashes.Legacy,
		V2Hash:       plan.hashes.V2,
		HasV1:        plan.hashes.HasV1,
		DataComplete: dataComplete,
	})
	for _, stage := range importReport.Stages {
		c.log.Info().
			Str("stage", stage.Stage.String()).
			Int64("duration_ms", stage.Duration.Milliseconds()).
			Msg("torrent client import stage finished")
	}
	c.log.Info().
		Bool("successful", err == nil).
		Bool("data_complete", dataComplete).
		Int64("duration_ms", time.Since(clientImportStarted).Milliseconds()).
		Msg("torrent client import completed")
	if err != nil {
		return outcomeOfError(torrentclient.ImportStatusCode(err), err).withCoverage(achieved)
	}

	return Outcome{Status: domain.StatusSuccessfulHardlink}.withCoverage(achieved)
}

type hardlinkAttemptResult struct {
	linkedCount   int
	sourceMissing bool
}

func (c *call) createPlanHardlinks(plan importPlan, importDestination placement.Destination) hardlinkAttemptResult {
	result := hardlinkAttemptResult{}
	outcomes := placement.New(plan.info, importDestination).Link(plan.links)
	for index, outcome := range outcomes {
		source := plan.links[index].Source
		_, fromFolder := plan.folderSources[source]
		withKind := func(event *zerolog.Event) *zerolog.Event {
			if fromFolder {
				return markSourceFolder(event)
			}
			return event
		}
		switch outcome.Status {
		case placement.Linked:
			withKind(c.log.Info()).Msgf("created or reused hardlink: source(%s), target(%s)", source, outcome.Target)
			result.linkedCount++
		case placement.SourceMissing:
			result.sourceMissing = true
			withKind(c.log.Warn()).
				Err(outcome.Err).
				Str("source", source).
				Msg("hardlink source is missing")
		default:
			withKind(c.log.Error()).Err(outcome.Err).Msgf("error creating hardlink: %s", source)
		}
	}
	return result
}

func (c *call) logHardlinkStage(plan importPlan, result hardlinkAttemptResult, started time.Time) {
	c.log.Info().
		Int("linked_episodes", result.linkedCount).
		Int("planned_links", len(plan.links)).
		Int("total_episodes", plan.totalEps).
		Int64("duration_ms", time.Since(started).Milliseconds()).
		Msg("hardlink stage completed")
}
