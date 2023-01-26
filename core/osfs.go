package core

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
)

type osFS struct {
	bp string
}

func NewFS(basePath string) *osFS { return &osFS{bp: basePath} }

func (f *osFS) CreateDir(path string) error {
	return os.MkdirAll(filepath.Join(f.bp, path), os.ModePerm)
}

func (f *osFS) CreateFile(path string, data []byte) error {
	return os.WriteFile(filepath.Join(f.bp, path), data, os.ModePerm)
}

func (f *osFS) ReadFile(path string) ([]byte, error) {
	return os.ReadFile(filepath.Join(f.bp, path))
}

func (f *osFS) Exists(path string) (ok bool, err error) {
	if _, err = os.Stat(filepath.Join(f.bp, path)); err == nil {
		ok = true
	} else {
		if errors.Is(err, fs.ErrNotExist) {
			err = nil
		}
	}
	return
}
