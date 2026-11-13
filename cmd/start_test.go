// Copyright (c) 2023 - 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package cmd

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type fakeManagedServer struct {
	started      chan struct{}
	stopped      chan struct{}
	stopOnce     sync.Once
	shutdownCall chan context.Context
}

func newFakeManagedServer() *fakeManagedServer {
	return &fakeManagedServer{
		started:      make(chan struct{}),
		stopped:      make(chan struct{}),
		shutdownCall: make(chan context.Context, 1),
	}
}

func (f *fakeManagedServer) Open(context.Context) error {
	close(f.started)
	<-f.stopped
	return nil
}

func (f *fakeManagedServer) Shutdown(ctx context.Context) error {
	f.shutdownCall <- ctx
	f.stopOnce.Do(func() { close(f.stopped) })
	return nil
}

func TestRunServer_ShutsDownWithBoundedContext(t *testing.T) {
	server := newFakeManagedServer()
	ctx, cancel := context.WithCancel(t.Context())
	result := make(chan error, 1)
	go func() {
		result <- runServer(ctx, server)
	}()

	<-server.started
	cancel()

	require.NoError(t, <-result)
	shutdownCtx := <-server.shutdownCall
	deadline, ok := shutdownCtx.Deadline()
	require.True(t, ok)
	require.LessOrEqual(t, time.Until(deadline), serverShutdownTimeout)
}
