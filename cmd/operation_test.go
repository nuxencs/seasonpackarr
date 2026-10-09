// Copyright (c) 2023 - 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package cmd

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOperationCommand_ValidatesInputsBeforeRequests(t *testing.T) {
	isolateCLI(t)
	for _, args := range [][]string{
		{"candidate"},
		{"candidate", ""},
		{"candidate", "one", "two"},
		{"match"},
		{"match", "Series.S01.WEB-DL-GRP"},
		{"import", "Series.S01.WEB-DL-GRP"},
		{"import", "missing.torrent"},
		{"match", "pack.torrent", "--release", ""},
		{"import", "pack.torrent", "--release", "  "},
		{"candidate", "Series.S01", "--release", "Series.S01"},
		{"start", "extra"},
		{"version", "extra"},
		{"gen-token", "extra"},
		{"search", "extra"},
		{"search", "--verify"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			code, stdout, stderr := runCLI(t, args...)
			require.Equal(t, 1, code)
			require.Empty(t, stdout)
			require.Contains(t, stderr, "Error:")
		})
	}
}

func TestOperationCommand_ReportsResults(t *testing.T) {
	isolateCLI(t)
	for _, tt := range []struct {
		status, exit    int
		result, message string
	}{
		{250, 0, "accepted", "release-name checks passed"},
		{200, 2, "rejected", "no matching releases"},
		{204, 2, "rejected", "cut did not match"},
		{230, 2, "rejected", "below threshold"},
		{445, 2, "rejected", "could not match"},
		{401, 1, "failed", "authentication failed"},
		{472, 1, "failed", "--client"},
		{500, 1, "failed", "HTTP 500"},
	} {
		t.Run(fmt.Sprint(tt.status), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, "/proxy/api/candidate", r.URL.Path)
				assert.Equal(t, "POST", r.Method)
				assert.Equal(t, "secret", r.Header.Get("X-API-Token"))
				var body map[string]any
				assert.NoError(t, json.NewDecoder(r.Body).Decode(&body))
				assert.NotContains(t, body, "torrent")
				assert.Equal(t, "default", body["clientname"])
				w.WriteHeader(tt.status)
				if tt.status == 200 {
					fmt.Fprint(w, `{"statusCode":200,"error":"no matching releases in client"}`)
				}
			}))
			defer server.Close()
			for _, jsonOutput := range []bool{false, true} {
				args := []string{"candidate", "Series.S01", "--url", server.URL + "/proxy/", "--api", "secret"}
				if jsonOutput {
					args = append(args, "--json")
				}
				code, stdout, stderr := runCLI(t, args...)
				require.Equal(t, tt.exit, code)
				require.Empty(t, stderr)
				require.Contains(t, stdout, tt.message)
				require.NotContains(t, stdout, "secret")
				if jsonOutput {
					var result operationResult
					require.NoError(t, json.Unmarshal([]byte(stdout), &result))
					require.Equal(t, tt.result, result.Status)
					require.Equal(t, tt.status, result.StatusCode)
				}
			}
		})
	}
}

func TestOperationCommand_UsesRealTorrentIdentity(t *testing.T) {
	isolateCLI(t)
	path, data := writeTorrent(t)
	for _, operation := range []string{"match", "import"} {
		t.Run(operation, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, "/api/"+operation, r.URL.Path)
				var body struct {
					Name, ClientName string
					Torrent          []byte
				}
				assert.NoError(t, json.NewDecoder(r.Body).Decode(&body))
				assert.Equal(t, "Series.S01.1080p.WEB-DL-GRP", body.Name)
				assert.Equal(t, "tv", body.ClientName)
				assert.Equal(t, data, body.Torrent)
				w.WriteHeader(250)
			}))
			defer server.Close()
			args := []string{operation, path, "--url", server.URL, "--client", "tv", "--json"}
			code, stdout, stderr := runCLI(t, args...)
			require.Equal(t, 0, code, stderr)
			require.True(t, json.Valid([]byte(stdout)))
			require.Empty(t, stderr)
		})
	}
	badPath := filepath.Join(t.TempDir(), "bad.torrent")
	require.NoError(t, os.WriteFile(badPath, []byte("not a torrent"), 0o600))
	code, _, stderr := runCLI(t, "match", badPath)
	require.Equal(t, 1, code)
	require.Contains(t, stderr, "invalid torrent file")
}

func TestOperationCommand_RejectsUnexpectedHTTP200(t *testing.T) {
	isolateCLI(t)
	path, _ := writeTorrent(t)
	for _, response := range []string{
		`<html>Sign in</html>`, ``, `null`, `{}`,
		`{"error":"no matching releases in client"}`,
		`{"statusCode":250,"error":"no matching releases in client"}`,
		`{"statusCode":200,"error":"  "}`,
		`{"statusCode":200,"error":"no matching releases in client"} trailing`,
	} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprint(w, response)
		}))
		for _, operation := range []string{"candidate", "match", "import"} {
			input := "Series.S01"
			if operation != "candidate" {
				input = path
			}
			for _, jsonOutput := range []bool{false, true} {
				args := []string{operation, input, "--url", server.URL}
				if jsonOutput {
					args = append(args, "--json")
				}
				code, stdout, stderr := runCLI(t, args...)
				require.Equal(t, 1, code, response)
				require.Empty(t, stdout)
				require.Contains(t, stderr, "invalid "+operation+" response")
			}
		}
		server.Close()
	}
}

func TestOperationCommand_ReleaseOverridePreservesTorrent(t *testing.T) {
	isolateCLI(t)
	path, data := writeTorrent(t)
	const releaseName = "Series.S01.1080p.WEB-DL.H.264-GRP"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Name    string
			Torrent []byte
		}
		assert.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		assert.Equal(t, releaseName, body.Name)
		assert.Equal(t, data, body.Torrent)
		w.WriteHeader(250)
	}))
	defer server.Close()
	for _, operation := range []string{"match", "import"} {
		args := []string{operation, path, "--release", releaseName, "--url", server.URL, "--json"}
		code, stdout, stderr := runCLI(t, args...)
		require.Equal(t, 0, code, stderr)
		var result operationResult
		require.NoError(t, json.Unmarshal([]byte(stdout), &result))
		require.Equal(t, releaseName, result.Release)
	}
	code, _, stderr := runCLI(t, "match", "missing.torrent", "--release", releaseName)
	require.Equal(t, 1, code)
	require.Contains(t, stderr, "read torrent file")
}
