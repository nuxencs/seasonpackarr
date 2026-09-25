// Copyright (c) 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSearchCommand_PreviewRequestAndFailureExit(t *testing.T) {
	isolateCLI(t)
	for _, test := range []struct {
		name, response string
		fail           bool
	}{
		{"preview", `{"dryRun":true,"outcomes":[{"status":"would_import"}],"failures":[]}`, false},
		{"partial failure", `{"outcomes":[],"failures":[{"reason":"tracker failed"}]}`, true},
		{"import failure", `{"outcomes":[{"status":"failed"}],"failures":[]}`, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				require.Equal(t, "POST", r.Method)
				require.Equal(t, "/api/search", r.URL.Path)
				require.Equal(t, "test-token", r.Header.Get("X-API-Token"))
				var body map[string]any
				require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
				require.Equal(t, "default", body["clientname"])
				require.Equal(t, true, body["dryRun"])
				require.Equal(t, false, body["verify"])
				fmt.Fprint(w, test.response)
			}))
			defer server.Close()
			cmd := newSearchCommand()
			var output bytes.Buffer
			cmd.SetOut(&output)
			cmd.SetErr(&output)
			cmd.SetArgs([]string{"--url", server.URL, "--api", "test-token", "--client", "default", "--dry-run", "--json"})
			err := cmd.ExecuteContext(t.Context())
			if test.fail {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			require.Contains(t, output.String(), `"outcomes"`)
		})
	}
}

func TestSearchCommand_VerifyRequiresDryRun(t *testing.T) {
	cmd := newSearchCommand()
	cmd.SetArgs([]string{"--verify"})
	require.ErrorContains(t, cmd.ExecuteContext(t.Context()), "--verify requires --dry-run")
}

func TestSearchCommand_ReadableSummaryAndJSONContract(t *testing.T) {
	isolateCLI(t)
	response := `{"dryRun":true,"verify":true,"scannedTorrents":12,"episodeTorrents":10,"coveredEpisodeTorrents":2,"groups":1,"requests":2,"torrentDownloads":1,"torrentCacheHits":0,"outcomes":[{"clientname":"tv","title":"Series.S01.1080p.WEB-DL-GRP","indexerId":7,"status":"would_import","reusableEpisodes":8,"totalEpisodes":10}],"failures":[],"futureField":42}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		require.Equal(t, true, body["dryRun"])
		require.Equal(t, true, body["verify"])
		fmt.Fprint(w, response)
	}))
	defer server.Close()
	args := []string{"search", "--url", server.URL, "--dry-run", "--verify"}
	code, stdout, stderr := runCLI(t, args...)
	require.Equal(t, 0, code, stderr)
	for _, want := range []string{"Exact preview complete", "12 torrents", "1 groups, 2 requests", "would import", "Client: tv", "Reuse: 8/10 episodes"} {
		require.Contains(t, stdout, want)
	}
	code, stdout, stderr = runCLI(t, append(args, "--json")...)
	require.Equal(t, 0, code, stderr)
	require.Empty(t, stderr)
	require.JSONEq(t, response, stdout)
}

func TestSearchCommand_InvalidResponses(t *testing.T) {
	isolateCLI(t)
	for _, response := range []string{`null`, `{}`, `<html>Sign in</html>`, `{"outcomes":[],"failures":[]} trailing`} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, response) }))
		code, stdout, stderr := runCLI(t, "search", "--url", server.URL, "--dry-run", "--json")
		server.Close()
		require.Equal(t, 1, code)
		require.Empty(t, stdout)
		require.Contains(t, stderr, "invalid search response")
	}
}

func TestSearchCommand_DefaultImportAndPartialFailures(t *testing.T) {
	isolateCLI(t)
	for _, response := range []string{
		`{"outcomes":[],"failures":[{"reason":"tracker unavailable","indexerId":5}]}`,
		`{"outcomes":[{"status":"failed","reason":"import failed","title":"Series.S01","clientname":"tv"}],"failures":[]}`,
	} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var body map[string]any
			require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
			require.Equal(t, false, body["dryRun"])
			require.Equal(t, false, body["verify"])
			fmt.Fprint(w, response)
		}))
		code, stdout, stderr := runCLI(t, "search", "--url", server.URL)
		server.Close()
		require.Equal(t, 1, code)
		require.Contains(t, stdout, "finished with failures")
		require.Contains(t, stderr, "Search and import started")
	}
}
