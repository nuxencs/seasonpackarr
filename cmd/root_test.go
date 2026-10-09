// Copyright (c) 2023 - 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package cmd

import (
	"testing"

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
