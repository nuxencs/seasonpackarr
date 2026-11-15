// Copyright (c) 2023 - 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

// Package seasonpack owns the three stages of a season pack: the candidate
// check, the match, and the import. The webhook handlers and Prowlarr
// discovery are its callers.
package seasonpack

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/nuxencs/seasonpackarr/internal/domain"
	"github.com/nuxencs/seasonpackarr/internal/inventory"
	"github.com/nuxencs/seasonpackarr/internal/logger"
	"github.com/nuxencs/seasonpackarr/internal/release"

	"github.com/rs/zerolog"
)

// Processor runs the candidate check, the match, and the import. It holds the
// import plans of accepted matches and the endpoint import locks. It is safe
// for concurrent use.
type Processor struct {
	log   zerolog.Logger
	inv   *inventory.Store
	plans planCache
	locks endpointLocks
}

// New returns a processor with no import plans. inv is the store of the client
// handles that the operations get.
func New(log logger.Logger, inv *inventory.Store) *Processor {
	return &Processor{
		log:   log.With().Str("module", "processor").Logger(),
		inv:   inv,
		plans: newPlanCache(),
	}
}

// Outcome is the result of one operation.
type Outcome struct {
	// Status is the status code of the API status table.
	Status domain.StatusCode
	// Err is nil for an accepted season pack.
	Err error
	// Coverage is nil when the operation matched no torrent episodes.
	Coverage *Coverage
}

// Coverage counts the episodes of a torrent and those that reusable episodes supply.
type Coverage struct {
	Reusable int
	Total    int
}

// meetsThreshold is the coverage rule of smart mode. The plan build uses it for
// the planned coverage, and the import for the achieved coverage after the hardlinks.
func (c Coverage) meetsThreshold(cfg domain.Config) bool {
	if !cfg.SmartMode {
		return true
	}
	return c.percent() >= cfg.SmartModeThreshold
}

// percent returns the coverage as a share from 0 to 1.
func (c Coverage) percent() float32 {
	return release.PercentOfTotalEpisodes(c.Total, c.Reusable)
}

// Class is the kind of an outcome, for the log level and the search report.
type Class int

const (
	// Accepted is an outcome with no error.
	Accepted Class = iota + 1
	// Rejected is an expected rejection of a season pack.
	Rejected
	// Failed is each other error.
	Failed
)

// Class returns Rejected for a status below 400 and for status 445, which
// reports a season pack with no reusable episode.
func (o Outcome) Class() Class {
	switch {
	case o.Err == nil:
		return Accepted
	case o.Status.Code() < http.StatusBadRequest || o.Status == domain.StatusFailedMatchToTorrentEps:
		return Rejected
	default:
		return Failed
	}
}

// outcomeOf returns an outcome with the text of status as its error.
func outcomeOf(status domain.StatusCode) Outcome {
	return Outcome{Status: status, Err: status.Error()}
}

// outcomeOfError returns an outcome whose error is err after the text of status.
func outcomeOfError(status domain.StatusCode, err error) Outcome {
	return Outcome{Status: status, Err: fmt.Errorf("%s: %w", status, err)}
}

// withCoverage returns the outcome with coverage.
func (o Outcome) withCoverage(coverage Coverage) Outcome {
	o.Coverage = &coverage
	return o
}

// The match with a fetch stops with one of these errors before the fetch,
// because a fetch can cost a tracker request.
var (
	// ErrNoSourceOnDisk has status 445: no client episode file is on disk.
	ErrNoSourceOnDisk = errors.New("no client episode file is on disk with the declared size")
	// ErrFileDetailRead has status 468: the client did not return the file
	// details of each candidate.
	ErrFileDetailRead = errors.New("could not read episode file details from torrent client")
)

// Torrent is the torrent of a match, in one of two forms. TorrentBytes and
// TorrentFetch make it.
type Torrent struct {
	bytes []byte
	fetch FetchFunc
}

// FetchFunc gets the bytes of a torrent on demand.
type FetchFunc func(ctx context.Context) ([]byte, error)

// TorrentBytes is a torrent that the caller has in hand, as for an announce.
// The match reads the torrent first. It logs a failed file-detail read and
// continues with the client episode files that it could read, because an
// announce comes once, and a lost season pack is worse than one episode download.
func TorrentBytes(data []byte) Torrent {
	return Torrent{bytes: data}
}

// TorrentFetch is a torrent that the match gets on demand, as for a search
// result. The match reads the client episode files first and calls fetch one
// time, only when each file-detail read worked and at least one file is on
// disk. Else it returns ErrFileDetailRead or ErrNoSourceOnDisk, because the
// caller can try the result again later.
func TorrentFetch(fetch FetchFunc) Torrent {
	return Torrent{fetch: fetch}
}

// call holds the inputs and the logger of one operation. Each operation makes
// its own, so no stage reads the input or the log fields of another call.
type call struct {
	p       *Processor
	log     zerolog.Logger
	client  *inventory.Client
	cfg     domain.Config
	release string
	// clientImportStarted is true from the first client import call of an import.
	clientImportStarted bool
}

func (p *Processor) newCall(client *inventory.Client, releaseName string) *call {
	return &call{
		p:       p,
		log:     p.log.With().Str("release", releaseName).Str("clientname", client.Name()).Logger(),
		client:  client,
		cfg:     client.Snapshot(),
		release: releaseName,
	}
}

// clientConfig returns the config of the client in the snapshot of its handle.
func (c *call) clientConfig() domain.Client {
	return *c.cfg.Clients[c.client.Name()]
}
