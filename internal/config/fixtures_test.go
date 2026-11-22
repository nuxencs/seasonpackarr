// Copyright (c) 2023 - 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/nuxencs/seasonpackarr/internal/domain"

	"github.com/stretchr/testify/require"
)

func writeTestConfig(t *testing.T, content string) string {
	t.Helper()

	configFile := filepath.Join(t.TempDir(), "config.yaml")
	require.NoError(t, os.WriteFile(configFile, []byte(content), 0o644))

	return configFile
}

// testInput returns the input of a load of a new config.yaml with contents, with environ as the whole environment.
func testInput(t *testing.T, contents string, environ ...string) Input {
	t.Helper()
	return Input{ConfigDir: filepath.Dir(writeTestConfig(t, contents)), Environ: environ, Version: "test"}
}

// loadFile loads configFile with environ as the whole environment.
func loadFile(configFile string, environ ...string) (domain.Config, error) {
	return Load(Input{ConfigDir: filepath.Dir(configFile), Environ: environ, Version: "test"})
}

// loadWithFile loads a new config.yaml with contents, with environ as the whole environment.
func loadWithFile(t *testing.T, contents string, environ ...string) (domain.Config, error) {
	t.Helper()
	return Load(testInput(t, contents, environ...))
}

// newTestAppConfig returns the holder of a new config.yaml with contents and the path of that file.
func newTestAppConfig(t *testing.T, contents string, environ ...string) (*AppConfig, string) {
	t.Helper()
	in := testInput(t, contents, environ...)
	cfg, err := New(in)
	require.NoError(t, err)
	return cfg, filepath.Join(in.ConfigDir, "config.yaml")
}

func writeConfigFile(t testing.TB, configFile, contents string) {
	t.Helper()
	require.NoError(t, os.WriteFile(configFile, []byte(contents), 0o644))
}
