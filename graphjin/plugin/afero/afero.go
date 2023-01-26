package afero

import (
	"os"

	"github.com/spf13/afero"
)

type AferoFS struct {
	fs afero.Fs
}

func NewFS(fs afero.Fs, basePath string) *AferoFS {
	return &AferoFS{fs: afero.NewBasePathFs(fs, basePath)}
}

func (f *AferoFS) CreateDir(path string) error {
	return f.fs.MkdirAll(path, os.ModePerm)
}

func (f *AferoFS) CreateFile(path string, data []byte) error {
	return afero.WriteFile(f.fs, path, data, os.ModePerm)
}

func (f *AferoFS) ReadFile(path string) ([]byte, error) {
	return afero.ReadFile(f.fs, path)
}

func (f *AferoFS) Exists(path string) (exists bool, err error) {
	return afero.Exists(f.fs, path)
}
