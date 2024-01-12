// Copyright (c) 2023 - 2024, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package utils

import (
	"os"
	"path/filepath"
)

func CreateHardlink(srcPath, trgPath string) error {
	trgDir := filepath.Dir(trgPath)

	// create the target directory if it doesn't exist
	if err := os.MkdirAll(trgDir, 0755); err != nil {
		return err
	}

	// link source path to target path if it doesn't exist
	if err := os.Link(srcPath, trgPath); err != nil {
		return err
	}

	return nil
}
