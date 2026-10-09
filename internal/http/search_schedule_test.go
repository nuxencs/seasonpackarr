// Copyright (c) 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package http

import (
	"context"
	"testing"
	"time"

	"github.com/nuxencs/seasonpackarr/internal/domain"
	"github.com/nuxencs/seasonpackarr/internal/logger"

	"github.com/stretchr/testify/require"
)

func TestRSSSchedule_OptInAndReload(t *testing.T) {
	now := time.Now()
	var schedule rssSchedule
	require.False(t, schedule.due(now, "0s"))
	require.False(t, schedule.due(now, "1h"))
	require.False(t, schedule.due(now.Add(59*time.Minute), "1h"))
	require.True(t, schedule.due(now.Add(time.Hour), "1h"))
	require.False(t, schedule.due(now.Add(time.Hour), "2h"))
	require.False(t, schedule.due(now.Add(4*time.Hour), "0s"))
	require.False(t, schedule.due(now.Add(5*time.Hour), "1h"))
}

func TestRSSSchedule_CancellationStopsWorker(t *testing.T) {
	f := newSearchFixture(t, fixtureOptions{packEpisodes: 1, clientEpisodes: 1, threshold: 0.75})
	runner := &searchRunner{cfg: f.config}
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() { runner.schedule(ctx); close(done) }()
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		require.FailNow(t, "scheduler did not stop")
	}
	require.Empty(t, f.queries)
}

func TestRSSSchedule_PollsRSSAndCancels(t *testing.T) {
	f := newSearchFixture(t, fixtureOptions{packEpisodes: 1, clientEpisodes: 1, threshold: 1})
	cfg := f.config.Snapshot()
	cfg.Search.RSSInterval = "1ms" // Bypass config validation to exercise the worker.
	f.config.Store(cfg)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	f.beforeSearch = cancel
	runner := &searchRunner{cfg: f.config, log: logger.New(&domain.Config{LogLevel: "ERROR"}), state: f.search.state}
	done := make(chan struct{})
	go func() { runner.schedule(ctx); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		require.FailNow(t, "RSS scheduler did not stop after cancellation")
	}
	require.Equal(t, []string{""}, f.queries, "the only automatic request must be an RSS poll")
}
