// Copyright (c) 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package cmd

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestConnection_ConfigAndFlagPrecedence(t *testing.T) {
	isolateCLI(t)
	t.Setenv("SEASONPACKARR__DISABLE_CONFIG_FILE", "")
	var gotClient, gotToken string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct{ ClientName string }
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		gotClient, gotToken = body.ClientName, r.Header.Get("X-API-Token")
		w.WriteHeader(250)
	}))
	defer server.Close()
	u, err := url.Parse(server.URL)
	require.NoError(t, err)
	_, port, err := net.SplitHostPort(u.Host)
	require.NoError(t, err)
	configDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "config.yaml"), fmt.Appendf(nil, "host: 0.0.0.0\nport: %s\napiToken: config-token\nclients:\n  tv: {}\n", port), 0o600))
	code, _, stderr := runCLI(t, "candidate", "Series.S01", "--config", configDir)
	require.Equal(t, 0, code, stderr)
	require.Equal(t, "tv", gotClient)
	require.Equal(t, "config-token", gotToken)

	t.Setenv("SEASONPACKARR__API_TOKEN", "env-token")
	t.Setenv("SEASONPACKARR__CLIENT", "env-client")
	t.Setenv("SEASONPACKARR__URL", server.URL)
	code, _, stderr = runCLI(t, "candidate", "Series.S01", "--config", configDir)
	require.Equal(t, 0, code, stderr)
	require.Equal(t, "env-client", gotClient)
	require.Equal(t, "env-token", gotToken)

	code, stdout, stderr := runCLI(t, "candidate", "Series.S01", "--config", configDir, "--client", "flag-client", "--api", "flag-token", "--json")
	require.Equal(t, 0, code, stderr)
	require.True(t, json.Valid([]byte(stdout)))
	require.Equal(t, "flag-client", gotClient)
	require.Equal(t, "flag-token", gotToken)
	require.Empty(t, stderr)

	// Explicit empty authentication overrides config and environment authentication.
	code, _, stderr = runCLI(t, "candidate", "Series.S01", "--config", configDir, "--api", "")
	require.Equal(t, 0, code, stderr)
	require.Empty(t, gotToken)

	// Explicit URL wins over environment URL; host/port flags do as well.
	t.Setenv("SEASONPACKARR__URL", "http://127.0.0.1:1")
	for _, flags := range [][]string{{"--url", server.URL}, {"-i", "127.0.0.1", "-p", port}} {
		args := append([]string{"candidate", "Series.S01", "--config", configDir}, flags...)
		code, _, stderr = runCLI(t, args...)
		require.Equal(t, 0, code, stderr)
	}
}

func TestConnection_MultipleClientsAndSearchSelection(t *testing.T) {
	isolateCLI(t)
	t.Setenv("SEASONPACKARR__DISABLE_CONFIG_FILE", "")
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "config.yaml"), []byte("clients:\n  zeta: {}\n  alpha: {}\n"), 0o600))
	code, _, stderr := runCLI(t, "candidate", "Series.S01", "--config", dir)
	require.Equal(t, 1, code)
	require.Contains(t, stderr, "--client")
	require.Contains(t, stderr, "alpha, zeta")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct{ ClientName string }
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		require.Empty(t, body.ClientName)
		fmt.Fprint(w, `{"dryRun":true,"outcomes":[],"failures":[]}`)
	}))
	defer server.Close()
	code, stdout, stderr := runCLI(t, "search", "--config", dir, "--url", server.URL, "--dry-run")
	require.Equal(t, 0, code, stderr)
	require.Contains(t, stdout, "No season-pack results.")
}

func TestConnection_InvalidAddresses(t *testing.T) {
	isolateCLI(t)
	for _, flags := range [][]string{
		{"--url", "https://user:secret@example.com"},
		{"--url", "https://example.com?apikey=secret"},
		{"--url", "https://example.com#fragment"},
		{"--url", "ftp://example.com"},
		{"--url", "http://example.com:99999"},
		{"--url", ""},
		{"--port", "0"},
		{"--port", "65536"},
		{"--host", "https://example.com"},
		{"--url", "https://example.com", "--host", "127.0.0.1"},
		{"--url", "https://example.com", "--port", "1234"},
	} {
		t.Run(strings.Join(flags, " "), func(t *testing.T) {
			code, _, stderr := runCLI(t, append([]string{"candidate", "Series.S01"}, flags...)...)
			require.Equal(t, 1, code)
			require.NotContains(t, stderr, "secret")
		})
	}
}

func TestConnection_ValidatesOnlyEffectivePort(t *testing.T) {
	isolateCLI(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(250)
	}))
	defer server.Close()
	u, err := url.Parse(server.URL)
	require.NoError(t, err)
	for _, value := range []string{"invalid", "0", "65536", "999999999999999999999999"} {
		t.Run(value, func(t *testing.T) {
			t.Setenv("SEASONPACKARR__PORT", value)
			code, stdout, stderr := runCLI(t, "candidate", "Series.S01")
			require.Equal(t, 1, code)
			require.Empty(t, stdout)
			require.Contains(t, stderr, "port must be an integer from 1 to 65535")
			for _, flags := range [][]string{{"--url", server.URL}, {"--port", u.Port()}} {
				code, _, stderr = runCLI(t, append([]string{"candidate", "Series.S01"}, flags...)...)
				require.Equal(t, 0, code, stderr)
			}
			t.Setenv("SEASONPACKARR__URL", server.URL)
			code, _, stderr = runCLI(t, "candidate", "Series.S01")
			require.Equal(t, 0, code, stderr)
			// An explicit host selects host/port again, so the invalid port matters.
			code, _, stderr = runCLI(t, "candidate", "Series.S01", "--host", "127.0.0.1")
			require.Equal(t, 1, code)
			require.Contains(t, stderr, "port must be an integer from 1 to 65535")
		})
	}
}
