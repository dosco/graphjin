//go:build js && wasm

package main

import (
	"path/filepath"
	"runtime"
	"strings"
	"syscall/js"

	"github.com/dosco/graphjin/plugin"
)

type JSFS struct {
	fs js.Value
	bp string
}

func NewJSFS(fs js.Value) *JSFS                      { return &JSFS{fs: fs} }
func NewJSFSWithBase(fs js.Value, path string) *JSFS { return &JSFS{fs: fs, bp: path} }

func (f *JSFS) CreateDir(path string) (err error) {
	defer func() {
		if err1 := recover(); err1 != nil {
			err = toError(err1)
		}
	}()
	opts := map[string]interface{}{"recursive": true}
	path = filepath.Join(f.bp, path)
	f.fs.Call("mkdirSync", path, opts)
	return nil
}

func (f *JSFS) ReadDir(path string) (fi []plugin.FileInfo, err error) {
	path = filepath.Join(f.bp, path)
	if err := f.exists(path); err != nil {
		return nil, err
	}
	defer func() {
		if err1 := recover(); err1 != nil {
			err = toError(err1)
		}
	}()
	opts := map[string]interface{}{"withFileTypes": true}
	files := f.fs.Call("readdirSync", path, opts)

	fi = make([]plugin.FileInfo, files.Length())
	for i := 0; i < files.Length(); i++ {
		f := files.Index(i)
		fi[i] = &FileInfo{
			name:  f.Get("name").String(),
			isDir: f.Call("isDirectory").Bool(),
		}
	}
	return fi, nil
}

func (f *JSFS) CreateFile(path string, data []byte) (err error) {
	defer func() {
		if err1 := recover(); err1 != nil {
			err = toError(err1)
		}
	}()
	path = filepath.Join(f.bp, path)

	a := js.Global().Get("Uint8Array").New(len(data))
	js.CopyBytesToJS(a, data)
	runtime.KeepAlive(data)
	jsData := js.Global().Get("Int8Array").New(
		a.Get("buffer"),
		a.Get("byteOffset"),
		a.Get("byteLength"))

	f.fs.Call("writeFileSync", path, jsData)
	return nil
}

func (f *JSFS) ReadFile(path string) (data []byte, err error) {
	path = filepath.Join(f.bp, path)
	if err := f.exists(path); err != nil {
		return nil, err
	}
	defer func() {
		if err1 := recover(); err1 != nil {
			err = toError(err1)
		}
	}()
	buf := f.fs.Call("readFileSync", path)

	a := js.Global().Get("Uint8Array").New(buf)
	data = make([]byte, a.Get("length").Int())
	js.CopyBytesToGo(data, a)
	return data, nil
}

func (f *JSFS) Exists(path string) (exists bool, err error) {
	path = filepath.Join(f.bp, path)
	if err := f.exists(path); err == plugin.ErrNotFound {
		return false, nil
	}
	return (err == nil), err
}

func (f *JSFS) exists(path string) (err error) {
	defer func() {
		if err1 := recover(); err1 != nil {
			err2 := toError(err1)
			if strings.HasPrefix(err2.Error(), "ENOENT:") {
				err = plugin.ErrNotFound
			} else {
				err = err2
			}
		}
	}()
	f.fs.Call("statSync", path)
	return nil
}

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
