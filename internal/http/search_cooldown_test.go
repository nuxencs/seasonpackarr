// Copyright (c) 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package http

import (
	stdhttp "net/http"
	"testing"
	"testing/synctest"

	"github.com/stretchr/testify/require"
)

func TestSearch_TransientFailureDoesNotBlockOtherIndexers(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		f := newSearchFixture(t, fixtureOptions{packEpisodes: 1, clientEpisodes: 1, threshold: 0.75})
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
	})
}
