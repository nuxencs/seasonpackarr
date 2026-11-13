// Copyright (c) 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package prowlarr

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/require"
)

func TestClient_RSSWithoutSearchCapability(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "test-key", r.Header.Get("X-Api-Key"))
		if r.URL.Path == "/base/api/v1/indexer" {
			fmt.Fprint(w, `[{"id":1,"enable":true,"protocol":"torrent","supportsRss":true,"capabilities":{"limitsMax":20,"categories":[{"id":5000}]}}]`)
			return
		}
		require.Equal(t, "/base/1/api", r.URL.Path)
		require.Equal(t, "search", r.URL.Query().Get("t"))
		require.Equal(t, "5000", r.URL.Query().Get("cat"))
		require.Equal(t, "20", r.URL.Query().Get("limit"))
		require.Equal(t, "40", r.URL.Query().Get("offset"))
		for _, key := range []string{"q", "season", "year", "apikey"} {
			require.False(t, r.URL.Query().Has(key))
		}
		fmt.Fprint(w, `<rss><channel><item><title>Example.S01</title><guid>one</guid></item></channel></rss>`)
	}))
	defer server.Close()
	client, err := New(server.URL+"/base", "test-key", 0)
	require.NoError(t, err)
	indexers, err := client.Indexers(t.Context())
	require.NoError(t, err)
	require.Len(t, indexers, 1)
	require.False(t, indexers[0].SupportsSearch)
	results, limit, err := client.RSSPage(t.Context(), indexers[0], 40)
	require.NoError(t, err)
	require.Equal(t, 20, limit)
	require.Equal(t, "one", results[0].GUID)
	_, _, err = client.RSSPage(t.Context(), Indexer{ID: 2}, 0)
	require.ErrorContains(t, err, "does not support RSS")
}

type rssRoundTripper func(*http.Request) (*http.Response, error)

func (f rssRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func TestClient_SpacingSurvivesIntervalReload(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		client, err := New("http://prowlarr.invalid", "key", 10*time.Second)
		require.NoError(t, err)
		var times []time.Time
		client.http.Transport = rssRoundTripper(func(*http.Request) (*http.Response, error) {
			times = append(times, time.Now())
			return &http.Response{StatusCode: 204, Body: http.NoBody, Header: make(http.Header)}, nil
		})
		client.Indexers(t.Context()) // An HTTP failure still consumes the request slot.
		client.SetRequestInterval(20 * time.Second)
		client.RSSPage(t.Context(), Indexer{ID: 1, SupportsRSS: true}, 0)
		client.SetRequestInterval(10 * time.Second)
		client.Download(t.Context(), 1, Result{Link: "/1/download"})
		require.Len(t, times, 3)
		require.Equal(t, 20*time.Second, times[1].Sub(times[0]))
		require.Equal(t, 10*time.Second, times[2].Sub(times[1]))
	})
}
