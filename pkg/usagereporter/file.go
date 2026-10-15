package usagereporter

import (
	"os"
	"path/filepath"
)

// writePrivateFile atomically replaces a private file after syncing its data.
// An interrupted write leaves the previous config/event intact.
func writePrivateFile(path string, content []byte) error {
	file, err := os.CreateTemp(filepath.Dir(path), ".usage-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	if _, err := file.Write(content); err != nil {
		file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return os.Rename(file.Name(), path)
}
