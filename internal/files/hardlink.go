// Copyright (c) 2023 - 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package files

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/nuxencs/seasonpackarr/internal/errtrace"
)

func CreateHardlink(sourcePath, targetPath string) error {
	targetDirectory := filepath.Dir(targetPath)

	// create the target directory if it doesn't exist
	if err := os.MkdirAll(targetDirectory, 0o755); err != nil {
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
