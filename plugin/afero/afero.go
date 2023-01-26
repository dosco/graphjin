package afero

import (
	"os"
	"path/filepath"

	"github.com/spf13/afero"
)

type AferoFS struct {
	fs afero.Fs
}

func NewFS(fs afero.Fs, basePath string) *AferoFS {
	return &AferoFS{fs: afero.NewBasePathFs(fs, basePath)}
}

func (f *AferoFS) Get(path string) ([]byte, error) {
	return afero.ReadFile(f.fs, path)
}

func (f *AferoFS) Put(path string, data []byte) (err error) {
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

func (f *AferoFS) Exists(path string) (exists bool, err error) {
	return afero.Exists(f.fs, path)
}
