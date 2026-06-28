// Copyright (c) 2023 - 2025, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package torrentclient

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/nuxencs/seasonpackarr/internal/domain"
)

// rpcTestServer starts an httptest.Server that enforces Transmission's
// X-Transmission-Session-Id handshake and routes requests by RPC method name.
func rpcTestServer(t *testing.T, responses map[string]string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Transmission-Session-Id") == "" {
			w.Header().Set("X-Transmission-Session-Id", "testsid")
			w.WriteHeader(http.StatusConflict)
			return
		}
		body, _ := io.ReadAll(r.Body)
		var env struct {
			Method string `json:"method"`
		}
		_ = json.Unmarshal(body, &env)
		resp, ok := responses[env.Method]
		if !ok {
			http.Error(w, "unexpected method: "+env.Method, http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, resp)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func newClientFromServer(t *testing.T, srv *httptest.Server) *transmissionClient {
	t.Helper()
	c, err := newTransmissionClient(&domain.Client{Host: srv.URL})
	if err != nil {
		t.Fatalf("newTransmissionClient: %v", err)
	}
	return c
}

const (
	legacySessionGetResp   = `{"result":"success","arguments":{"rpc-version-semver":"5.3.0"}}`
	jsonrpc2SessionGetResp = `{"result":"success","arguments":{"rpc-version-semver":"6.0.1"}}`
	noSemverSessionGetResp = `{"result":"success","arguments":{}}`
)

func TestMajorVersion(t *testing.T) {
	t.Parallel()
	tests := []struct {
		semver string
		want   int
	}{
		{"6.0.1", 6},
		{"5.3.0", 5},
		{"10.0.0", 10},
		{"0.0.0", 0},
		{"", 0},
		{"notaversion", 0},
		{"abc.def.ghi", 0},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.semver, func(t *testing.T) {
			t.Parallel()
			if got := majorVersion(tt.semver); got != tt.want {
				t.Errorf("majorVersion(%q) = %d, want %d", tt.semver, got, tt.want)
			}
		})
	}
}

func TestNewTransmissionClient_SessionIDStored(t *testing.T) {
	t.Parallel()
	srv := rpcTestServer(t, map[string]string{"session-get": legacySessionGetResp})
	c := newClientFromServer(t, srv)
	if c.sessionID != "testsid" {
		t.Errorf("sessionID = %q, want %q", c.sessionID, "testsid")
	}
}

func TestNewTransmissionClient_LegacyProtocol(t *testing.T) {
	t.Parallel()
	srv := rpcTestServer(t, map[string]string{"session-get": legacySessionGetResp})
	c := newClientFromServer(t, srv)
	if c.proto != protoLegacy {
		t.Errorf("proto = %v, want protoLegacy", c.proto)
	}
}

func TestNewTransmissionClient_JSONRPC2Protocol(t *testing.T) {
	t.Parallel()
	srv := rpcTestServer(t, map[string]string{"session-get": jsonrpc2SessionGetResp})
	c := newClientFromServer(t, srv)
	if c.proto != protoJSONRPC2 {
		t.Errorf("proto = %v, want protoJSONRPC2", c.proto)
	}
}

func TestNewTransmissionClient_NoSemverFallsBackToLegacy(t *testing.T) {
	t.Parallel()
	srv := rpcTestServer(t, map[string]string{"session-get": noSemverSessionGetResp})
	c := newClientFromServer(t, srv)
	if c.proto != protoLegacy {
		t.Errorf("proto = %v, want protoLegacy", c.proto)
	}
}

func TestNew_TransmissionType(t *testing.T) {
	t.Parallel()
	srv := rpcTestServer(t, map[string]string{"session-get": legacySessionGetResp})
	c, err := New(&domain.Client{Type: "transmission", Host: srv.URL})
	if err != nil {
		t.Fatalf("New(transmission): %v", err)
	}
	if c == nil {
		t.Fatal("New(transmission) returned nil client")
	}
}

func TestTransmissionGetTorrents_Legacy(t *testing.T) {
	t.Parallel()
	const resp = `{"result":"success","arguments":{"torrents":[{"hashString":"abc123","name":"Show.S01","downloadDir":"/downloads"}]}}`
	srv := rpcTestServer(t, map[string]string{
		"session-get": legacySessionGetResp,
		"torrent-get": resp,
	})
	c := newClientFromServer(t, srv)

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
}

