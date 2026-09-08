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
			cmd.SetArgs([]string{"--url", server.URL, "--api", "test-token", "--client", "default", "--dry-run"})
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
