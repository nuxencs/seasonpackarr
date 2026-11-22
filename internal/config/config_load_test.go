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

func TestNew_ReturnsLoadError(t *testing.T) {
	clearConnectionEnvironment(t)
	dir := filepath.Dir(writeTestConfig(t, "port: not-a-port\n"))

	cfg, err := New(dir, "test")

	require.ErrorContains(t, err, "decoding config file "+filepath.Join(dir, "config.yaml"))
	assert.Nil(t, cfg)
}

func TestNew_ReturnsWriteError(t *testing.T) {
	clearConnectionEnvironment(t)
	// A file where the config directory must be stops the creation of config.yaml.
	dir := filepath.Join(t.TempDir(), "file")
	require.NoError(t, os.WriteFile(dir, nil, 0o600))

	cfg, err := New(filepath.Join(dir, "config"), "test")

	require.ErrorContains(t, err, "writing config file")
	assert.Nil(t, cfg)
}

// writeLegacyConfigDir returns a config directory with a valid config.yaml and a legacy config.toml.
func writeLegacyConfigDir(t *testing.T) string {
	t.Helper()
	dir := filepath.Dir(writeTestConfig(t, "clients: {}\n"))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "config.toml"), nil, 0o600))
	return dir
}

func TestNew_RejectsLegacyConfigFile(t *testing.T) {
	clearConnectionEnvironment(t)

	cfg, err := New(writeLegacyConfigDir(t), "test")

	require.ErrorContains(t, err, "A legacy 'config.toml' file has been detected.")
	assert.ErrorContains(t, err, "renaming 'config.toml' to 'config.toml.old'")
	assert.Nil(t, cfg)
}

func TestLoad_RejectsLegacyConfigFile(t *testing.T) {
	clearConnectionEnvironment(t)

	_, err := Load(writeLegacyConfigDir(t), "test")

	require.ErrorContains(t, err, "A legacy 'config.toml' file has been detected.")
	assert.ErrorContains(t, err, "renaming 'config.toml' to 'config.toml.old'")
}
