package core

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
)

type osFS struct {
	basePath string
}

// NewOsFS creates a new OSFS instance
func NewOsFS(basePath string) *osFS { return &osFS{basePath: basePath} }

// Get returns the file content
func (f *osFS) Get(path string) ([]byte, error) {
	return os.ReadFile(filepath.Join(f.basePath, path))
}

// Put writes the data to the file
func (f *osFS) Put(path string, data []byte) (err error) {
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

// Exists checks if the file exists
func (f *osFS) Exists(path string) (ok bool, err error) {
	path = filepath.Join(f.basePath, path)
	ok, err = f.exists(path)
	return
}

// Remove deletes the file
func (f *osFS) exists(path string) (ok bool, err error) {
	if _, err = os.Stat(path); err == nil {
		ok = true
	} else {
		if errors.Is(err, fs.ErrNotExist) {
			err = nil
		}
	}
	return
}
