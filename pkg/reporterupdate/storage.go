package reporterupdate

import (
	"errors"
	"os"
	"path/filepath"

	"github.com/QuantumNous/new-api/common"
)

func WritePrivate(path string, data []byte) error {
	if err := SecureDir(filepath.Dir(path)); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".update-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if err := f.Chmod(0o600); err != nil {
		f.Close()
		return err
	}
	if _, err = f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err = f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	if err = os.Rename(f.Name(), path); err != nil {
		return err
	}
	return syncDirectory(filepath.Dir(path))
}
func Save(path string, value any) error {
	data, err := common.Marshal(value)
	if err != nil {
		return err
	}
	return WritePrivate(path, data)
}
func Load(path string, value any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if len(data) > 4<<20 {
		return errors.New("metadata_too_large")
	}
	return common.Unmarshal(data, value)
}
func SecureDir(path string) error {
	if err := os.MkdirAll(path, 0o700); err != nil {
		return err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("unsafe_directory")
	}
	return protectDirectory(path)
}
