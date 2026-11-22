// Copyright (c) 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

// The external test package limits the source tests to Begin and the run methods.
package discovery_test

import (
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nuxencs/seasonpackarr/internal/discovery"
	"github.com/nuxencs/seasonpackarr/internal/domain"
	"github.com/nuxencs/seasonpackarr/internal/logger/loggertest"
	"github.com/nuxencs/seasonpackarr/internal/state"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"
)

// defaultIndexers holds, in priority order for a search: 2 (search only),
// 1 (search and RSS); for RSS: 3 (RSS only), 1. Indexer 4 is disabled and
// indexer 5 is a Usenet indexer, so Prowlarr gives neither to a run.
const defaultIndexers = `[
	{"id":1,"priority":2,"enable":true,"protocol":"torrent","supportsSearch":true,"supportsRss":true},
	{"id":2,"priority":1,"enable":true,"protocol":"torrent","supportsSearch":true},
	{"id":3,"priority":1,"enable":true,"protocol":"torrent","supportsRss":true},
	{"id":4,"priority":1,"enable":false,"protocol":"torrent","supportsSearch":true,"supportsRss":true},
	{"id":5,"priority":1,"enable":true,"protocol":"usenet","supportsSearch":true,"supportsRss":true}
]`

// capturedRequest is one request that the fake Prowlarr received.
type capturedRequest struct {
	Path string
	At   time.Time
}

// fakeProwlarr answers the requests of the source in process.
type fakeProwlarr struct {
	apiKey   string
	indexers string
	// respond, when set, answers a request in place of the default answer.
	respond  func(*http.Request) *http.Response
	requests []capturedRequest
}

func (f *fakeProwlarr) RoundTrip(r *http.Request) (*http.Response, error) {
	f.requests = append(f.requests, capturedRequest{Path: r.URL.Path, At: time.Now()})
	if r.Header.Get("X-Api-Key") != f.apiKey {
		return response(http.StatusUnauthorized, ""), nil
	}
	if f.respond != nil {
		if resp := f.respond(r); resp != nil {
			return resp, nil
		}
	}
	if r.URL.Path == "/api/v1/indexer" {
		return response(http.StatusOK, f.indexers), nil
	}
	return response(http.StatusNotFound, ""), nil
}

func response(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}
}

// cooldownResponse is a temporary failure with a Retry-After deadline in seconds.
func cooldownResponse(status int, retryAfter string) *http.Response {
	resp := response(status, "")
	resp.Header.Set("Retry-After", retryAfter)
	return resp
}

// sourceFixture is a source on a real SQLite file and a fake Prowlarr. It
// needs no torrent client, no pack, and no config provider.
type sourceFixture struct {
	path     string
	store    *state.Store
	source   *discovery.Source
	prowlarr *fakeProwlarr
	log      *loggertest.Logger
	settings domain.Search
}

func newSourceFixture(t *testing.T) *sourceFixture {
	t.Helper()
	f := &sourceFixture{
		path:     filepath.Join(t.TempDir(), "seasonpackarr.db"),
		prowlarr: &fakeProwlarr{apiKey: "prowlarr-test-key", indexers: defaultIndexers},
		log:      loggertest.New(),
		settings: domain.Search{ProwlarrURL: "http://prowlarr.invalid", APIKey: "prowlarr-test-key", RequestInterval: "0s"},
	}
	f.open(t)
	return f
}

func (f *sourceFixture) open(t *testing.T) {
	t.Helper()
	store, err := state.Open(t.Context(), f.path, zerolog.Nop())
	require.NoError(t, err)
	t.Cleanup(func() { store.Close() })
	f.store = store
	f.source = discovery.New(store, f.log, f.prowlarr)
}

// restart closes the store, opens it again, and makes a new source, so only
// what SQLite keeps survives.
func (f *sourceFixture) restart(t *testing.T) {
	t.Helper()
	require.NoError(t, f.store.Close())
	f.open(t)
}

// begin opens a run of kind and ends it at the end of the test.
func (f *sourceFixture) begin(t *testing.T, kind discovery.Kind) *discovery.Run {
	t.Helper()
	run, err := f.source.Begin(t.Context(), f.settings, kind)
	require.NoError(t, err)
	t.Cleanup(run.End)
	return run
}

// indexers runs one Indexers call in a run of kind, ends the run, and returns
// the usable indexer ids and the failures.
func (f *sourceFixture) indexers(t *testing.T, kind discovery.Kind) ([]int, []discovery.Failure) {
	t.Helper()
	run, err := f.source.Begin(t.Context(), f.settings, kind)
	require.NoError(t, err)
	defer run.End()
	var ids []int
	for _, indexer := range run.Indexers(t.Context()) {
		ids = append(ids, indexer.ID)
	}
	return ids, run.Failures()
}

// requestCount is the number of requests that the fake Prowlarr received.
func (f *sourceFixture) requestCount() int { return len(f.prowlarr.requests) }
