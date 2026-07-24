// Copyright (c) 2023 - 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package files

import (
	"os"
	"path/filepath"
)

func CreateHardlink(sourcePath, targetPath string) error {
	targetDirectory := filepath.Dir(targetPath)

	// create the target directory if it doesn't exist
	if err := os.MkdirAll(targetDirectory, 0o755); err != nil {
		return err
	}

	// link source path to target path
	if err := os.Link(sourcePath, targetPath); err != nil {
		return err
	}

	return nil
}
