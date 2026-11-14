// Copyright (c) 2023 - 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package http

import (
	"context"
	"fmt"

	"github.com/nuxencs/seasonpackarr/internal/domain"
	"github.com/nuxencs/seasonpackarr/internal/inventory"
	"github.com/nuxencs/seasonpackarr/internal/release"

	"github.com/autobrr/rls"
	"github.com/rs/zerolog"
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

	if _, ok := snapshot.Clients[clientName]; !ok {
		return domain.StatusClientNotFound, domain.StatusClientNotFound.Error()
	}

	candidates, statusCode, err := p.findCandidates(ctx, clientName, snapshot)
	if err != nil {
		return statusCode, err
	}
	for _, candidate := range candidates {
		p.log.Debug().
			Str("client_release", candidate.Torrent.Name).
			Msg("client release passed candidate gate")
	}

	return domain.StatusSuccessfulMatch, nil
}

func (p *processor) findCandidates(ctx context.Context, clientName string, cfg domain.Config) ([]inventory.Entry, domain.StatusCode, error) {
	if len(p.req.Name) == 0 {
		return nil, domain.StatusAnnounceNameError, domain.StatusAnnounceNameError.Error()
	}

	client, err := p.connect(ctx, clientName, cfg)
	if err != nil {
		return nil, domain.StatusGetClientError, fmt.Errorf("%s: %w", domain.StatusGetClientError, err)
	}

	clientInventory, err := client.Inventory(ctx)
	if err != nil {
		return nil, domain.StatusGetTorrentsError, fmt.Errorf("%s: %w", domain.StatusGetTorrentsError, err)
	}

	requestRls := rls.ParseString(p.req.Name)
	filteredEntries := clientInventory.SameTitle(requestRls)
	if len(filteredEntries) == 0 {
		return nil, domain.StatusNoMatches, domain.StatusNoMatches.Error()
	}

	candidates := make([]inventory.Entry, 0, len(filteredEntries))
	for _, filteredEntry := range filteredEntries {
		compareInfo := release.CheckCandidates(requestRls, *filteredEntry.Release, cfg.FuzzyMatching)
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
				Str("client_release", filteredEntry.Release.String()).
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
