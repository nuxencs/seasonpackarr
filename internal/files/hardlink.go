// Copyright (c) 2023 - 2025, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package files

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
)

func CreateHardlink(sourcePath, targetPath string) error {
	targetDirectory := filepath.Dir(targetPath)

	// create the target directory if it doesn't exist
	if err := os.MkdirAll(targetDirectory, 0o755); err != nil {
		return err
	}

	// Link the source to the target. A prior successful attempt is also success,
	// but an unrelated file at the target remains a conflict.
	if err := os.Link(sourcePath, targetPath); err != nil {
		if !errors.Is(err, fs.ErrExist) {
			return err
		}

		sourceInfo, sourceErr := os.Stat(sourcePath)
		if sourceErr != nil {
			return sourceErr
		}
		targetInfo, targetErr := os.Stat(targetPath)
		if targetErr != nil {
			return targetErr
		}
		if !os.SameFile(sourceInfo, targetInfo) {
			return err
		}
	}

	return nil
}
