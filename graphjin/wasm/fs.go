package main

/*
type Fs struct {
	fs js.Value
}

func (fs *Fs) Create(name string) (File, error) {

}




	// Mkdir creates a directory in the filesystem, return an error if any
	// happens.
	Mkdir(name string, perm os.FileMode) error

	// MkdirAll creates a directory path and all parents that does not exist
	// yet.
	MkdirAll(path string, perm os.FileMode) error

	// Open opens a file, returning it or an error, if any happens.
	Open(name string) (File, error)

	// OpenFile opens a file using the given flags and the given mode.
	OpenFile(name string, flag int, perm os.FileMode) (File, error)

	// Remove removes a file identified by name, returning an error, if any
	// happens.
	Remove(name string) error

	// RemoveAll removes a directory path and any children it contains. It
	// does not fail if the path does not exist (return nil).
	RemoveAll(path string) error

	// Rename renames a file.
	Rename(oldname, newname string) error

	// Stat returns a FileInfo describing the named file, or an error, if any
	// happens.
	Stat(name string) (os.FileInfo, error)

	// The name of this FileSystem
	Name() string

	// Chmod changes the mode of the named file to mode.
	Chmod(name string, mode os.FileMode) error

	// Chown changes the uid and gid of the named file.
	Chown(name string, uid, gid int) error

	//Chtimes changes the access and modification times of the named file
	Chtimes(name string, atime time.Time, mtime time.Time) error
}


type File struct {
	fs js.Value
	f js.Value
	fd js.Value
}

func (f *File) Close() error {
	f.fs.Call("closeSync", f.fd)
}

func (f *File) Read(p []byte) (n int, err error)
	n := f.fs.Call("readSync", f.fd, &p)
	return n.Int(), nil
}

func (f *File) ReadAt(p []byte, off int64) (n int, err error) {
	n := f.fs.Call("readvSync", f.fd, off, &p)
	return n.Int(), nil
}

func (f *File) Seek(offset int64, whence int) (int64, error) {
	n := f.fs.Call("readSync", f.fd, &p)
	return n.Int(), nil
}

	io.Seeker
	io.Writer
	io.WriterAt

	Name() string
	Readdir(count int) ([]os.FileInfo, error)
	Readdirnames(n int) ([]string, error)
	Stat() (os.FileInfo, error)
	Sync() error
	Truncate(size int64) error
	WriteString(s string) (ret int, err error)
}
*/
