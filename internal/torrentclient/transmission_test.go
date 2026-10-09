// Copyright (c) 2023 - 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package torrentclient

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"sync"
	"testing"

	"github.com/nuxencs/seasonpackarr/internal/domain"

	"github.com/stretchr/testify/require"
)

// capturedRequest records a single decoded Transmission RPC request for assertions.
type capturedRequest struct {
	Method    string
	Arguments map[string]any
	User      string
	Pass      string
	HadAuth   bool
}

// transmissionTestServer starts an httptest.Server that enforces the
// X-Transmission-Session-Id 409 handshake, echoes the request tag (the library
// rejects mismatched tags), routes canned argument payloads by method name, and
// records every request so tests can assert the wire format the adapter produces.
func transmissionTestServer(t *testing.T, responses map[string]string) (*httptest.Server, *[]capturedRequest) {
	t.Helper()
	var mu sync.Mutex
	captured := make([]capturedRequest, 0)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Transmission-Session-Id") == "" {
			w.Header().Set("X-Transmission-Session-Id", "testsid")
			w.WriteHeader(http.StatusConflict)
			return
		}

		body, _ := io.ReadAll(r.Body)
		var req struct {
			Method    string         `json:"method"`
			Arguments map[string]any `json:"arguments"`
			Tag       int            `json:"tag"`
		}
		_ = json.Unmarshal(body, &req)

		user, pass, hadAuth := r.BasicAuth()
		mu.Lock()
		captured = append(captured, capturedRequest{
			Method:    req.Method,
			Arguments: req.Arguments,
			User:      user,
			Pass:      pass,
			HadAuth:   hadAuth,
		})
		mu.Unlock()

		args, ok := responses[req.Method]
		if !ok {
			http.Error(w, "unexpected method: "+req.Method, http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{"result":"success","tag":%d,"arguments":%s}`, req.Tag, args)
	}))
	t.Cleanup(srv.Close)
	return srv, &captured
}

// emptySessionResp satisfies the constructor's session-get ping.
const emptySessionResp = `{}`

func newTransmissionClientFromServer(t *testing.T, srv *httptest.Server, user, pass string) *transmissionClient {
	t.Helper()
	c, err := newTransmissionClient(t.Context(), &domain.Client{Host: srv.URL, Username: user, Password: pass})
	require.NoError(t, err)
	return c
}

// lastRequest returns the most recent captured request for the given method.
func lastRequest(t *testing.T, captured *[]capturedRequest, method string) capturedRequest {
	t.Helper()
	for i := range slices.Backward(*captured) {
		if (*captured)[i].Method == method {
			return (*captured)[i]
		}
	}
	require.FailNow(t, "no captured request", "method %q", method)
	return capturedRequest{}
}

// requireStringSlice checks that a JSON-decoded argument value is a string
// array equal to want (order-sensitive).
func requireStringSlice(t *testing.T, got any, want []string) {
	t.Helper()
	raw, ok := got.([]any)
	require.True(t, ok, "value %v (%T) is not a JSON array", got, got)
	values := make([]string, 0, len(raw))
	for index, element := range raw {
		value, ok := element.(string)
		require.True(t, ok, "element %d %v (%T) is not a string", index, element, element)
		values = append(values, value)
	}
	require.Equal(t, want, values)
}

func TestNewTransmissionClient_PerformsSessionHandshakeAndPing(t *testing.T) {
	t.Parallel()
	srv, captured := transmissionTestServer(t, map[string]string{"session-get": emptySessionResp})
	newTransmissionClientFromServer(t, srv, "", "")
	// The constructor pings via session-get; reaching here means the 409 handshake
	// and the authenticated retry both completed.
	lastRequest(t, captured, "session-get")
}

func TestNew_CreatesTransmissionClient(t *testing.T) {
	t.Parallel()
	srv, _ := transmissionTestServer(t, map[string]string{"session-get": emptySessionResp})
	c, err := New(t.Context(), &domain.Client{Type: "transmission", Host: srv.URL})
	require.NoError(t, err)
	require.NotNil(t, c)
}

func TestTransmissionGetTorrents(t *testing.T) {
	t.Parallel()
	const resp = `{"torrents":[{"hashString":"abc123","name":"Show.S01","downloadDir":"/downloads"}]}`
	srv, captured := transmissionTestServer(t, map[string]string{
		"session-get": emptySessionResp,
		"torrent-get": resp,
	})
	c := newTransmissionClientFromServer(t, srv, "", "")

	torrents, err := c.GetTorrents(t.Context())
	require.NoError(t, err)
	require.Equal(t, []Torrent{{Hash: "abc123", Name: "Show.S01", SavePath: "/downloads"}}, torrents)

	// Wire-format assertion: the adapter must request exactly the fields it maps,
	// and must not scope the listing by ids.
	req := lastRequest(t, captured, "torrent-get")
	requireStringSlice(t, req.Arguments["fields"], []string{"hashString", "name", "downloadDir"})
	require.NotContains(t, req.Arguments, "ids", "GetTorrents must not scope the listing")
}

func TestTransmissionGetFiles(t *testing.T) {
	t.Parallel()
	// The server response order differs from the requested hash order.
	const resp = `{"torrents":[{"hashString":"DEF456","files":[{"name":"Other.S01/E01.mkv","length":2000000}]},{"hashString":"abc123","files":[{"name":"Show.S01/E01.mkv","length":1000000},{"name":"Show.S01/E02.mkv","length":1050000}]}]}`
	srv, captured := transmissionTestServer(t, map[string]string{
		"session-get": emptySessionResp,
		"torrent-get": resp,
	})
	c := newTransmissionClientFromServer(t, srv, "", "")

	results := c.GetFiles(t.Context(), []string{"abc123", "def456"})
	require.Len(t, results, 2)
	require.NoError(t, results[0].Err)
	require.Equal(t, "abc123", results[0].Hash)
	require.Len(t, results[0].Files, 2)
	require.Equal(t, "Show.S01/E01.mkv", results[0].Files[0].Name)
	require.EqualValues(t, 1000000, results[0].Files[0].Size)
	require.NoError(t, results[1].Err)
	require.Equal(t, "def456", results[1].Hash)
	require.Len(t, results[1].Files, 1)

	// Wire-format assertion: both hashes use one request with identity and files.
	req := lastRequest(t, captured, "torrent-get")
	requireStringSlice(t, req.Arguments["fields"], []string{"hashString", "files"})
	requireStringSlice(t, req.Arguments["ids"], []string{"abc123", "def456"})
}

func TestTransmissionGetFiles_ReportsMissingTorrent(t *testing.T) {
	t.Parallel()
	const resp = `{"torrents":[{"hashString":"abc123","files":[{"name":"Show.S01/E01.mkv","length":1}]}]}`
	srv, _ := transmissionTestServer(t, map[string]string{
		"session-get": emptySessionResp,
		"torrent-get": resp,
	})
	c := newTransmissionClientFromServer(t, srv, "", "")

	results := c.GetFiles(t.Context(), []string{"abc123", "notexist"})
	require.Len(t, results, 2)
	require.NoError(t, results[0].Err)
	require.ErrorContains(t, results[1].Err, "not found")
}

func TestTransmissionGetFiles_ExpandsWholeCallError(t *testing.T) {
	t.Parallel()

	errBoom := errors.New("boom")
	client := newTestTransmissionClient(&stubTransmissionAPI{getErr: errBoom}, domain.ImportPolicy{})
	results := client.GetFiles(t.Context(), []string{"one", "two"})

	require.Len(t, results, 2)
	for index, hash := range []string{"one", "two"} {
		require.Equal(t, hash, results[index].Hash)
		require.ErrorIs(t, results[index].Err, errBoom)
	}
}

func TestTransmissionClient_UsesBasicAuth(t *testing.T) {
	t.Parallel()
	srv, captured := transmissionTestServer(t, map[string]string{"session-get": emptySessionResp})
	newTransmissionClientFromServer(t, srv, "admin", "secret")

	req := lastRequest(t, captured, "session-get")
	require.True(t, req.HadAuth, "basic auth header was not sent")
	require.Equal(t, "admin", req.User)
	require.Equal(t, "secret", req.Pass)
}

func TestNewTransmissionClient_FailsFastOnConnectionError(t *testing.T) {
	t.Parallel()
	// Server completes the 409 handshake but then rejects auth, so the constructor
	// ping must surface a connect error rather than returning a usable client.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Transmission-Session-Id") == "" {
			w.Header().Set("X-Transmission-Session-Id", "testsid")
			w.WriteHeader(http.StatusConflict)
			return
		}
		w.WriteHeader(http.StatusUnauthorized)
	}))
	t.Cleanup(srv.Close)

	_, err := newTransmissionClient(t.Context(), &domain.Client{Host: srv.URL, Username: "x", Password: "y"})
	require.ErrorContains(t, err, "connect to transmission")
}
