// Copyright (c) 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package http

import (
	"context"

	"github.com/nuxencs/seasonpackarr/internal/config"
	"github.com/nuxencs/seasonpackarr/internal/inventory"
	"github.com/nuxencs/seasonpackarr/internal/logger"
)

// watchConfigReloads runs the inventory clean-up after each config reload
// until ctx ends. A nil reloads channel only waits for ctx.
func watchConfigReloads(ctx context.Context, log logger.Logger, cfg config.Provider, inv *inventory.Store, reloads <-chan struct{}) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-reloads:
			dropped, err := inv.CleanUp(cfg.Snapshot())
			if len(dropped) > 0 {
				log.Debug().Strs("clients", dropped).Msg("dropped caches of removed or changed clients")
			}
			if err != nil {
				log.Warn().Err(err).Msg("could not close a removed torrent client")
			}
		}
	}
}
