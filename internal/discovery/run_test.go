// Copyright (c) 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package discovery_test

import (
	"net/http"
	"testing"
	"testing/synctest"
	"time"

	"github.com/nuxencs/seasonpackarr/internal/discovery"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIndexers_CapabilityOfKind(t *testing.T) {
	for _, tt := range []struct {
		name string
		kind discovery.Kind
		want []int
	}{
		{"search", discovery.KindSearch, []int{2, 1}},
		{"rss", discovery.KindRSS, []int{3, 1}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			f := newSourceFixture(t)

			ids, failures := f.indexers(t, tt.kind)

			assert.Equal(t, tt.want, ids)
			assert.Empty(t, failures)
			assert.Equal(t, 1, f.requestCount())
		})
	}
}

func TestIndexers_Allowlist(t *testing.T) {
	const (
		noSearchable  = "no enabled searchable torrent indexers in Prowlarr"
		missingSearch = "selected indexer is missing, disabled, or not a searchable torrent indexer"
		missingRSS    = "selected indexer is missing, disabled, or not a RSS-capable torrent indexer"
		noRSSCapable  = "no enabled RSS-capable torrent indexers in Prowlarr"
	)
	for _, tt := range []struct {
		name         string
		kind         discovery.Kind
		ids          []int
		want         []int
		wantFailures []discovery.Failure
	}{
		{"selected", discovery.KindSearch, []int{1}, []int{1}, nil},
		{"selected keeps priority order", discovery.KindSearch, []int{1, 2}, []int{2, 1}, nil},
		{"missing", discovery.KindSearch, []int{99}, nil, []discovery.Failure{
			{IndexerID: 99, Reason: missingSearch},
			{Reason: noSearchable},
		}},
		{"partial", discovery.KindSearch, []int{1, 99}, []int{1}, []discovery.Failure{
			{IndexerID: 99, Reason: missingSearch},
		}},
		{"disabled", discovery.KindSearch, []int{4}, nil, []discovery.Failure{
			{IndexerID: 4, Reason: missingSearch},
			{Reason: noSearchable},
		}},
		{"not capable", discovery.KindRSS, []int{2}, nil, []discovery.Failure{
			{IndexerID: 2, Reason: missingRSS},
			{Reason: noRSSCapable},
		}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			f := newSourceFixture(t)
			f.settings.IndexerIDs = tt.ids

			ids, failures := f.indexers(t, tt.kind)

			assert.Equal(t, tt.want, ids)
			assert.Equal(t, tt.wantFailures, failures)
		})
	}
}

func TestIndexers_FailedReadPutsProwlarrInCooldown(t *testing.T) {
	for _, tt := range []struct {
		name        string
		first, next discovery.Kind
	}{
		{"search then rss", discovery.KindSearch, discovery.KindRSS},
		{"rss then search", discovery.KindRSS, discovery.KindSearch},
	} {
		t.Run(tt.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				f := newSourceFixture(t)
				f.prowlarr.respond = func(*http.Request) *http.Response { return cooldownResponse(http.StatusServiceUnavailable, "7200") }
				inCooldown := []discovery.Failure{{Reason: "Prowlarr cooldown is still active"}}

				ids, failures := f.indexers(t, tt.first)
				require.Empty(t, ids)
				require.Equal(t, []discovery.Failure{{Reason: "Prowlarr returned HTTP 503"}}, failures)
				f.prowlarr.respond = nil

				ids, failures = f.indexers(t, tt.next)
				assert.Empty(t, ids)
				assert.Equal(t, inCooldown, failures)
				assert.Equal(t, 1, f.requestCount(), "the cooldown must apply to the next run of each kind")

				f.restart(t)
				time.Sleep(time.Hour)
				ids, failures = f.indexers(t, tt.first)
				assert.Empty(t, ids)
				assert.Equal(t, inCooldown, failures)
				assert.Equal(t, 1, f.requestCount(), "the cooldown must hold across a restart")

				time.Sleep(time.Hour)
				ids, failures = f.indexers(t, tt.first)
				assert.NotEmpty(t, ids)
				assert.Empty(t, failures)
				assert.Equal(t, 2, f.requestCount(), "a run after the deadline must send the request again")
			})
		})
	}
}

func TestIndexers_ElapsedRetryAfterDoesNotBlockNextRun(t *testing.T) {
	f := newSourceFixture(t)
	f.prowlarr.respond = func(*http.Request) *http.Response { return cooldownResponse(http.StatusTooManyRequests, "0") }
	_, failures := f.indexers(t, discovery.KindSearch)
	require.Equal(t, []discovery.Failure{{Reason: "Prowlarr returned HTTP 429; indexer is rate limited"}}, failures)
	f.prowlarr.respond = nil

	ids, failures := f.indexers(t, discovery.KindSearch)

	assert.Equal(t, []int{2, 1}, ids)
	assert.Empty(t, failures)
	assert.Equal(t, 2, f.requestCount())
}

func TestIndexers_DatabaseFailure(t *testing.T) {
	f := newSourceFixture(t)
	run := f.begin(t, discovery.KindSearch)
	require.NoError(t, run.Err())
	require.NoError(t, f.store.Close())
	f.prowlarr.respond = func(*http.Request) *http.Response { return cooldownResponse(http.StatusServiceUnavailable, "7200") }

	ids := run.Indexers(t.Context())

	assert.Empty(t, ids)
	assert.ErrorIs(t, run.Err(), discovery.ErrDatabase)
	assert.Equal(t, []discovery.Failure{
		{Reason: "could not access database; check service logs"},
		{Reason: "Prowlarr returned HTTP 503"},
	}, run.Failures())
	event := f.log.Events(t).Require(t, "database operation failed")
	assert.Equal(t, "sql: database is closed", event["error"])
}

func TestIndexers_KeepsAPIKeySecret(t *testing.T) {
	const key = "prowlarr-key-sentinel-3c1f"
	f := newSourceFixture(t)
	f.settings.APIKey = key
	f.prowlarr.apiKey = key
	run := f.begin(t, discovery.KindSearch)
	require.NoError(t, f.store.Close())
	// The body echoes the key, as a proxy error page can.
	f.prowlarr.respond = func(r *http.Request) *http.Response {
		resp := cooldownResponse(http.StatusInternalServerError, "7200")
		resp.Body = response(0, "upstream failed for apikey="+r.Header.Get("X-Api-Key")).Body
		return resp
	}

	run.Indexers(t.Context())

	require.Len(t, run.Failures(), 2)
	for _, failure := range run.Failures() {
		assert.NotContains(t, failure.Reason, key)
	}
	f.log.Events(t).Require(t, "database operation failed")
	f.log.AssertNoSecrets(t, map[string]string{"Prowlarr API key": key})
}
