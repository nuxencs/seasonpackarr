// Copyright (c) 2023 - 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExecute_ReportsFailures(t *testing.T) {
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

func TestExecute_RunsLocalToolsAndHelp(t *testing.T) {
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

// TestExecute_OnlyStartCreatesConfigFile: start creates a missing config.yaml in its --config directory, and no
// other command and no start without --config creates a file. The tests pass the environment through execute.
func TestExecute_OnlyStartCreatesConfigFile(t *testing.T) {
	for _, args := range [][]string{
		{"notification", "test"},
		{"candidate", "Series.S01"},
		{"search", "--dry-run"},
		{"setup", "filter", "--webhook-url", "http://127.0.0.1:42069", "--client", "tv", "--dry-run"},
	} {
		t.Run(strings.Join(args[:min(2, len(args))], " "), func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), "config")
			var stdout, stderr bytes.Buffer

			code := execute(t.Context(), append(args, "--config", dir), nil, &stdout, &stderr)

			assert.Equal(t, 1, code, stderr.String())
			assert.NoDirExists(t, dir)
		})
	}

	t.Run("start without --config", func(t *testing.T) {
		workingDir := t.TempDir()
		t.Chdir(workingDir)
		var stdout, stderr bytes.Buffer

		code := execute(t.Context(), []string{"start"}, []string{"HOME=" + t.TempDir()}, &stdout, &stderr)

		assert.Equal(t, 1, code)
		assert.Contains(t, stderr.String(), "could not find config file")
		entries, err := os.ReadDir(workingDir)
		require.NoError(t, err)
		assert.Empty(t, entries)
	})

	t.Run("start --config", func(t *testing.T) {
		dir := filepath.Join(t.TempDir(), "config")
		var stdout, stderr bytes.Buffer

		code := execute(t.Context(), []string{"start", "--config", dir}, nil, &stdout, &stderr)

		// The default client of the new file has no import category, so the load stops start after the write.
		assert.Equal(t, 1, code)
		assert.Contains(t, stderr.String(), `client "default": must configure import.category or import.savePath`)
		want, err := os.ReadFile("../config.yaml")
		require.NoError(t, err)
		got, err := os.ReadFile(filepath.Join(dir, "config.yaml"))
		require.NoError(t, err)
		assert.Equal(t, string(want), string(got))
	})
}

// TestGetenv_MatchesNamesAsTheSystemDoes: Windows environment names have no case, so a lookup there ignores it.
func TestGetenv_MatchesNamesAsTheSystemDoes(t *testing.T) {
	environ := []string{"Xdg_Data_Home=mixed", "XDG_DATA_HOME=exact"}
	want := "exact"
	if runtime.GOOS == "windows" {
		want = "mixed"
	}

	assert.Equal(t, want, getenv(environ, "XDG_DATA_HOME"))
	assert.Empty(t, getenv(environ, "XDG_CONFIG_HOME"))
}
