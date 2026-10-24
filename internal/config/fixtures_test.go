// Copyright (c) 2023 - 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func writeTestConfig(t *testing.T, content string) string {
	t.Helper()

	configFile := filepath.Join(t.TempDir(), "config.yaml")
	require.NoError(t, os.WriteFile(configFile, []byte(content), 0o644))

	return configFile
}

func newTestAppConfig(t *testing.T, contents string) (*AppConfig, string) {
	t.Helper()
	configFile := writeTestConfig(t, contents)
	cfg := &AppConfig{configFile: configFile, version: "test"}
	snapshot, err := cfg.loadSnapshot()
	require.NoError(t, err)
	cfg.current.Store(snapshot)
	return cfg, configFile
}

func writeConfigFile(t *testing.T, configFile, contents string) {
	t.Helper()
	require.NoError(t, os.WriteFile(configFile, []byte(contents), 0o644))
}
