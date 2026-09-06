// Copyright (c) 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package prowlarr

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestClient_IndexersAndSeasonQueries(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "test-key", r.Header.Get("X-Api-Key"))
		require.Empty(t, r.URL.Query().Get("apikey"))
		switch r.URL.Path {
		case "/prowlarr/api/v1/indexer":
			fmt.Fprint(w, `[{"id":2,"priority":50,"enable":true,"protocol":"torrent","supportsSearch":true},{"id":1,"priority":10,"enable":true,"protocol":"torrent","supportsSearch":true,"capabilities":{"tvSearchParams":["q","season"],"limitsMax":25,"categories":[{"id":5000}]}},{"id":3,"enable":false,"protocol":"torrent","supportsSearch":true},{"id":4,"enable":true,"protocol":"usenet","supportsSearch":true}]`)
		case "/prowlarr/1/api":
			require.Equal(t, "tvsearch", r.URL.Query().Get("t"))
			require.Equal(t, "Example", r.URL.Query().Get("q"))
			require.Equal(t, "2", r.URL.Query().Get("season"))
			require.Equal(t, "5000", r.URL.Query().Get("cat"))
			require.Equal(t, "25", r.URL.Query().Get("limit"))
			require.Equal(t, "50", r.URL.Query().Get("offset"))
			fmt.Fprint(w, `<rss><channel><item><title>Example.S02</title><guid>one</guid></item></channel></rss>`)
		default:
			t.Errorf("unexpected request %s", r.URL.Path)
			w.WriteHeader(404)
		}
	}))
	defer server.Close()
	client, err := New(server.URL+"/prowlarr", "test-key", 0)
	require.NoError(t, err)
	indexers, err := client.Indexers(t.Context())
	require.NoError(t, err)
	require.Len(t, indexers, 2)
	require.Equal(t, 1, indexers[0].ID)
	results, limit, err := client.SearchPage(t.Context(), indexers[0], Query{Title: "Example", Year: 2024, Season: 2}, 50)
	require.NoError(t, err)
	require.Equal(t, 25, limit)
	require.Len(t, results, 1)
	require.Equal(t, "one", results[0].GUID)
}

func TestClient_TextFallbackAndInvalidFeeds(t *testing.T) {
	for _, test := range []struct {
		name, body string
		fail       bool
	}{
		{"empty", `<rss><channel/></rss>`, false},
		{"torznab error", `<error code="100" description="secret-key"/>`, true},
		{"login page", `<html><body>login</body></html>`, true},
		{"malformed", `<rss>`, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				require.Empty(t, r.URL.Query().Get("cat"), "uncategorized trackers must not be excluded")
				require.Equal(t, "search", r.URL.Query().Get("t"))
				require.Equal(t, "Example S01", r.URL.Query().Get("q"))
				fmt.Fprint(w, test.body)
			}))
			defer server.Close()
			client, err := New(server.URL, "secret-key", 0)
			require.NoError(t, err)
			indexer := Indexer{ID: 1}
			indexer.Capabilities.SearchParams = []string{"q"}
			_, _, err = client.SearchPage(t.Context(), indexer, Query{Title: "Example", Year: 2024, Season: 1}, 0)
			if test.fail {
				require.Error(t, err)
				require.NotContains(t, err.Error(), "secret-key")
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestClient_DownloadProxyBoundary(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		require.Equal(t, "key", r.Header.Get("X-Api-Key"))
		require.Empty(t, r.URL.Query().Get("apikey"))
		if r.URL.Query().Get("link") == "redirect" {
			http.Redirect(w, r, "https://tracker.invalid/?passkey=secret", http.StatusFound)
			return
		}
		fmt.Fprint(w, "torrent bytes")
	}))
	defer server.Close()
	client, err := New(server.URL+"/base", "key", 0)
	require.NoError(t, err)
	data, err := client.Download(t.Context(), 1, Result{Link: server.URL + "/base/1/download?apikey=secret&link=opaque&file=t"})
	require.NoError(t, err)
	require.Equal(t, "torrent bytes", string(data))
	for _, link := range []string{"magnet:?xt=urn:btih:secret", "https://tracker.invalid/download?passkey=secret", server.URL + "/base/2/download", server.URL + "/api/v1/config"} {
		_, err := client.Download(t.Context(), 1, Result{Link: link})
		require.Error(t, err)
		require.NotContains(t, err.Error(), "secret")
	}
	require.Equal(t, 1, requests)
	_, err = client.Download(t.Context(), 1, Result{Link: server.URL + "/base/1/download?link=redirect"})
	require.ErrorContains(t, err, "HTTP 302")
	require.NotContains(t, err.Error(), "secret")
}

func TestClient_CancellationInterruptsSpacing(t *testing.T) {
	client, err := New("http://127.0.0.1:1", "key", time.Hour)
	require.NoError(t, err)
	client.nextRequest = time.Now().Add(time.Hour)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err = client.Indexers(ctx)
	require.ErrorIs(t, err, context.Canceled)
}

func TestClient_RateLimitDeadline(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "3600")
		w.WriteHeader(429)
		fmt.Fprint(w, "secret-key")
	}))
	defer server.Close()
	client, err := New(server.URL, "key", 0)
	require.NoError(t, err)
	_, err = client.Indexers(t.Context())
	var limited *CooldownError
	require.ErrorAs(t, err, &limited)
	require.WithinDuration(t, time.Now().Add(time.Hour), limited.Until, time.Second)
	require.NotContains(t, err.Error(), "secret-key")
}
