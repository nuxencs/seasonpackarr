// Copyright (c) 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package inventory

import (
	"errors"
	"io"
	"maps"
	"slices"

	"github.com/nuxencs/seasonpackarr/internal/domain"
	"github.com/nuxencs/seasonpackarr/internal/errtrace"
)

// CleanUp drops the connection and the inventory of each client that cfg no
// longer has or configures differently, the parsed names of each endpoint
// and inventory scope that no client of cfg uses, and the scan result of each
// source folder that no client of cfg names. Without it, each removed client
// or changed host keeps a full inventory in memory until a restart. Then it
// tells the scan schedule to scan the new folders of cfg. It returns the
// sorted names of the dropped connections and the close errors of their
// adapters.
func (s *Store) CleanUp(cfg domain.Config) ([]string, error) {
	current := func(name string, client domain.Client) bool {
		configured, ok := cfg.Clients[name]
		return ok && configured != nil && configured.Equal(client)
	}
	usedKeys := make(map[parsedNamesKey]bool, len(cfg.Clients))
	named := make(map[string]struct{})
	for _, client := range cfg.Clients {
		if client != nil {
			usedKeys[parsedNamesKeyFor(*client)] = true
			for _, folder := range client.SourceFolders {
				named[folder] = struct{}{}
			}
		}
	}

	var dropped []string
	var closers []io.Closer
	s.mu.Lock()
	// The checks and deletes share one lock, so no request can store data for cfg
	// between a check and its delete, and no read can miss the count of this clean-up.
	maps.DeleteFunc(s.inventories, func(name string, sl *slot) bool {
		return !current(name, sl.config)
	})
	for name, conn := range s.connections {
		if current(name, conn.config) {
			continue
		}
		delete(s.connections, name)
		dropped = append(dropped, name)
		if closer, ok := conn.adapter.(io.Closer); ok {
			closers = append(closers, closer)
		}
	}
	s.cleanUps++
	maps.DeleteFunc(s.parsed, func(key parsedNamesKey, _ parsedNames) bool {
		return !usedKeys[key]
	})
	s.namedFolders = named
	maps.DeleteFunc(s.folderScans, func(folder string, _ folderScan) bool {
		_, ok := named[folder]
		return !ok
	})
	s.mu.Unlock()
	slices.Sort(dropped)
	// One pending signal stands for any number of clean-ups, because the schedule reads the newest snapshot.
	select {
	case s.reloaded <- struct{}{}:
	default:
	}

	// A request can still hold a dropped adapter. See the Close contract of
	// torrentclient.TorrentClient.
	var closeErrs []error
	for _, closer := range closers {
		if err := closer.Close(); err != nil {
			closeErrs = append(closeErrs, errtrace.WithStack(err))
		}
	}
	return dropped, errors.Join(closeErrs...)
}
