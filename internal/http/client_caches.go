// Copyright (c) 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package http

import (
	"context"
	"errors"
	"io"
	"slices"

	"github.com/nuxencs/seasonpackarr/internal/config"
	"github.com/nuxencs/seasonpackarr/internal/domain"
	"github.com/nuxencs/seasonpackarr/internal/errtrace"
	"github.com/nuxencs/seasonpackarr/internal/logger"
)

// watchConfigReloads prunes the client caches after each config reload until
// ctx ends. A nil reloads channel only waits for ctx.
func watchConfigReloads(ctx context.Context, log logger.Logger, cfg config.Provider, reloads <-chan struct{}) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-reloads:
			dropped, err := pruneClientCaches(cfg.Snapshot())
			if len(dropped) > 0 {
				log.Debug().Strs("clients", dropped).Msg("dropped caches of removed or changed clients")
			}
			if err != nil {
				log.Warn().Err(err).Msg("could not close a removed torrent client")
			}
		}
	}
}

// pruneClientCaches drops the connection and inventory of each client that cfg
// no longer has or configures differently, and the parsed names of endpoints
// and inventory scopes that no client uses. Without it, each removed client or changed host keeps a
// full inventory in memory until a restart. It returns the dropped client names.
func pruneClientCaches(cfg domain.Config) ([]string, error) {
	current := func(name string, client domain.Client) bool {
		configured, ok := cfg.Clients[name]
		return ok && clientConfigsEqual(*configured, client)
	}

	entryMap.Range(func(name string, cached *entryCache) bool {
		if !current(name, cached.clientConfig) {
			// Compare, so a newer inventory that a request stored meanwhile stays.
			// A missing key must also delete, because Compute otherwise stores the zero value.
			entryMap.Compute(name, func(latest *entryCache, loaded bool) (*entryCache, bool) {
				return latest, !loaded || latest == cached
			})
		}
		return true
	})

	var dropped []string
	var closers []io.Closer
	clientMap.Range(func(name string, cached cachedTorrentClient) bool {
		if current(name, cached.config) {
			return true
		}
		clientMap.Compute(name, func(latest cachedTorrentClient, loaded bool) (cachedTorrentClient, bool) {
			if !loaded || latest.client != cached.client {
				return latest, !loaded
			}
			dropped = append(dropped, name)
			if closer, ok := cached.client.(io.Closer); ok {
				closers = append(closers, closer)
			}
			return latest, true
		})
		return true
	})
	slices.Sort(dropped)

	// Count the clean-up before the deletes, so a scan that stores its names after this point sees it.
	parsedNamesGeneration.Add(1)
	scopes := make(map[parsedNamesKey]bool, len(cfg.Clients))
	for _, client := range cfg.Clients {
		scopes[parsedNamesKeyFor(*client)] = true
	}
	parsedReleaseMap.Range(func(key parsedNamesKey, _ parsedReleases) bool {
		if !scopes[key] {
			parsedReleaseMap.Delete(key)
		}
		return true
	})

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
