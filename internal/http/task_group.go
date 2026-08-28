// Copyright (c) 2023 - 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package http

import (
	"context"
	"sync"
)

type taskGroup struct {
	ctx    context.Context
	cancel context.CancelFunc
	mu     sync.Mutex
	closed bool
	wg     sync.WaitGroup
}

func newTaskGroup() *taskGroup {
	ctx, cancel := context.WithCancel(context.Background())
	return &taskGroup{ctx: ctx, cancel: cancel}
}

func (g *taskGroup) Go(task func(context.Context)) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.closed {
		return
	}
	g.wg.Go(func() {
		task(g.ctx)
	})
}

func (g *taskGroup) Wait(ctx context.Context) error {
	g.mu.Lock()
	g.closed = true
	g.mu.Unlock()

	done := make(chan struct{})
	go func() {
		g.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		g.cancel()
		return nil
	case <-ctx.Done():
		g.cancel()
		return ctx.Err()
	}
}
