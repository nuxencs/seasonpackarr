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
				inv, read := s.readClientAtStartup(ctx, cfg, name)
				if inv == nil {
					continue
				}
				// The count uses the folders of the scan, not those of a later snapshot.
				folders := snapshot.Clients[name].SourceFolders
				if len(folders) == 0 {
					s.logInventoryReady(name, inv, read, nil)
					continue
				}
				// The line waits for the scan in its own goroutine, so the next client of the endpoint does not wait.
				wg.Go(func() {
					select {
					case <-ctx.Done():
					case <-scanned:
						s.logInventoryReady(name, inv, read, folders)
					}
				})
			}
		})
	}
	wg.Wait()
}

// readClientAtStartup returns the inventory and the duration of the read that worked, or nil.
func (s *Store) readClientAtStartup(ctx context.Context, cfg config.Provider, name string) (*Inventory, time.Duration) {
	for attempt := 1; ; attempt++ {
		snapshot := cfg.Snapshot()
		if _, ok := snapshot.Clients[name]; !ok {
			return nil, 0
		}

		started := time.Now()
		var inv *Inventory
		client, err := s.Client(ctx, name, snapshot)
		if err == nil {
			inv, err = client.Inventory(ctx)
		}
		if ctx.Err() != nil {
			return nil, 0
		}
		if err == nil {
			return inv, time.Since(started)
		}
		if attempt == startupAttempts {
			s.log.Warn().Err(err).Str("clientname", name).Msg("could not read client inventory at startup")
			return nil, 0
		}
		s.log.Debug().Err(err).Str("clientname", name).Int("attempt", attempt).Msg("client inventory not ready at startup")

		select {
		case <-ctx.Done():
			return nil, 0
		case <-time.After(startupRetryDelay):
		}
	}
}

// logInventoryReady logs the torrent count, and for a client with source folders the number of their episode files.
func (s *Store) logInventoryReady(name string, inv *Inventory, read time.Duration, folders []string) {
	event := s.log.Info().Str("clientname", name).Int("torrents", inv.torrents)
	if len(folders) > 0 {
		event = event.Int("source_folder_files", s.sourceFolderFiles(folders))
	}
	event.Int64("duration_ms", read.Milliseconds()).Msg("client inventory ready")
}
