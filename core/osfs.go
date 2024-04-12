package core

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
)

type OSFS struct {
	basePath string
}

func NewOsFS(basePath string) *OSFS { return &OSFS{basePath: basePath} }

func (f *OSFS) Get(path string) ([]byte, error) {
	return os.ReadFile(filepath.Join(f.basePath, path))
}

func (f *OSFS) Put(path string, data []byte) (err error) {
	path = filepath.Join(f.basePath, path)

	dir := filepath.Dir(path)
	ok, err := f.exists(dir)
	if !ok {
		err = os.MkdirAll(dir, os.ModePerm)
	}
	if err != nil {
		return
	}

	return os.WriteFile(path, data, os.ModePerm)
}

func (f *OSFS) Exists(path string) (ok bool, err error) {
	path = filepath.Join(f.basePath, path)
	ok, err = f.exists(path)
	return
}

func (f *OSFS) exists(path string) (ok bool, err error) {
	if _, err = os.Stat(path); err == nil {
		ok = true
	} else {
		if errors.Is(err, fs.ErrNotExist) {
			err = nil
		}
	}
	return
}
