// Copyright (c) 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

// Package discovery is the source of Prowlarr discovery. It owns each call to
// Prowlarr and to the discovery tables of the store, and the rules that
// connect them. A run has its own types, so a caller of a run needs no type
// of the Prowlarr or the store package.
package discovery

import (
	"context"
	"errors"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/nuxencs/seasonpackarr/internal/domain"
	"github.com/nuxencs/seasonpackarr/internal/errtrace"
	"github.com/nuxencs/seasonpackarr/internal/logger"
	"github.com/nuxencs/seasonpackarr/internal/prowlarr"
	"github.com/nuxencs/seasonpackarr/internal/state"
)

// The error classes of Begin and of Listing.Torrent. An invalid interval or
// connection matches ErrInvalidSettings, and a failed download matches
// ErrDownload. Both keep the text of their cause.
var (
	ErrRunActive       = errors.New("a Prowlarr discovery run is already active")
	ErrDatabase        = errors.New("could not access database; check service logs")
	ErrInvalidSettings = errors.New("invalid Prowlarr discovery settings")
	ErrDownload        = errors.New("torrent download failed")
)

// classError puts its cause in an error class. Its text is the text of the
// cause, so the report of the run does not change.
type classError struct{ class, err error }

func (e classError) Error() string        { return e.err.Error() }
func (e classError) Unwrap() error        { return e.err }
func (e classError) Is(target error) bool { return target == e.class }

// Kind is the kind of a discovery run. It selects the capability that an
// indexer must have.
type Kind int

const (
	KindSearch Kind = iota
	KindRSS
)

// capability names the capability of the kind in a failure text.
func (k Kind) capability() string {
	if k == KindRSS {
		return "RSS-capable"
	}
	return "searchable"
}

func (k Kind) supportedBy(indexer prowlarr.Indexer) bool {
	if k == KindRSS {
		return indexer.SupportsRSS
	}
	return indexer.SupportsSearch
}

// queryGap is the minimum time from the answer of one query to an indexer
// to the start of the next query to that indexer. A query is one search page
// or one feed page. Torrent downloads do not wait for it.
const queryGap = time.Minute

// Source opens one discovery run at a time. It keeps the Prowlarr connection
// and the last query time of each indexer across runs, so the request spacing
// and the query gap hold from one run to the next.
type Source struct {
	store     *state.Store
	log       logger.Logger
	transport http.RoundTripper
	running   atomic.Bool

	// The active run owns these fields. The overlap guard serializes the runs.
	client      *prowlarr.Client
	prowlarrURL string
	prowlarrKey string
	// lastQuery holds the answer time of the last query, by indexer id. It
	// survives a connection change, because an id can still name the same tracker.
	lastQuery map[int]time.Time
}

// New makes a source. A nil transport selects http.DefaultTransport.
func New(store *state.Store, log logger.Logger, transport http.RoundTripper) *Source {
	return &Source{store: store, log: log, transport: transport, lastQuery: make(map[int]time.Time)}
}

// query waits for the query gap of the indexer, then calls send, which sends
// one query. The client then also waits for the request spacing. A canceled
// wait returns the context error and sends nothing. The gap starts when send
// returns, also after a failure, because a failed query can have reached the
// indexer.
func (s *Source) query(ctx context.Context, indexerID int, send func() ([]prowlarr.Release, int, error)) ([]prowlarr.Release, int, error) {
	if wait := time.Until(s.lastQuery[indexerID].Add(queryGap)); wait > 0 {
		timer := time.NewTimer(wait)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return nil, 0, ctx.Err()
		case <-timer.C:
		}
	}
	releases, limit, err := send()
	s.lastQuery[indexerID] = time.Now()
	return releases, limit, err
}

// Begin opens a discovery run with the search settings of one config
// snapshot. Its errors match ErrRunActive, ErrDatabase, or
// ErrInvalidSettings, except a canceled context, which returns as is. After
// an error, no run is open. After a nil error, the caller must call End.
func (s *Source) Begin(ctx context.Context, settings domain.Search, kind Kind) (*Run, error) {
	if !s.running.CompareAndSwap(false, true) {
		return nil, ErrRunActive
	}
	run, err := s.begin(ctx, settings, kind)
	if err != nil {
		s.running.Store(false)
		return nil, err
	}
	return run, nil
}

// End releases the overlap guard. The run must not be used after End. A
// second call does nothing, so it cannot release the guard of a later run.
func (r *Run) End() {
	if r.ended {
		return
	}
	r.ended = true
	r.source.running.Store(false)
}

func (s *Source) begin(ctx context.Context, settings domain.Search, kind Kind) (*Run, error) {
	spacing, err := time.ParseDuration(settings.RequestInterval)
	if err != nil || spacing < 0 {
		return nil, classError{ErrInvalidSettings, errors.New("invalid search.requestInterval")}
	}
	if s.client == nil || s.prowlarrURL != settings.ProwlarrURL || s.prowlarrKey != settings.APIKey {
		client, err := prowlarr.New(settings.ProwlarrURL, settings.APIKey, spacing, s.transport)
		if err != nil {
			return nil, classError{ErrInvalidSettings, err}
		}
		s.client = client
		s.prowlarrURL = settings.ProwlarrURL
		s.prowlarrKey = settings.APIKey
	}
	s.client.SetRequestInterval(spacing)
	if err := s.store.UseConnection(ctx, settings.ProwlarrURL, settings.APIKey); err != nil {
		return nil, s.stateError(err)
	}
	cooldowns, err := s.store.Cooldowns(ctx, time.Now())
	if err != nil {
		return nil, s.stateError(err)
	}
	return &Run{
		source:     s,
		kind:       kind,
		indexerIDs: settings.IndexerIDs,
		cooldowns:  cooldowns,
		out:        make(map[int]bool),
	}, nil
}

// stateError logs a database failure with its stack and returns ErrDatabase.
// A canceled context returns as is.
func (s *Source) stateError(err error) error {
	if canceled(err) {
		return err
	}
	s.log.Error().Err(errtrace.WithStack(err)).Msg("database operation failed")
	return ErrDatabase
}

// canceled reports whether err is the error of a canceled context.
func canceled(err error) bool {
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}
