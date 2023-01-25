package osfs

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
)

type FS struct {
	bp string
}

func NewFS() *FS                    { return &FS{} }
func NewFSWithBase(path string) *FS { return &FS{bp: path} }

func (f *FS) CreateDir(path string) error {
	return os.MkdirAll(filepath.Join(f.bp, path), os.ModePerm)
}

func (f *FS) CreateFile(path string, data []byte) error {
	return os.WriteFile(filepath.Join(f.bp, path), data, os.ModePerm)
}

func (f *FS) ReadFile(path string) ([]byte, error) {
	return os.ReadFile(filepath.Join(f.bp, path))
}

func (f *FS) Exists(path string) (ok bool, err error) {
	if _, err = os.Stat(filepath.Join(f.bp, path)); err == nil {
		ok = true
	} else {
		if errors.Is(err, fs.ErrNotExist) {
			err = nil
		}
	}
	return
}
