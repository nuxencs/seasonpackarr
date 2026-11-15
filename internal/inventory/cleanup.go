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
// longer has or configures differently, and the parsed names of each endpoint
// and inventory scope that no client of cfg uses. Without it, each removed
// client or changed host keeps a full inventory in memory until a restart. It
// returns the sorted names of the dropped connections and the close errors of
// their adapters.
func (s *Store) CleanUp(cfg domain.Config) ([]string, error) {
	current := func(name string, client domain.Client) bool {
		configured, ok := cfg.Clients[name]
		return ok && configured != nil && configured.Equal(client)
	}
	usedKeys := make(map[parsedNamesKey]bool, len(cfg.Clients))
	for _, client := range cfg.Clients {
		if client != nil {
			usedKeys[parsedNamesKeyFor(*client)] = true
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
	s.mu.Unlock()
	slices.Sort(dropped)

	// A request can still hold a dropped adapter. Close must leave it usable,
	// as the Deluge adapter does by connecting again on its next call.
	var closeErrs []error
	for _, closer := range closers {
		if err := closer.Close(); err != nil {
			closeErrs = append(closeErrs, errtrace.WithStack(err))
		}
	}
	return dropped, errors.Join(closeErrs...)
}
