package utils

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"seasonpackarr/pkg/errors"
)

func CreateHardlink(srcPath, trgPath string) error {
	// create the target directory if it doesn't exist
	trgDir := filepath.Dir(trgPath)
	err := os.MkdirAll(trgDir, 0755)
	if err != nil {
		return err
	}

	if _, err = os.Stat(trgPath); err == nil {
		// target file exist
		return fmt.Errorf("file already exist")
	} else if errors.Is(err, fs.ErrNotExist) {
		// target file does not exist, create a hardlink
		err = os.Link(srcPath, trgPath)
		if err != nil {
			return err
		}
	} else {
		// any other error
		return err
	}
	return nil
}
