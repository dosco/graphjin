package core

type FS interface {
	Get(path string) (data []byte, err error)
	Put(path string, data []byte) error
	Exists(path string) (exists bool, err error)
	List(path string) (entries []string, err error)
}

// FSBase is the optional half of FS, implemented only by filesystems
// backed by a real directory on disk. Almost every path in a config
// reaches disk through FS and is therefore already interpreted relative
// to that directory; the handful that are handed to the OS directly (a
// local filesystem table's root, the OpenAPI specs dir) use this to get
// the same treatment instead of resolving against whatever working
// directory the process happens to have.
//
// Filesystems with no OS directory behind them — a deploy bundle, an
// embedded FS, a test double — deliberately don't implement it, and
// relative paths there keep falling back to the process working
// directory.
type FSBase interface {
	// BasePath returns the directory relative paths resolve against.
	BasePath() string
}
