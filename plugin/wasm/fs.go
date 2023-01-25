package wasm

type FileInfo struct {
	name  string
	isDir bool
}

func (fi *FileInfo) Name() string {
	return fi.name
}

func (fi *FileInfo) IsDir() bool {
	return fi.isDir
}
