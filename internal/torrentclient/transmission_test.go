// Copyright (c) 2023 - 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package torrentclient

import (
	"context"
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

	"github.com/hekmon/transmissionrpc/v3"
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
	client := newTestTransmissionClient(&fakeTransmissionAPI{getErr: errBoom}, domain.ImportPolicy{})
	results := client.GetFiles(t.Context(), []string{"one", "two"})

	require.Len(t, results, 2)
	for index, hash := range []string{"one", "two"} {
		require.Equal(t, hash, results[index].Hash)
		require.ErrorIs(t, results[index].Err, errBoom)
	}
}

func TestNewTransmissionClient_UsesBasicAuth(t *testing.T) {
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

type fakeTransmissionAPI struct {
	addCalled  bool
	addPayload transmissionrpc.TorrentAddPayload

	getErr error

	sessionDir string
}

func (s *fakeTransmissionAPI) TorrentGet(context.Context, []string, []int64) ([]transmissionrpc.Torrent, error) {
	return nil, nil
}

func (s *fakeTransmissionAPI) TorrentGetHashes(context.Context, []string, []string) ([]transmissionrpc.Torrent, error) {
	return nil, s.getErr
}

func (s *fakeTransmissionAPI) TorrentAdd(_ context.Context, payload transmissionrpc.TorrentAddPayload) (transmissionrpc.Torrent, error) {
	s.addCalled = true
	s.addPayload = payload
	return transmissionrpc.Torrent{}, nil
}

func (s *fakeTransmissionAPI) SessionArgumentsGetAll(context.Context) (transmissionrpc.SessionArguments, error) {
	dir := s.sessionDir
	return transmissionrpc.SessionArguments{DownloadDir: &dir}, nil
}

func newTestTransmissionClient(api *fakeTransmissionAPI, policy domain.ImportPolicy) *transmissionClient {
	return &transmissionClient{
		c:      api,
		policy: policy,
	}
}

// TestTransmissionImport_AddsStartedWithoutVerify is the regression guard for
// the autobrr timeout: Transmission verifies a partial pack and then starts it
// by itself, so the adapter must not force or wait for a verify.
func TestTransmissionImport_AddsStartedWithoutVerify(t *testing.T) {
	const hash = "abc123"
	api := &fakeTransmissionAPI{}
	tc := newTestTransmissionClient(api, domain.ImportPolicy{SavePath: "/data/tv", Tags: []string{"seasonpackarr"}})

	report, err := tc.Import(t.Context(), ImportRequest{TorrentBytes: []byte("torrent"), LegacyHash: hash, HasV1: true, SavePath: "/data/tv"})
	require.NoError(t, err)
	require.Equal(t, []ImportStage{
		ImportStageConfig,
		ImportStageAdd,
	}, importStageNames(report))

	require.True(t, api.addCalled)
	require.NotNil(t, api.addPayload.MetaInfo)
	require.NotNil(t, api.addPayload.DownloadDir)
	require.Equal(t, "/data/tv", *api.addPayload.DownloadDir)
	require.NotNil(t, api.addPayload.Paused)
	require.False(t, *api.addPayload.Paused)
	require.Equal(t, []string{"seasonpackarr"}, api.addPayload.Labels)
}

func TestTransmissionImportDestination(t *testing.T) {
	t.Run("explicit save path wins", func(t *testing.T) {
		tc := newTestTransmissionClient(&fakeTransmissionAPI{sessionDir: "/downloads"}, domain.ImportPolicy{SavePath: "/data/tv"})
		destination, err := tc.ImportDestination(t.Context())
		require.NoError(t, err)
		require.Equal(t, normalizePath("/data/tv"), destination.SavePath())
	})

	t.Run("falls back to session download dir", func(t *testing.T) {
		tc := newTestTransmissionClient(&fakeTransmissionAPI{sessionDir: "/downloads"}, domain.ImportPolicy{})
		destination, err := tc.ImportDestination(t.Context())
		require.NoError(t, err)
		require.Equal(t, normalizePath("/downloads"), destination.SavePath())
	})

	t.Run("errors when download dir empty", func(t *testing.T) {
		tc := newTestTransmissionClient(&fakeTransmissionAPI{sessionDir: ""}, domain.ImportPolicy{})
		_, err := tc.ImportDestination(t.Context())
		require.Error(t, err)
		require.Equal(t, domain.StatusImportConfigError, ImportStatusCode(err))
	})
}

func TestBuildTransmissionURL(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		client  *domain.Client
		want    string
		wantErr bool
	}{
		{
			name:    "empty host",
			client:  &domain.Client{Host: ""},
			wantErr: true,
		},
		{
			name:   "bare hostname appends rpc path",
			client: &domain.Client{Host: "localhost"},
			want:   "http://localhost/transmission/rpc",
		},
		{
			name:   "bare hostname with port field",
			client: &domain.Client{Host: "localhost", Port: 9091},
			want:   "http://localhost:9091/transmission/rpc",
		},
		{
			name:   "hostname with http scheme",
			client: &domain.Client{Host: "http://myhost"},
			want:   "http://myhost/transmission/rpc",
		},
		{
			name:   "hostname with https scheme",
			client: &domain.Client{Host: "https://myhost"},
			want:   "https://myhost/transmission/rpc",
		},
		{
			name:   "ip address with port field",
			client: &domain.Client{Host: "192.168.1.1", Port: 9091},
			want:   "http://192.168.1.1:9091/transmission/rpc",
		},
		{
			name:   "existing port overridden by port field",
			client: &domain.Client{Host: "http://localhost:8080", Port: 9091},
			want:   "http://localhost:9091/transmission/rpc",
		},
		{
			name:   "zero port field does not append port",
			client: &domain.Client{Host: "http://localhost", Port: 0},
			want:   "http://localhost/transmission/rpc",
		},
		{
			name:   "credentials embedded as user info",
			client: &domain.Client{Host: "localhost", Port: 9091, Username: "admin", Password: "secret"},
			want:   "http://admin:secret@localhost:9091/transmission/rpc",
		},
		{
			name:   "username only still embeds user info",
			client: &domain.Client{Host: "localhost", Username: "admin"},
			want:   "http://admin:@localhost/transmission/rpc",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := buildTransmissionURL(tt.client)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.want, got.String())
		})
	}
}
