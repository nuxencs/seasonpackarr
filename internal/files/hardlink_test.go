// Copyright (c) 2023 - 2025, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package files

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCreateHardlink_AcceptsExistingLinkToSource(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "source.mkv")
	target := filepath.Join(dir, "pack", "target.mkv")
	require.NoError(t, os.WriteFile(source, []byte("episode"), 0o644))

	require.NoError(t, CreateHardlink(dir, source, target))
	require.NoError(t, CreateHardlink(dir, source, target))

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

	require.Error(t, CreateHardlink(dir, source, target))
}

func TestCreateHardlink_PreservesFilesystemErrorIdentity(t *testing.T) {
	dir := t.TempDir()
	err := CreateHardlink(dir, filepath.Join(dir, "missing.mkv"), filepath.Join(dir, "pack", "target.mkv"))

	require.ErrorIs(t, err, fs.ErrNotExist)
	_, ok := errors.AsType[*os.LinkError](err)
	require.True(t, ok)
}

// TestCreateHardlink_RejectsTargetOutsideRoot: target paths come from torrent
// metadata, so a target must stay below the import root (issue #290).
func TestCreateHardlink_RejectsTargetOutsideRoot(t *testing.T) {
	newDirs := func(t *testing.T) (root, outside, source string) {
		t.Helper()
		dir := t.TempDir()
		root = filepath.Join(dir, "import")
		outside = filepath.Join(dir, "outside")
		source = filepath.Join(dir, "source.mkv")
		require.NoError(t, os.Mkdir(root, 0o755))
		require.NoError(t, os.Mkdir(outside, 0o755))
		require.NoError(t, os.WriteFile(source, []byte("episode"), 0o644))
		return root, outside, source
	}

	t.Run("parent elements", func(t *testing.T) {
		root, outside, source := newDirs(t)

		err := CreateHardlink(root, source, filepath.Join(root, "pack", "..", "..", "outside", "target.mkv"))

		assert.Error(t, err)
		assert.NoFileExists(t, filepath.Join(outside, "target.mkv"))
	})

	t.Run("new folder below the parent of the root", func(t *testing.T) {
		root, _, source := newDirs(t)

		err := CreateHardlink(root, source, filepath.Join(root, "..", "created", "target.mkv"))

		assert.Error(t, err)
		assert.NoDirExists(t, filepath.Join(filepath.Dir(root), "created"))
	})

	t.Run("symlinked pack folder", func(t *testing.T) {
		root, outside, source := newDirs(t)
		require.NoError(t, os.Symlink(outside, filepath.Join(root, "pack")))

		err := CreateHardlink(root, source, filepath.Join(root, "pack", "target.mkv"))

		assert.Error(t, err)
		assert.NoFileExists(t, filepath.Join(outside, "target.mkv"))
	})

	t.Run("relative symlink that stays below the root", func(t *testing.T) {
		root, _, source := newDirs(t)
		require.NoError(t, os.Mkdir(filepath.Join(root, "real"), 0o755))
		require.NoError(t, os.Symlink("real", filepath.Join(root, "pack")))

		require.NoError(t, CreateHardlink(root, source, filepath.Join(root, "pack", "target.mkv")))

		require.FileExists(t, filepath.Join(root, "real", "target.mkv"))
	})
}
