package core

type FS interface {
	Get(path string) (data []byte, err error)
	Put(path string, data []byte) error
	Exists(path string) (exists bool, err error)
}
