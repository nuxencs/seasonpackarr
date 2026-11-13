// Copyright (c) 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func clearConnectionEnvironment(t *testing.T) {
	t.Helper()
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		if strings.HasPrefix(key, "SEASONPACKARR__") {
			t.Setenv(key, "")
		}
	}
}

func TestReadConnectionSettings_ReadOnlyProjection(t *testing.T) {
	clearConnectionEnvironment(t)
	dir := t.TempDir()
	content := []byte("host: 0.0.0.0\nport: 4242\napiToken: secret\nclients:\n  tv: {}\nsearch: not-valid-server-settings\n")
	path := filepath.Join(dir, "config.yaml")
	require.NoError(t, os.WriteFile(path, content, 0o600))
	settings, err := ReadConnectionSettings(dir)
	require.NoError(t, err)
	require.Equal(t, ConnectionSettings{Host: "0.0.0.0", Port: "4242", APIToken: "secret", Clients: []string{"tv"}}, settings)
	after, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, content, after)
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	require.Len(t, entries, 1)
}

func TestReadConnectionSettings_MissingExplicitConfig(t *testing.T) {
	clearConnectionEnvironment(t)
	dir := filepath.Join(t.TempDir(), "missing")
	_, err := ReadConnectionSettings(dir)
	require.ErrorContains(t, err, "could not read config")
	_, err = os.Stat(dir)
	require.True(t, os.IsNotExist(err))
}

func TestReadConnectionSettings_DefaultDiscovery(t *testing.T) {
	clearConnectionEnvironment(t)
	t.Chdir(t.TempDir())
	require.NoError(t, os.WriteFile("config.yaml", []byte("apiToken: local-token\nclients:\n  only: {}\n"), 0o600))
	settings, err := ReadConnectionSettings("")
	require.NoError(t, err)
	require.Equal(t, "local-token", settings.APIToken)
	require.Equal(t, "42069", settings.Port)
	require.Equal(t, []string{"only"}, settings.Clients)
}

func TestReadConnectionSettings_Environment(t *testing.T) {
	clearConnectionEnvironment(t)
	t.Setenv("SEASONPACKARR__DISABLE_CONFIG_FILE", "true")
	t.Setenv("SEASONPACKARR__HOST", "::")
	t.Setenv("SEASONPACKARR__PORT", "4243")
	t.Setenv("SEASONPACKARR__API_TOKEN", "environment-token")
	t.Setenv("SEASONPACKARR__CLIENTS_TV_HOST", "127.0.0.1")
	settings, err := ReadConnectionSettings(filepath.Join(t.TempDir(), "ignored"))
	require.NoError(t, err)
	require.Equal(t, ConnectionSettings{Host: "::", Port: "4243", APIToken: "environment-token", Clients: []string{"tv"}}, settings)
	for _, value := range []string{"abc", "0", "65536"} {
		t.Setenv("SEASONPACKARR__PORT", value)
		settings, err := ReadConnectionSettings("")
		require.NoError(t, err)
		require.Equal(t, value, settings.Port)
	}
}

func TestReadConnectionSettings_ParseErrorsDoNotExposeValues(t *testing.T) {
	clearConnectionEnvironment(t)
	for _, content := range []string{"apiToken: [secret", "port: secret"} {
		path := writeTestConfig(t, content)
		_, err := ReadConnectionSettings(filepath.Dir(path))
		require.Error(t, err)
		require.NotContains(t, err.Error(), "secret")
	}
}
