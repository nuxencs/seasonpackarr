// Copyright (c) 2023 - 2025, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package files

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCreateHardlinkAcceptsExistingLinkToSource(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "source.mkv")
	target := filepath.Join(dir, "pack", "target.mkv")
	require.NoError(t, os.WriteFile(source, []byte("episode"), 0o644))

	require.NoError(t, CreateHardlink(source, target))
	require.NoError(t, CreateHardlink(source, target))

	sourceInfo, err := os.Stat(source)
	require.NoError(t, err)
	targetInfo, err := os.Stat(target)
	require.NoError(t, err)
	require.True(t, os.SameFile(sourceInfo, targetInfo))
}

func TestCreateHardlinkRejectsConflictingTarget(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "source.mkv")
	target := filepath.Join(dir, "target.mkv")
	require.NoError(t, os.WriteFile(source, []byte("source"), 0o644))
	require.NoError(t, os.WriteFile(target, []byte("different"), 0o644))

	require.Error(t, CreateHardlink(source, target))
}
