// Copyright (c) 2023 - 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package seasonpack

import (
	"context"

	"github.com/nuxencs/seasonpackarr/internal/domain"
	"github.com/nuxencs/seasonpackarr/internal/inventory"
	"github.com/nuxencs/seasonpackarr/internal/release"

	"github.com/autobrr/rls"
)

// Candidate is the candidate check. It establishes from the release name alone
// that the client holds at least one compatible episode. It requests no
// torrent bytes and no per-torrent file details.
func (p *Processor) Candidate(ctx context.Context, client *inventory.Client, releaseName string) Outcome {
	c := p.newCall(client, releaseName)

	candidates, outcome := c.findCandidates(ctx)
	if outcome.Err != nil {
		return outcome
	}
	for _, candidate := range candidates {
		c.log.Debug().
			Str("client_release", candidate.Torrent.Name).
			Msg("client release passed candidate gate")
	}

	return outcome
}

// findCandidates returns the client torrents that are compatible with the
// release name. The match and the import run it again.
func (c *call) findCandidates(ctx context.Context) ([]inventory.Entry, Outcome) {
	if len(c.release) == 0 {
		return nil, outcomeOf(domain.StatusAnnounceNameError)
	}

	clientInventory, err := c.client.Inventory(ctx)
	if err != nil {
		return nil, outcomeOfError(domain.StatusGetTorrentsError, err)
	}

	requestRls := rls.ParseString(c.release)
	var candidates []inventory.Entry
	for filteredEntry := range clientInventory.SameTitle(&requestRls) {
		compareInfo := release.CheckCandidates(requestRls, *filteredEntry.Release, c.cfg.FuzzyMatching)
		switch compareInfo.StatusCode {
		case domain.StatusAlreadyInClient, domain.StatusNotASeasonPack:
			return nil, outcomeOf(compareInfo.StatusCode)
		case domain.StatusSuccessfulMatch:
			candidates = append(candidates, filteredEntry)
		default:
			c.log.Info().
				Str("reason", "compatibility_mismatch").
				Str("field", candidateMismatchField(compareInfo.StatusCode)).
				Interface("want", compareInfo.RejectValueA).
				Interface("got", compareInfo.RejectValueB).
				Str("client_release", filteredEntry.Release.String()).
				Msg("client release is not compatible")
		}
	}

	if len(candidates) == 0 {
		return nil, outcomeOf(domain.StatusNoMatches)
	}

	return candidates, Outcome{Status: domain.StatusSuccessfulMatch}
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
