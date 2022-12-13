package fs

import (
	"os"
	"path/filepath"

	"github.com/dosco/graphjin/plugin"
)

type OsFS struct {
	bp string
}

func NewOsFS() *OsFS                    { return &OsFS{} }
func NewOsFSWithBase(path string) *OsFS { return &OsFS{bp: path} }

func (f *OsFS) CreateDir(path string) error {
	return os.MkdirAll(filepath.Join(f.bp, path), os.ModePerm)
}

func (f *OsFS) ReadDir(path string) (fi []plugin.FileInfo, err error) {
	path = filepath.Join(f.bp, path)
	if err := f.exists(path); err != nil {
		return nil, err
	}
	files, err := os.ReadDir(path)
	if err != nil {
		return nil, err
	}
	fi = make([]plugin.FileInfo, len(files))
	for i, v := range files {
		fi[i] = &FileInfo{name: v.Name(), isDir: v.IsDir()}
	}
	return fi, nil
}

func (f *OsFS) CreateFile(path string, data []byte) error {
	return os.WriteFile(filepath.Join(f.bp, path), data, os.ModePerm)
}

func (f *OsFS) ReadFile(path string) ([]byte, error) {
	path = filepath.Join(f.bp, path)
	if err := f.exists(path); err != nil {
		return nil, err
	}
	return os.ReadFile(path)
}

func (f *OsFS) Exists(path string) (exists bool, err error) {
	path = filepath.Join(f.bp, path)
	if err := f.exists(path); err == plugin.ErrNotFound {
		return false, nil
	}
	return (err == nil), err
}

func (f *OsFS) exists(path string) error {
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return plugin.ErrNotFound
		} else {
			return err
		}
	}
	return nil
}
