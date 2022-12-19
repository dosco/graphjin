package fs

import (
	"os"

	"github.com/dosco/graphjin/v2/plugin"
	"github.com/spf13/afero"
)

type AferoFS struct {
	fs afero.Fs
}

func NewAferoFS(fs afero.Fs) *AferoFS { return &AferoFS{fs: fs} }

func NewAferoFSWithBase(fs afero.Fs, path string) *AferoFS {
	return &AferoFS{fs: afero.NewBasePathFs(fs, path)}
}

func (f *AferoFS) CreateDir(path string) error {
	return f.fs.MkdirAll(path, os.ModePerm)
}

func (f *AferoFS) ReadDir(path string) (fi []plugin.FileInfo, err error) {
	if ok, err := afero.DirExists(f.fs, path); !ok {
		return nil, plugin.ErrNotFound
	} else if err != nil {
		return nil, err
	}
	files, err := afero.ReadDir(f.fs, path)
	if err != nil {
		return nil, err
	}
	fi = make([]plugin.FileInfo, len(files))
	for i, v := range files {
		fi[i] = &FileInfo{name: v.Name(), isDir: v.IsDir()}
	}
	return fi, nil
}

func (f *AferoFS) CreateFile(path string, data []byte) error {
	return afero.WriteFile(f.fs, path, data, os.ModePerm)
}

func (f *AferoFS) ReadFile(path string) ([]byte, error) {
	if ok, err := afero.Exists(f.fs, path); !ok {
		return nil, plugin.ErrNotFound
	} else if err != nil {
		return nil, err
	}
	return afero.ReadFile(f.fs, path)
}

func (f *AferoFS) Exists(path string) (exists bool, err error) {
	return afero.Exists(f.fs, path)
}
