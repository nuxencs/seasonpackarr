// Copyright (c) 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

//go:build unix

package placement

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A cached import plan can name a file of a source folder that became a named pipe. An open of a pipe with no writer
// waits, and the import holds the endpoint import lock, so the link must fail without an open.
func TestLink_RefusesSourceFolderFileThatBecameNamedPipe(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "import")
	folder := filepath.Join(dir, "library")
	require.NoError(t, os.MkdirAll(folder, 0o755))
	source := filepath.Join(folder, episode1)
	require.NoError(t, syscall.Mkfifo(source, 0o644))

	outcomes := New(newPackInfo(t), PackFolderDestination(root)).Link([]PlannedLink{
		{Source: source, TorrentPath: episode1, SourceFolder: folder},
	})

	require.Len(t, outcomes, 1)
	assert.Equal(t, Failed, outcomes[0].Status)
	assert.ErrorContains(t, outcomes[0].Err, "is not a regular file")
	assert.NoFileExists(t, filepath.Join(root, packName, episode1))
}
