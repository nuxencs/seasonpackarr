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

	// Read the client first: `setup filter` sends a single-episode sample and needs 468 for an unreadable client.
	clientInventory, err := c.client.Inventory(ctx)
	if err != nil {
		return nil, outcomeOfError(domain.StatusGetTorrentsError, err)
	}

	requestRls := rls.ParseString(c.release)
	pack, isPack := release.NewSeasonPack(&requestRls, c.cfg.FuzzyMatching)
	if !isPack {
		return nil, outcomeOf(domain.StatusNotASeasonPack)
	}

	var candidates []inventory.Entry
	for filteredEntry := range clientInventory.SameSeason(pack.Season()) {
		switch relation, mismatch := pack.Compare(filteredEntry.Release); relation {
		case release.SamePack:
			return nil, outcomeOf(domain.StatusAlreadyInClient)
		case release.Compatible:
			candidates = append(candidates, filteredEntry)
		case release.OtherReleaseVariant:
			c.log.Info().
				Str("reason", "compatibility_mismatch").
				Str("field", string(mismatch.Field)).
				Interface("want", mismatch.Want).
				Interface("got", mismatch.Got).
				Str("client_release", filteredEntry.Release.String()).
				Msg("client release is not compatible")
		}
	}

	if len(candidates) == 0 {
		return nil, outcomeOf(domain.StatusNoMatches)
	}

	return candidates, Outcome{Status: domain.StatusSuccessfulMatch}
}
