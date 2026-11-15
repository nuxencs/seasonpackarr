// Copyright (c) 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package seasonpack

import (
	"context"
	"sync"

	"github.com/nuxencs/seasonpackarr/internal/torrentclient"
)

// endpointLocks holds one import lock for each client endpoint in use. The
// zero value holds no lock.
type endpointLocks struct {
	mu     sync.Mutex
	values map[torrentclient.Endpoint]*endpointLock
}

type endpointLock struct {
	token chan struct{}
	users int
}

// lock serializes imports to a client endpoint, including client configs that
// share that endpoint. The wait stops when ctx ends. The caller must call the
// returned function to release the lock.
func (l *endpointLocks) lock(ctx context.Context, endpoint torrentclient.Endpoint) (func(), error) {
	l.mu.Lock()
	if l.values == nil {
		l.values = make(map[torrentclient.Endpoint]*endpointLock)
	}
	lock := l.values[endpoint]
	if lock == nil {
		lock = &endpointLock{token: make(chan struct{}, 1)}
		l.values[endpoint] = lock
	}
	lock.users++
	l.mu.Unlock()
	release := func() {
		l.mu.Lock()
		defer l.mu.Unlock()
		lock.users--
		if lock.users == 0 {
			delete(l.values, endpoint)
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
