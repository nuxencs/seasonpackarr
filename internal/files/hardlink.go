// Copyright (c) 2023 - 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package files

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/nuxencs/seasonpackarr/internal/errtrace"
)

// CreateHardlink links sourcePath to targetPath, which must be below root.
// The target path comes from torrent metadata. os.Root makes the target
// folders, because it rejects parent elements and symlinks that leave root.
func CreateHardlink(root, sourcePath, targetPath string) error {
	relTarget, err := filepath.Rel(root, targetPath)
	if err != nil || !filepath.IsLocal(relTarget) {
		return errtrace.WithStack(fmt.Errorf("hardlink target %q is not below %q", targetPath, root))
	}

	// The operator sets the root, so it can be new and can be a symlink.
	if err := os.MkdirAll(root, 0o755); err != nil {
		return errtrace.WithStack(err)
	}
	rootDir, err := os.OpenRoot(root)
	if err != nil {
		return errtrace.WithStack(err)
	}
	defer func() { _ = rootDir.Close() }()
	if err := rootDir.MkdirAll(filepath.Dir(relTarget), 0o755); err != nil {
		return errtrace.WithStack(err)
	}

	// Link the source to the target. A prior successful attempt is also success,
	// but an unrelated file at the target remains a conflict.
	if err := os.Link(sourcePath, targetPath); err != nil {
		if !errors.Is(err, fs.ErrExist) {
			return errtrace.WithStack(err)
		}

		sourceInfo, sourceErr := os.Stat(sourcePath)
		if sourceErr != nil {
			return errtrace.WithStack(sourceErr)
		}
		targetInfo, targetErr := os.Stat(targetPath)
		if targetErr != nil {
			return errtrace.WithStack(targetErr)
		}
		if !os.SameFile(sourceInfo, targetInfo) {
			return errtrace.WithStack(err)
		}
	}

	return nil
}