func TestTransmissionGetTorrents_JSONRPC2(t *testing.T) {
	t.Parallel()
	const resp = `{"jsonrpc":"2.0","id":1,"result":{"torrents":[{"hash_string":"def456","name":"Show.S02","download_dir":"/media"}]}}`
	srv := rpcTestServer(t, map[string]string{
		"session-get": jsonrpc2SessionGetResp,
		"torrent_get": resp,
	})
	c := newClientFromServer(t, srv)

	torrents, err := c.GetTorrents()
	if err != nil {
		t.Fatalf("GetTorrents: %v", err)
	}
	if len(torrents) != 1 {
		t.Fatalf("len(torrents) = %d, want 1", len(torrents))
	}
	got := torrents[0]
	if got.Hash != "def456" || got.Name != "Show.S02" || got.SavePath != "/media" {
		t.Errorf("torrent = %+v, want {Hash:def456 Name:Show.S02 SavePath:/media}", got)
	}
}

func TestTransmissionGetFiles_Legacy(t *testing.T) {
	t.Parallel()
	// Multi-file torrent: file paths include the top-level folder prefix.
	const resp = `{"result":"success","arguments":{"torrents":[{"files":[{"name":"Show.S01/E01.mkv","length":1000000},{"name":"Show.S01/E02.mkv","length":1050000}]}]}}`
	srv := rpcTestServer(t, map[string]string{
		"session-get": legacySessionGetResp,
		"torrent-get": resp,
	})
	c := newClientFromServer(t, srv)

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
}

func TestTransmissionGetFiles_JSONRPC2(t *testing.T) {
	t.Parallel()
	const resp = `{"jsonrpc":"2.0","id":2,"result":{"torrents":[{"files":[{"name":"Show.S02/E01.mkv","length":2000000},{"name":"Show.S02/E02.mkv","length":2100000}]}]}}`
	srv := rpcTestServer(t, map[string]string{
		"session-get": jsonrpc2SessionGetResp,
		"torrent_get": resp,
	})
	c := newClientFromServer(t, srv)

	files, err := c.GetFiles("def456")
	if err != nil {
		t.Fatalf("GetFiles: %v", err)
	}
	if len(files) != 2 {
		t.Fatalf("len(files) = %d, want 2", len(files))
	}
	if files[0].Name != "Show.S02/E01.mkv" || files[0].Size != 2000000 {
		t.Errorf("files[0] = %+v, want {Name:Show.S02/E01.mkv Size:2000000}", files[0])
	}
	if files[1].Name != "Show.S02/E02.mkv" || files[1].Size != 2100000 {
		t.Errorf("files[1] = %+v, want {Name:Show.S02/E02.mkv Size:2100000}", files[1])
	}
}

func TestTransmissionGetFiles_NotFound_Legacy(t *testing.T) {
	t.Parallel()
	const resp = `{"result":"success","arguments":{"torrents":[]}}`
	srv := rpcTestServer(t, map[string]string{
		"session-get": legacySessionGetResp,
		"torrent-get": resp,
	})
	c := newClientFromServer(t, srv)

	_, err := c.GetFiles("notexist")
	if err == nil {
		t.Fatal("expected error for missing torrent, got nil")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("error = %q, want to contain 'not found'", err.Error())
	}
}

func TestTransmissionGetFiles_NotFound_JSONRPC2(t *testing.T) {
	t.Parallel()
	const resp = `{"jsonrpc":"2.0","id":1,"result":{"torrents":[]}}`
	srv := rpcTestServer(t, map[string]string{
		"session-get": jsonrpc2SessionGetResp,
		"torrent_get": resp,
	})
	c := newClientFromServer(t, srv)

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
	var gotUser, gotPass string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Transmission-Session-Id") == "" {
			w.Header().Set("X-Transmission-Session-Id", "testsid")
			w.WriteHeader(http.StatusConflict)
			return
		}
		gotUser, gotPass, _ = r.BasicAuth()
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, legacySessionGetResp)
	}))
	t.Cleanup(srv.Close)

	_, err := newTransmissionClient(&domain.Client{
		Host:     srv.URL,
		Username: "admin",
		Password: "secret",
	})
	if err != nil {
		t.Fatalf("newTransmissionClient: %v", err)
	}
	if gotUser != "admin" || gotPass != "secret" {
		t.Errorf("BasicAuth = (%q, %q), want (admin, secret)", gotUser, gotPass)
	}
}

func TestTransmissionClient_JSONRPCError(t *testing.T) {
	t.Parallel()
	const errResp = `{"jsonrpc":"2.0","id":1,"error":{"code":-1,"message":"no such torrent"}}`
	srv := rpcTestServer(t, map[string]string{
		"session-get": jsonrpc2SessionGetResp,
		"torrent_get": errResp,
	})
	c := newClientFromServer(t, srv)

	_, err := c.GetTorrents()
	if err == nil {
		t.Fatal("expected error from JSON-RPC error object, got nil")
	}
	if !strings.Contains(err.Error(), "no such torrent") {
		t.Errorf("error = %q, want to contain 'no such torrent'", err.Error())
	}
}
