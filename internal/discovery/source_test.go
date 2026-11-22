// Copyright (c) 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package discovery_test

import (
	"net/http"
	"testing"
	"testing/synctest"
	"time"

	"github.com/nuxencs/seasonpackarr/internal/discovery"
	"github.com/nuxencs/seasonpackarr/internal/domain"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBegin_OverlapGuard(t *testing.T) {
	f := newSourceFixture(t)
	run, err := f.source.Begin(t.Context(), f.settings, discovery.KindSearch)
	require.NoError(t, err)

	_, err = f.source.Begin(t.Context(), f.settings, discovery.KindRSS)
	require.ErrorIs(t, err, discovery.ErrRunActive)
	assert.EqualError(t, err, "a Prowlarr discovery run is already active")

	run.End()
	next, err := f.source.Begin(t.Context(), f.settings, discovery.KindRSS)
	require.NoError(t, err)
	defer next.End()

	run.End()
	_, err = f.source.Begin(t.Context(), f.settings, discovery.KindSearch)
	assert.ErrorIs(t, err, discovery.ErrRunActive, "a second End of an old run must not release the guard of the next run")
}

func TestBegin_ErrorClasses(t *testing.T) {
	for _, tt := range []struct {
		name      string
		mutate    func(*domain.Search)
		closeDB   bool
		wantClass error
		wantText  string
	}{
		{"invalid interval", func(s *domain.Search) { s.RequestInterval = "soon" }, false, discovery.ErrInvalidSettings, "invalid search.requestInterval"},
		{"negative interval", func(s *domain.Search) { s.RequestInterval = "-1s" }, false, discovery.ErrInvalidSettings, "invalid search.requestInterval"},
		{"invalid url", func(s *domain.Search) { s.ProwlarrURL = "ftp://prowlarr.invalid" }, false, discovery.ErrInvalidSettings, "Prowlarr URL must be an HTTP(S) base URL without credentials, query, or fragment"},
		{"missing api key", func(s *domain.Search) { s.APIKey = "" }, false, discovery.ErrInvalidSettings, "Prowlarr API key is required"},
		{"closed store", func(*domain.Search) {}, true, discovery.ErrDatabase, "could not access database; check service logs"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			f := newSourceFixture(t)
			tt.mutate(&f.settings)
			if tt.closeDB {
				require.NoError(t, f.store.Close())
			}

			_, err := f.source.Begin(t.Context(), f.settings, discovery.KindSearch)

			require.ErrorIs(t, err, tt.wantClass)
			assert.EqualError(t, err, tt.wantText)
			for _, other := range []error{discovery.ErrRunActive, discovery.ErrDatabase, discovery.ErrInvalidSettings} {
				if other != tt.wantClass {
					assert.NotErrorIs(t, err, other)
				}
			}
			if tt.closeDB {
				f.log.Events(t).Require(t, "database operation failed")
			}
			_, err = f.source.Begin(t.Context(), f.settings, discovery.KindSearch)
			assert.NotErrorIs(t, err, discovery.ErrRunActive, "a failed Begin must release the guard")
			assert.Zero(t, f.requestCount())
		})
	}
}

func TestBegin_ConnectionChangeResetsCooldowns(t *testing.T) {
	for name, change := range map[string]func(*sourceFixture){
		"url":     func(f *sourceFixture) { f.settings.ProwlarrURL = "http://other-prowlarr.invalid" },
		"api key": func(f *sourceFixture) { f.settings.APIKey = "other-key"; f.prowlarr.apiKey = "other-key" },
	} {
		t.Run(name, func(t *testing.T) {
			f := newSourceFixture(t)
			f.prowlarr.respond = func(*http.Request) *http.Response { return cooldownResponse(http.StatusServiceUnavailable, "7200") }
			f.indexers(t, discovery.KindSearch)
			f.prowlarr.respond = nil
			_, failures := f.indexers(t, discovery.KindSearch)
			require.Equal(t, []discovery.Failure{{Reason: "Prowlarr cooldown is still active"}}, failures)

			change(f)
			ids, failures := f.indexers(t, discovery.KindSearch)

			assert.Equal(t, []int{2, 1}, ids)
			assert.Empty(t, failures)
			assert.Equal(t, 2, f.requestCount())
		})
	}
}

func TestBegin_KeepsRequestSpacingAcrossRuns(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		f := newSourceFixture(t)
		f.settings.RequestInterval = "10s"

		f.indexers(t, discovery.KindSearch)
		f.indexers(t, discovery.KindRSS)

		require.Equal(t, 2, f.requestCount())
		assert.Equal(t, 10*time.Second, f.prowlarr.requests[1].At.Sub(f.prowlarr.requests[0].At))
	})
}
