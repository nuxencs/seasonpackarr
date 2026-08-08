// Copyright (c) 2023 - 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package torrentclient

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/nuxencs/seasonpackarr/internal/domain"
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
		fmt.Fprintf(w, `{"result":"success","tag":%d,"arguments":%s}`, req.Tag, args)
	}))
	t.Cleanup(srv.Close)
	return srv, &captured
}

// emptySessionResp satisfies the constructor's session-get ping.
const emptySessionResp = `{}`

func newTransmissionClientFromServer(t *testing.T, srv *httptest.Server, user, pass string) *transmissionClient {
	t.Helper()
	c, err := newTransmissionClient(&domain.Client{Host: srv.URL, Username: user, Password: pass})
	if err != nil {
		t.Fatalf("newTransmissionClient: %v", err)
	}
	return c
}

// lastRequest returns the most recent captured request for the given method.
func lastRequest(t *testing.T, captured *[]capturedRequest, method string) capturedRequest {
	t.Helper()
	for i := len(*captured) - 1; i >= 0; i-- {
		if (*captured)[i].Method == method {
			return (*captured)[i]
		}
	}
	t.Fatalf("no captured request for method %q", method)
	return capturedRequest{}
}

// assertStringSlice checks that a JSON-decoded argument value is a string array
// equal to want (order-sensitive).
func assertStringSlice(t *testing.T, got any, want []string) {
	t.Helper()
	raw, ok := got.([]any)
	if !ok {
		t.Fatalf("value %v (%T) is not a JSON array", got, got)
	}
	if len(raw) != len(want) {
		t.Fatalf("array = %v, want %v", raw, want)
	}
	for i, w := range want {
		s, ok := raw[i].(string)
		if !ok {
			t.Fatalf("element %d %v (%T) is not a string", i, raw[i], raw[i])
		}
		if s != w {
			t.Errorf("element %d = %q, want %q", i, s, w)
		}
	}
}

func TestNewTransmissionClient_SessionHandshakeAndPing(t *testing.T) {
	t.Parallel()
	srv, captured := transmissionTestServer(t, map[string]string{"session-get": emptySessionResp})
	newTransmissionClientFromServer(t, srv, "", "")
	// The constructor pings via session-get; reaching here means the 409 handshake
	// and the authenticated retry both completed.
	lastRequest(t, captured, "session-get")
}

func TestNew_TransmissionType(t *testing.T) {
	t.Parallel()
	srv, _ := transmissionTestServer(t, map[string]string{"session-get": emptySessionResp})
	c, err := New(&domain.Client{Type: "transmission", Host: srv.URL})
	if err != nil {
		t.Fatalf("New(transmission): %v", err)
	}
	if c == nil {
		t.Fatal("New(transmission) returned nil client")
	}
}

func TestTransmissionGetTorrents(t *testing.T) {
	t.Parallel()
	const resp = `{"torrents":[{"hashString":"abc123","name":"Show.S01","downloadDir":"/downloads"}]}`
	srv, captured := transmissionTestServer(t, map[string]string{
		"session-get": emptySessionResp,
		"torrent-get": resp,
	})
	c := newTransmissionClientFromServer(t, srv, "", "")

	torrents, err := c.GetTorrents()
	if err != nil {
		t.Fatalf("GetTorrents: %v", err)
	}
	if len(torrents) != 1 {
		t.Fatalf("len(torrents) = %d, want 1", len(torrents))
	}
	got := torrents[0]
	if got.Hash != "abc123" || got.Name != "Show.S01" || got.SavePath != "/downloads" {
		t.Errorf("torrent = %+v, want {Hash:abc123 Name:Show.S01 SavePath:/downloads}", got)
	}

	// Wire-format assertion: the adapter must request exactly the fields it maps,
	// and must not scope the listing by ids.
	req := lastRequest(t, captured, "torrent-get")
	assertStringSlice(t, req.Arguments["fields"], []string{"hashString", "name", "downloadDir"})
	if _, ok := req.Arguments["ids"]; ok {
		t.Errorf("GetTorrents must not send ids, got %v", req.Arguments["ids"])
	}
}

func TestTransmissionGetFiles(t *testing.T) {
	t.Parallel()
	// Multi-file torrent: file paths include the top-level folder prefix.
	const resp = `{"torrents":[{"files":[{"name":"Show.S01/E01.mkv","length":1000000},{"name":"Show.S01/E02.mkv","length":1050000}]}]}`
	srv, captured := transmissionTestServer(t, map[string]string{
		"session-get": emptySessionResp,
		"torrent-get": resp,
	})
	c := newTransmissionClientFromServer(t, srv, "", "")

	files, err := c.GetFiles("abc123")
	if err != nil {
		t.Fatalf("GetFiles: %v", err)
	}
	if len(files) != 2 {
		t.Fatalf("len(files) = %d, want 2", len(files))
	}
	if files[0].Name != "Show.S01/E01.mkv" || files[0].Size != 1000000 {
		t.Errorf("files[0] = %+v, want {Name:Show.S01/E01.mkv Size:1000000}", files[0])
	}
	if files[1].Name != "Show.S01/E02.mkv" || files[1].Size != 1050000 {
		t.Errorf("files[1] = %+v, want {Name:Show.S01/E02.mkv Size:1050000}", files[1])
	}

	// Wire-format assertion: the hash is sent as the ids identifier, files requested.
	req := lastRequest(t, captured, "torrent-get")
	assertStringSlice(t, req.Arguments["fields"], []string{"files"})
	assertStringSlice(t, req.Arguments["ids"], []string{"abc123"})
}

func TestTransmissionGetFiles_NotFound(t *testing.T) {
	t.Parallel()
	const resp = `{"torrents":[]}`
	srv, _ := transmissionTestServer(t, map[string]string{
		"session-get": emptySessionResp,
		"torrent-get": resp,
	})
	c := newTransmissionClientFromServer(t, srv, "", "")

	_, err := c.GetFiles("notexist")
	if err == nil {
		t.Fatal("expected error for missing torrent, got nil")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("error = %q, want to contain 'not found'", err.Error())
	}
}

func TestTransmissionClient_BasicAuth(t *testing.T) {
	t.Parallel()
	srv, captured := transmissionTestServer(t, map[string]string{"session-get": emptySessionResp})
	newTransmissionClientFromServer(t, srv, "admin", "secret")

	req := lastRequest(t, captured, "session-get")
	if !req.HadAuth {
		t.Fatal("expected basic auth header to be sent")
	}
	if req.User != "admin" || req.Pass != "secret" {
		t.Errorf("BasicAuth = (%q, %q), want (admin, secret)", req.User, req.Pass)
	}
}

func TestNewTransmissionClient_ConnectErrorFailsFast(t *testing.T) {
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

	_, err := newTransmissionClient(&domain.Client{Host: srv.URL, Username: "x", Password: "y"})
	if err == nil {
		t.Fatal("expected error when server rejects auth, got nil")
	}
	if !strings.Contains(err.Error(), "connect to transmission") {
		t.Errorf("error = %q, want to contain 'connect to transmission'", err.Error())
	}
}
