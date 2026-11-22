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
	dir := filepath.Dir(writeTestConfig(t, "port: not-a-port\n"))

	cfg, err := New(Input{ConfigDir: dir, Version: "test"})

	require.ErrorContains(t, err, "decoding config file "+filepath.Join(dir, "config.yaml"))
	assert.Nil(t, cfg)
}

func TestNew_ReturnsWriteError(t *testing.T) {
	// A file where the config directory must be stops the creation of config.yaml.
	dir := filepath.Join(t.TempDir(), "file")
	require.NoError(t, os.WriteFile(dir, nil, 0o600))

	cfg, err := New(Input{ConfigDir: filepath.Join(dir, "config"), Version: "test", CreateFile: true})

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
	cfg, err := New(Input{ConfigDir: writeLegacyConfigDir(t), Version: "test"})

	require.ErrorContains(t, err, "A legacy 'config.toml' file has been detected.")
	assert.ErrorContains(t, err, "renaming 'config.toml' to 'config.toml.old'")
	assert.Nil(t, cfg)
}

func TestLoad_RejectsLegacyConfigFile(t *testing.T) {
	_, err := Load(Input{ConfigDir: writeLegacyConfigDir(t), Version: "test"})

	require.ErrorContains(t, err, "A legacy 'config.toml' file has been detected.")
	assert.ErrorContains(t, err, "renaming 'config.toml' to 'config.toml.old'")
}

func TestNew_CreatesNoFileWithoutCreateFile(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "config")

	cfg, err := New(Input{ConfigDir: dir, Version: "test"})

	require.ErrorContains(t, err, "reading config file "+filepath.Join(dir, "config.yaml"))
	assert.Nil(t, cfg)
	assert.NoDirExists(t, dir)
}

func TestNew_CreateFileNeedsConfigDir(t *testing.T) {
	workingDir := t.TempDir()
	t.Chdir(workingDir)

	cfg, err := New(Input{Version: "test", CreateFile: true})

	require.ErrorContains(t, err, "CreateFile needs a ConfigDir")
	assert.Nil(t, cfg)
	entries, err := os.ReadDir(workingDir)
	require.NoError(t, err)
	assert.Empty(t, entries)
}

func TestNew_CreatesNoFileWhenConfigFileIsDisabled(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "config")

	cfg, err := New(Input{ConfigDir: dir, Environ: []string{"SEASONPACKARR__DISABLE_CONFIG_FILE=true"}, Version: "test", CreateFile: true})

	require.NoError(t, err)
	assert.Equal(t, 42069, cfg.Snapshot().Port)
	assert.NoDirExists(t, dir)
}

func TestLoad_RejectsCreateFile(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "config")

	_, err := Load(Input{ConfigDir: dir, Version: "test", CreateFile: true})

	require.ErrorIs(t, err, errCreateFileOnlyNew)
	assert.NoDirExists(t, dir)
}

func TestLoad_ReadsNoFileWhenConfigFileIsDisabled(t *testing.T) {
	dir := filepath.Dir(writeTestConfig(t, "port: not-a-port\n"))

	cfg, err := Load(Input{ConfigDir: dir, Environ: []string{"SEASONPACKARR__DISABLE_CONFIG_FILE=true"}, Version: "test"})

	require.NoError(t, err)
	assert.Equal(t, 42069, cfg.Port)
}

// TestNew_AcceptsEmptyFileBecauseNoPreviousSnapshotExists: at start, an empty file is a valid config of all defaults.
// TestReload_RejectsEmptyFileBecauseItIsAWriteInProgress is the other half of this rule.
func TestNew_AcceptsEmptyFileBecauseNoPreviousSnapshotExists(t *testing.T) {
	cfg, _ := newTestAppConfig(t, " \n")

	assert.Equal(t, 42069, cfg.Snapshot().Port)
}

// TestReload_RejectsEmptyFileBecauseItIsAWriteInProgress: on a reload, an empty file is almost always a write in
// progress, so the reload keeps the previous snapshot and does not reset the config to the defaults.
func TestReload_RejectsEmptyFileBecauseItIsAWriteInProgress(t *testing.T) {
	cfg, configFile := newTestAppConfig(t, "port: 4242\n")

	writeConfigFile(t, configFile, " \n")
	_, err := cfg.reload()

	require.ErrorContains(t, err, "is empty")
	assert.Equal(t, 4242, cfg.Snapshot().Port)
}
