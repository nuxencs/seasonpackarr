package utils

import (
	"fmt"
	"os"
	"path/filepath"
)

func CreateHardlink(srcPath, trgPath string) error {
	// create the target directory if it doesn't exist
	trgDir := filepath.Dir(trgPath)
	err := os.MkdirAll(trgDir, 0755)
	if err != nil {
		return err
	}

	if _, err = os.Stat(trgPath); os.IsNotExist(err) {
		// target file does not exist, create a hardlink
		err = os.Link(srcPath, trgPath)
		if err != nil {
			return err
		}
	} else {
		return fmt.Errorf("file already exist")
	}
	return nil
}
