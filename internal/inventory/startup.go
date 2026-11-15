// Copyright (c) 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package inventory

import (
	"context"
	"maps"
	"slices"
	"sync"
	"time"

	"github.com/nuxencs/seasonpackarr/internal/config"
	"github.com/nuxencs/seasonpackarr/internal/torrentclient"
)

const (
	// startupAttempts covers clients that start after seasonpackarr, for example after a host restart.
	startupAttempts   = 4
	startupRetryDelay = 10 * time.Second
)

// ReadAtStartup reads the inventory of each configured client once, so the
// first announce does not log in or parse every torrent name. It returns when
// each client is read, has failed each attempt, or ctx ends. A failure only
// logs. Each attempt reads a new snapshot from cfg, so a reload applies to the
// next attempt.
func (s *Store) ReadAtStartup(ctx context.Context, cfg config.Provider) {
	endpoints := make(map[torrentclient.Endpoint][]string)
	snapshot := cfg.Snapshot()
	for _, name := range slices.Sorted(maps.Keys(snapshot.Clients)) {
		endpoint := torrentclient.EndpointOf(*snapshot.Clients[name])
		endpoints[endpoint] = append(endpoints[endpoint], name)
	}

	// One goroutine for each endpoint, so a client that times out does not delay the others.
	var wg sync.WaitGroup
	for _, names := range endpoints {
		wg.Go(func() {
			// One client at a time, so the first parses the names and the others reuse them.
			for _, name := range names {
				s.readClientAtStartup(ctx, cfg, name)
			}
		})
	}
	wg.Wait()
}

func (s *Store) readClientAtStartup(ctx context.Context, cfg config.Provider, name string) {
	for attempt := 1; ; attempt++ {
		snapshot := cfg.Snapshot()
		if _, ok := snapshot.Clients[name]; !ok {
			return
		}

		started := time.Now()
		var inv *Inventory
		client, err := s.Client(ctx, name, snapshot)
		if err == nil {
			inv, err = client.Inventory(ctx)
		}
		if ctx.Err() != nil {
			return
		}
		if err == nil {
			s.log.Info().
				Str("clientname", name).
				Int("titles", len(inv.titles)).
				Int64("duration_ms", time.Since(started).Milliseconds()).
				Msg("client inventory ready")
			return
		}
		if attempt == startupAttempts {
			s.log.Warn().Err(err).Str("clientname", name).Msg("could not read client inventory at startup")
			return
		}
		s.log.Debug().Err(err).Str("clientname", name).Int("attempt", attempt).Msg("client inventory not ready at startup")

		select {
		case <-ctx.Done():
			return
		case <-time.After(startupRetryDelay):
		}
	}
}
