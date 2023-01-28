package serv

import (
	"os"
	"path/filepath"

	"github.com/spf13/afero"
)

type aferoFS struct {
	fs afero.Fs
}

func newAferoFS(fs afero.Fs, basePath string) *aferoFS {
	return &aferoFS{fs: afero.NewBasePathFs(fs, basePath)}
}

func (f *aferoFS) Get(path string) ([]byte, error) {
	return afero.ReadFile(f.fs, path)
}

func (f *aferoFS) Put(path string, data []byte) (err error) {
	dir := filepath.Dir(path)
	ok, err := f.Exists(dir)
	if !ok {
		err = f.fs.MkdirAll(dir, os.ModePerm)
	}
	if err != nil {
		return
	}

	return afero.WriteFile(f.fs, path, data, os.ModePerm)
}

func (f *aferoFS) Exists(path string) (exists bool, err error) {
	return afero.Exists(f.fs, path)
}
