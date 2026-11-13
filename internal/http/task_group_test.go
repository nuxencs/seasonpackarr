// Copyright (c) 2023 - 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package http

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestTaskGroup_CancelsTasksAtShutdownDeadline(t *testing.T) {
	tasks := newTaskGroup()
	taskCanceled := make(chan struct{})
	tasks.Go(func(ctx context.Context) {
		<-ctx.Done()
		close(taskCanceled)
	})

	shutdownCtx, cancel := context.WithCancel(t.Context())
	cancel()
	require.ErrorIs(t, tasks.Wait(shutdownCtx), context.Canceled)
	<-taskCanceled
}

func TestTaskGroup_RejectsTasksAfterShutdownStarts(t *testing.T) {
	tasks := newTaskGroup()
	require.NoError(t, tasks.Wait(t.Context()))

	ran := make(chan struct{}, 1)
	tasks.Go(func(context.Context) { ran <- struct{}{} })
	select {
	case <-ran:
		t.Fatal("task started after shutdown")
	case <-time.After(10 * time.Millisecond):
	}
}
