// Copyright (c) 2023 - 2025, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package files

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCreateHardlink_AcceptsExistingLinkToSource(t *testing.T) {
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

func TestCreateHardlink_RejectsConflictingTarget(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "source.mkv")
	target := filepath.Join(dir, "target.mkv")
	require.NoError(t, os.WriteFile(source, []byte("source"), 0o644))
	require.NoError(t, os.WriteFile(target, []byte("different"), 0o644))

	require.Error(t, CreateHardlink(source, target))
}

func TestCreateHardlink_PreservesFilesystemErrorIdentity(t *testing.T) {
	dir := t.TempDir()
	err := CreateHardlink(filepath.Join(dir, "missing.mkv"), filepath.Join(dir, "pack", "target.mkv"))

	require.ErrorIs(t, err, fs.ErrNotExist)
	_, ok := errors.AsType[*os.LinkError](err)
	require.True(t, ok)
}
