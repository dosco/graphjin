package core

type FS interface {
	CreateDir(path string) error
	CreateFile(path string, data []byte) error
	ReadFile(path string) (data []byte, err error)
	Exists(path string) (exists bool, err error)
}
