// Copyright (c) 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReadConnectionSettings_ReadOnlyProjection(t *testing.T) {
	dir := t.TempDir()
	content := []byte("host: 0.0.0.0\nport: 4242\napiToken: secret\nclients:\n  tv: {}\nsearch: not-valid-server-settings\n")
	path := filepath.Join(dir, "config.yaml")
	require.NoError(t, os.WriteFile(path, content, 0o600))
	settings, err := ReadConnectionSettings(Input{ConfigDir: dir})
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
	dir := filepath.Join(t.TempDir(), "missing")
	_, err := ReadConnectionSettings(Input{ConfigDir: dir})
	require.ErrorContains(t, err, "could not read config")
	_, err = os.Stat(dir)
	require.True(t, os.IsNotExist(err))
}

func TestReadConnectionSettings_DefaultDiscovery(t *testing.T) {
	t.Chdir(t.TempDir())
	require.NoError(t, os.WriteFile("config.yaml", []byte("apiToken: local-token\nclients:\n  only: {}\n"), 0o600))
	settings, err := ReadConnectionSettings(Input{})
	require.NoError(t, err)
	require.Equal(t, "local-token", settings.APIToken)
	require.Equal(t, "42069", settings.Port)
	require.Equal(t, []string{"only"}, settings.Clients)
}

func TestReadConnectionSettings_Environment(t *testing.T) {
	settings, err := ReadConnectionSettings(Input{ConfigDir: filepath.Join(t.TempDir(), "ignored"), Environ: []string{
		"SEASONPACKARR__DISABLE_CONFIG_FILE=true",
		"SEASONPACKARR__HOST=::",
		"SEASONPACKARR__PORT=4243",
		"SEASONPACKARR__API_TOKEN=environment-token",
		"SEASONPACKARR__CLIENTS_TV_HOST=127.0.0.1",
	}})
	require.NoError(t, err)
	require.Equal(t, ConnectionSettings{Host: "::", Port: "4243", APIToken: "environment-token", Clients: []string{"tv"}}, settings)
	for _, value := range []string{"abc", "0", "65536"} {
		settings, err := ReadConnectionSettings(Input{Environ: []string{"SEASONPACKARR__DISABLE_CONFIG_FILE=true", "SEASONPACKARR__PORT=" + value}})
		require.NoError(t, err)
		require.Equal(t, value, settings.Port)
	}
}

func TestReadConnectionSettings_ParseErrorsDoNotExposeValues(t *testing.T) {
	for _, content := range []string{"apiToken: [secret", "port: secret"} {
		path := writeTestConfig(t, content)
		_, err := ReadConnectionSettings(Input{ConfigDir: filepath.Dir(path)})
		require.Error(t, err)
		require.NotContains(t, err.Error(), "secret")
	}
}

// TestReadConnectionSettings_AllowsUnknownKeys keeps the CLI usable against a
// config from a different version, which the service load rejects.
func TestReadConnectionSettings_AllowsUnknownKeys(t *testing.T) {
	path := writeTestConfig(t, "apiToken: token\nfutureKey: true\nclients:\n  tv:\n    futureClientKey: 1\n")
	settings, err := ReadConnectionSettings(Input{ConfigDir: filepath.Dir(path)})
	require.NoError(t, err)
	assert.Equal(t, "token", settings.APIToken)
	assert.Equal(t, []string{"tv"}, settings.Clients)
}

// TestReadConnectionSettings_ChecksOnlyConnectionVariables keeps the CLI usable
// when a server-only variable is bad, and stops it when a variable it reads is bad.
func TestReadConnectionSettings_ChecksOnlyConnectionVariables(t *testing.T) {
	for variable, value := range map[string]string{
		"SEASONPACKARR__SMART_MODE":             "yes",
		"SEASONPACKARR__LOG_MAX_SIZE":           "big",
		"SEASONPACKARR__CLIENTS_TV_PORT":        "abc",
		"SEASONPACKARR__NOTIFICATIONS_SHOUTRRR": " ",
	} {
		t.Run("server-only "+variable, func(t *testing.T) {
			settings, err := ReadConnectionSettings(Input{Environ: []string{"SEASONPACKARR__DISABLE_CONFIG_FILE=true", variable + "=" + value}})
			require.NoError(t, err)
			assert.Equal(t, "42069", settings.Port)
		})
	}
	for _, variable := range []string{"SEASONPACKARR__HOST", "SEASONPACKARR__API_TOKEN"} {
		t.Run("connection "+variable, func(t *testing.T) {
			_, err := ReadConnectionSettings(Input{Environ: []string{"SEASONPACKARR__DISABLE_CONFIG_FILE=true", variable + "=sentinel-connection-value\n"}})
			require.Error(t, err)
			assert.Contains(t, err.Error(), variable)
			assert.NotContains(t, err.Error(), "sentinel")
		})
	}
}

func TestReadConnectionSettings_IgnoresLegacyConfigFile(t *testing.T) {
	dir := filepath.Dir(writeTestConfig(t, "apiToken: token\n"))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "config.toml"), nil, 0o600))

	settings, err := ReadConnectionSettings(Input{ConfigDir: dir})

	require.NoError(t, err)
	assert.Equal(t, "token", settings.APIToken)
}

func TestReadConnectionSettings_RejectsCreateFile(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "config")

	_, err := ReadConnectionSettings(Input{ConfigDir: dir, CreateFile: true})

	require.ErrorIs(t, err, errCreateFileOnlyNew)
	assert.NoDirExists(t, dir)
}

// TestReadConnectionSettings_SharesSourceOfFullLoad: both reads resolve the file from the passed environment, so they cannot disagree.
func TestReadConnectionSettings_SharesSourceOfFullLoad(t *testing.T) {
	home := t.TempDir()
	configFile := filepath.Join(home, ".config", "seasonpackarr", "config.yaml")
	require.NoError(t, os.MkdirAll(filepath.Dir(configFile), 0o755))
	writeConfigFile(t, configFile, "apiToken: home-token\nclients: {}\n")
	t.Chdir(t.TempDir())
	in := Input{Environ: []string{"HOME=" + home}}

	settings, err := ReadConnectionSettings(in)
	require.NoError(t, err)
	cfg, err := Load(in)
	require.NoError(t, err)

	assert.Equal(t, "home-token", settings.APIToken)
	assert.Equal(t, "home-token", cfg.APIToken)
	assert.Equal(t, filepath.Dir(configFile), cfg.ConfigPath)
}
