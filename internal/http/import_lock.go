// Copyright (c) 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package http

import (
	"context"
	"sync"

	"github.com/nuxencs/seasonpackarr/internal/domain"
	"github.com/nuxencs/seasonpackarr/internal/torrentclient"
)

type importLock struct {
	token chan struct{}
	users int
}

var importLocks = struct {
	sync.Mutex
	values map[torrentclient.Endpoint]*importLock
}{values: make(map[torrentclient.Endpoint]*importLock)}

// lockImport serializes imports to a client endpoint, including configurations
// that share that endpoint. Waiting respects request or worker cancellation.
func lockImport(ctx context.Context, client domain.Client) (func(), error) {
	key := torrentclient.EndpointOf(client)
	importLocks.Lock()
	lock := importLocks.values[key]
	if lock == nil {
		lock = &importLock{token: make(chan struct{}, 1)}
		importLocks.values[key] = lock
	}
	lock.users++
	importLocks.Unlock()
	release := func() {
		importLocks.Lock()
		defer importLocks.Unlock()
		lock.users--
		if lock.users == 0 {
			delete(importLocks.values, key)
		}
	}
	select {
	case lock.token <- struct{}{}:
		if err := ctx.Err(); err != nil {
			<-lock.token
			release()
			return nil, err
		}
		return func() { <-lock.token; release() }, nil
	case <-ctx.Done():
		release()
		return nil, ctx.Err()
	}
}

// A client may add a torrent before it reports a verification or resume
// failure, so the import runs this before it releases the endpoint import lock.
func (p *processor) invalidateClientImports(client domain.Client) {
	endpoint := torrentclient.EndpointOf(client)
	p.inv.DiscardEndpoint(endpoint)
	planMap.Range(func(key importPlanCacheKey, cached cachedImportPlan) bool {
		if torrentclient.EndpointOf(cached.clientConfig) == endpoint {
			planMap.Delete(key)
		}
		return true
	})
}
