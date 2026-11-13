// Copyright (c) 2023 - 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/autobrr/go-torrent/bencode"
	"github.com/autobrr/go-torrent/metainfo"
	"github.com/stretchr/testify/require"
)

func isolateCLI(t *testing.T) {
	t.Helper()
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		if strings.HasPrefix(key, "SEASONPACKARR__") {
			t.Setenv(key, "")
		}
	}
	t.Setenv("SEASONPACKARR__DISABLE_CONFIG_FILE", "true")
	t.Chdir(t.TempDir())
}

func runCLI(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := execute(t.Context(), args, &stdout, &stderr)
	return code, stdout.String(), stderr.String()
}

func TestCommands_ReportsFailures(t *testing.T) {
	isolateCLI(t)
	for _, args := range [][]string{
		{"match"},
		{"candidate", "Series.S01.1080p.WEB-DL-GRP", "--port", "1"},
	} {
		code, stdout, stderr := runCLI(t, args...)
		require.Equal(t, 1, code)
		require.Empty(t, stdout)
		require.Contains(t, stderr, "Error:")
	}
}

func TestCommands_ValidateInputsBeforeRequests(t *testing.T) {
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

func TestCommands_OperationResults(t *testing.T) {
	isolateCLI(t)
	for _, test := range []struct {
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
		t.Run(fmt.Sprint(test.status), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				require.Equal(t, "/proxy/api/candidate", r.URL.Path)
				require.Equal(t, "POST", r.Method)
				require.Equal(t, "secret", r.Header.Get("X-API-Token"))
				var body map[string]any
				require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
				require.NotContains(t, body, "torrent")
				require.Equal(t, "default", body["clientname"])
				w.WriteHeader(test.status)
				if test.status == 200 {
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
				require.Equal(t, test.exit, code)
				require.Empty(t, stderr)
				require.Contains(t, stdout, test.message)
				require.NotContains(t, stdout, "secret")
				if jsonOutput {
					var result operationResult
					require.NoError(t, json.Unmarshal([]byte(stdout), &result))
					require.Equal(t, test.result, result.Status)
					require.Equal(t, test.status, result.StatusCode)
				}
			}
		})
	}
}

func writeTorrent(t *testing.T) (string, []byte) {
	t.Helper()
	info, err := bencode.Marshal(metainfo.Info{
		Name: "Series.S01.1080p.WEB-DL-GRP", PieceLength: 16384,
		Pieces: make([]byte, 20),
		Files:  []metainfo.FileInfo{{Path: []string{"Series.S01E01.1080p.WEB-DL-GRP.mkv"}, Length: 1}},
	})
	require.NoError(t, err)
	meta := metainfo.MetaInfo{InfoBytes: info}
	var data bytes.Buffer
	require.NoError(t, meta.Write(&data))
	path := filepath.Join(t.TempDir(), "download.TORRENT")
	require.NoError(t, os.WriteFile(path, data.Bytes(), 0o600))
	return path, data.Bytes()
}

func TestCommands_UseRealTorrentIdentity(t *testing.T) {
	isolateCLI(t)
	path, data := writeTorrent(t)
	for _, operation := range []string{"match", "import"} {
		t.Run(operation, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				require.Equal(t, "/api/"+operation, r.URL.Path)
				var body struct {
					Name, ClientName string
					Torrent          []byte
				}
				require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
				require.Equal(t, "Series.S01.1080p.WEB-DL-GRP", body.Name)
				require.Equal(t, "tv", body.ClientName)
				require.Equal(t, data, body.Torrent)
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

func TestCommands_LocalToolsAndHelp(t *testing.T) {
	isolateCLI(t)
	t.Setenv("SEASONPACKARR__PORT", "invalid")
	for _, args := range [][]string{{"version"}, {"gen-token"}, {"--help"}, {"match", "--help"}} {
		code, stdout, stderr := runCLI(t, args...)
		require.Equal(t, 0, code, stderr)
		require.NotEmpty(t, stdout)
		require.Empty(t, stderr)
	}
	_, help, _ := runCLI(t, "match", "--help")
	require.Contains(t, help, "<file.torrent>")
	require.Contains(t, help, "--config")
	require.Contains(t, help, "--url")
	require.Contains(t, help, "--release")
}

func TestCommands_RejectUnexpectedHTTP200(t *testing.T) {
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

func TestCommands_ReleaseOverridePreservesTorrent(t *testing.T) {
	isolateCLI(t)
	path, data := writeTorrent(t)
	const releaseName = "Series.S01.1080p.WEB-DL.H.264-GRP"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Name    string
			Torrent []byte
		}
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		require.Equal(t, releaseName, body.Name)
		require.Equal(t, data, body.Torrent)
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
