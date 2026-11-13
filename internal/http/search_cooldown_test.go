// Copyright (c) 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package http

import (
	"fmt"
	stdhttp "net/http"
	"testing"
	"time"

	"github.com/nuxencs/seasonpackarr/internal/domain"
	"github.com/nuxencs/seasonpackarr/internal/logger"
	"github.com/stretchr/testify/require"
)

func TestSearch_CooldownsAcrossRuns(t *testing.T) {
	for _, endpoint := range []string{"/api/v1/indexer", "/1/api", "/1/download"} {
		for _, status := range []int{429, 503} {
			t.Run(fmt.Sprintf("%s/%d", endpoint, status), func(t *testing.T) {
				f := newSearchFixture(t, 1, 1, 0.75)
				cfg := f.config.Snapshot()
				cfg.Search.IndexerIDs = []int{1}
				f.config.Store(cfg)
				log := logger.New(&domain.Config{LogLevel: "ERROR", Version: "test"})
				server := NewServer(log, f.config, noopNotificationSender{})
				f.handler = server.Handler()
				// Multiple candidates must not cause more requests after a failure.
				f.titles = append(f.titles, f.releaseName)
				calls := 0
				f.respond = func(w stdhttp.ResponseWriter, r *stdhttp.Request) bool {
					if r.URL.Path != endpoint {
						return false
					}
					calls++
					w.Header().Set("Retry-After", "7200")
					w.WriteHeader(status)
					return true
				}
				f.runExact(t, true)
				require.Equal(t, 1, calls)
				report := f.runExact(t, true)
				require.Equal(t, 1, calls, "cooldown must prevent requests across runs")
				require.Len(t, report.Failures, 1)
				require.Contains(t, report.Failures[0].Reason, "cooldown")
				id := 1
				if endpoint == "/api/v1/indexer" {
					id = 0
				}
				require.WithinDuration(t, time.Now().Add(2*time.Hour), server.search.cooldowns[id], 2*time.Second)
				server.search.cooldowns[id] = time.Now().Add(-time.Second)
				f.runExact(t, true)
				require.Equal(t, 2, calls, "requests resume after cooldown expiry")
				f.handler = NewServer(log, f.config, noopNotificationSender{}).Handler()
				f.runExact(t, true)
				require.Equal(t, 3, calls, "restart resets in-memory cooldowns")
			})
		}
	}
}

func TestSearch_TransientFailureDoesNotBlockOtherIndexers(t *testing.T) {
	f := newSearchFixture(t, 1, 1, 0.75)
	failedDownloads := 0
	f.respond = func(w stdhttp.ResponseWriter, r *stdhttp.Request) bool {
		if r.URL.Path != "/1/download" {
			return false
		}
		failedDownloads++
		w.WriteHeader(503)
		return true
	}
	first := f.runExact(t, true)
	require.Len(t, first.Outcomes, 2)
	require.Equal(t, "failed", first.Outcomes[0].Status)
	require.Equal(t, "would_import", first.Outcomes[1].Status)
	second := f.runExact(t, true)
	require.Len(t, second.Failures, 1)
	require.Equal(t, 1, second.Failures[0].IndexerID)
	require.Len(t, second.Outcomes, 1)
	require.Equal(t, "would_import", second.Outcomes[0].Status)
	require.Equal(t, 1, failedDownloads)
}

func TestSearch_ElapsedRetryAfterSkipsRestOfRun(t *testing.T) {
	for _, header := range []string{"0", time.Now().Add(-time.Hour).UTC().Format(stdhttp.TimeFormat)} {
		t.Run(header, func(t *testing.T) {
			f := newSearchFixture(t, 1, 1, 0.75)
			cfg := f.config.Snapshot()
			cfg.Search.IndexerIDs = []int{1}
			f.config.Store(cfg)
			f.titles = append(f.titles, f.releaseName)
			calls := 0
			f.respond = func(w stdhttp.ResponseWriter, r *stdhttp.Request) bool {
				if r.URL.Path != "/1/download" {
					return false
				}
				calls++
				w.Header().Set("Retry-After", header)
				w.WriteHeader(429)
				return true
			}
			f.runExact(t, true)
			require.Equal(t, 1, calls, "do not continue downloading candidates after a failure")
			f.runExact(t, true)
			require.Equal(t, 2, calls, "an elapsed server deadline must not block a later run")
		})
	}
}
