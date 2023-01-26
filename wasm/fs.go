//go:build wasm && js

package main

import (
	"path/filepath"
	"runtime"
	"strings"
	"syscall/js"
)

type JSFS struct {
	fs js.Value
	bp string
}

func NewJSFS(fs js.Value, path string) *JSFS { return &JSFS{fs: fs, bp: path} }

func (f *JSFS) Get(path string) (data []byte, err error) {
	path = filepath.Join(f.bp, path)
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

func (f *JSFS) Put(path string, data []byte) (err error) {
	defer func() {
		if err1 := recover(); err1 != nil {
			err = toError(err1)
		}
	}()
	path = filepath.Join(f.bp, path)

	dir := filepath.Dir(path)
	ok, err := f.exists(dir)
	if !ok {
		err = f.createDir(dir)
	}
	if err != nil {
		return
	}

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

func (f *JSFS) Exists(path string) (ok bool, err error) {
	path = filepath.Join(f.bp, path)
	ok, err = f.exists(path)
	return
}

func (f *JSFS) exists(path string) (ok bool, err error) {
	defer func() {
		if err1 := recover(); err1 != nil {
			err2 := toError(err1)
			if strings.HasPrefix(err2.Error(), "ENOENT:") {
				ok = false
			} else {
				err = err2
			}
		}
	}()
	f.fs.Call("statSync", path)
	return
}

func (f *JSFS) createDir(path string) (err error) {
	defer func() {
		if err1 := recover(); err1 != nil {
			err = toError(err1)
		}
	}()
	opts := map[string]interface{}{"recursive": true}
	f.fs.Call("mkdirSync", path, opts)
	return nil
}
