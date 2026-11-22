// Copyright (c) 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

// The external test package limits these tests to the exported surface.
package prowlarr_test

import (
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/nuxencs/seasonpackarr/internal/prowlarr"

	"github.com/stretchr/testify/require"
)

// fakeRoundTripper answers client requests in process.
type fakeRoundTripper func(*http.Request) (*http.Response, error)

func (f fakeRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestNew_TransportKeepsRedirectRule(t *testing.T) {
	var paths []string
	client, err := prowlarr.New("http://prowlarr.invalid", "key", 0, fakeRoundTripper(func(r *http.Request) (*http.Response, error) {
		paths = append(paths, r.URL.Path)
		if r.URL.Path == "/1/download" {
			return &http.Response{StatusCode: http.StatusFound, Body: http.NoBody, Header: http.Header{"Location": {"http://prowlarr.invalid/redirected"}}}, nil
		}
		body := `[{"id":1,"enable":true,"protocol":"torrent"}]`
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
	}))
	require.NoError(t, err)

	indexers, err := client.Indexers(t.Context())
	require.NoError(t, err)
	require.Len(t, indexers, 1)
	_, err = client.Download(t.Context(), 1, prowlarr.Result{Link: "/1/download"})

	require.ErrorContains(t, err, "HTTP 302")
	require.Equal(t, []string{"/api/v1/indexer", "/1/download"}, paths, "the client must not follow the redirect")
}

func TestNew_TransportKeepsTimeout(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		client, err := prowlarr.New("http://prowlarr.invalid", "key", 0, fakeRoundTripper(func(r *http.Request) (*http.Response, error) {
			<-r.Context().Done()
			return nil, r.Context().Err()
		}))
		require.NoError(t, err)
		start := time.Now()

		_, err = client.Indexers(t.Context())

		_, ok := errors.AsType[*prowlarr.CooldownError](err)
		require.True(t, ok, "%v", err)
		require.Equal(t, 2*time.Minute, time.Since(start))
	})
}
