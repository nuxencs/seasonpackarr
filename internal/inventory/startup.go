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
// first announce does not log in or parse every torrent name. Beside the
// client reads, it scans the source folders of the first snapshot once. It
// returns when each client is read, has failed each attempt, or ctx ends, and
// when the scan is done. A failure only logs. Each attempt reads a new
// snapshot from cfg, so a reload applies to the next attempt.
func (s *Store) ReadAtStartup(ctx context.Context, cfg config.Provider) {
	endpoints := make(map[torrentclient.Endpoint][]string)
	snapshot := cfg.Snapshot()
	for _, name := range slices.Sorted(maps.Keys(snapshot.Clients)) {
		endpoint := torrentclient.EndpointOf(*snapshot.Clients[name])
		endpoints[endpoint] = append(endpoints[endpoint], name)
	}

	var wg sync.WaitGroup
	// The client lines wait for the scan, so they can count the source folder files.
	scanned := make(chan struct{})
	wg.Go(func() {
		defer close(scanned)
		s.scanSourceFolders(ctx, snapshot)
	})
	// One goroutine for each endpoint, so a client that times out does not delay the others.
	for _, names := range endpoints {
		wg.Go(func() {
			// One client at a time, so the first parses the names and the others reuse them.
			for _, name := range names {
				s.readClientAtStartup(ctx, cfg, name, scanned)
			}
		})
	}
	wg.Wait()
}

func (s *Store) readClientAtStartup(ctx context.Context, cfg config.Provider, name string, scanned <-chan struct{}) {
	for attempt := 1; ; attempt++ {
		snapshot := cfg.Snapshot()
		clientConfig, ok := snapshot.Clients[name]
		if !ok {
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
			s.logInventoryReady(ctx, name, clientConfig.SourceFolders, inv, time.Since(started), scanned)
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

// logInventoryReady logs the torrent count, and for a client with source folders the number of their episode files.
func (s *Store) logInventoryReady(ctx context.Context, name string, folders []string, inv *Inventory, read time.Duration, scanned <-chan struct{}) {
	if len(folders) == 0 {
		s.log.Info().Str("clientname", name).Int("torrents", inv.torrents).Int64("duration_ms", read.Milliseconds()).Msg("client inventory ready")
		return
	}
	select {
	case <-ctx.Done():
		return
	case <-scanned:
	}
	s.log.Info().
		Str("clientname", name).
		Int("torrents", inv.torrents).
		Int("source_folder_files", s.sourceFolderFiles(folders)).
		Int64("duration_ms", read.Milliseconds()).
		Msg("client inventory ready")
}
