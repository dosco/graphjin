//nolint:errcheck
package serv

import (
	"bytes"
	"crypto/sha1"
	"encoding/hex"
	"hash"
	"net/http"

	"github.com/go-http-utils/fresh"
	"github.com/go-http-utils/headers"
)

type hashWriter struct {
	rw     http.ResponseWriter
	hash   hash.Hash
	buf    *bytes.Buffer
	status int
}

func (hw hashWriter) Header() http.Header {
	return hw.rw.Header()
}

func (hw *hashWriter) WriteHeader(status int) {
	hw.status = status
}

func (hw *hashWriter) Write(b []byte) (int, error) {
	if hw.status == 0 {
		hw.status = http.StatusOK
	}
	// bytes.Buffer.Write(b) always return (len(b), nil), so just
	// ignore the return values.
	hw.buf.Write(b)

	l, err := hw.hash.Write(b)
	return l, err
}

func writeRaw(res http.ResponseWriter, hw hashWriter) {
	res.WriteHeader(hw.status)

	res.Write(hw.buf.Bytes())
}

// Handler wraps the http.Handler h with ETag support.
func ETagHandler(h http.Handler, weak bool) http.Handler {
	return http.HandlerFunc(func(res http.ResponseWriter, req *http.Request) {
		hw := hashWriter{rw: res, hash: sha1.New(), buf: bytes.NewBuffer(nil)}
		h.ServeHTTP(&hw, req)

		resHeader := res.Header()

		if hw.hash == nil ||
			resHeader.Get(headers.ETag) != "" ||
			hw.status == http.StatusNoContent ||
			hw.buf.Len() == 0 {
			writeRaw(res, hw)
			return
		}

		etag := hex.EncodeToString(hw.hash.Sum(nil))

		if weak {
			etag = "W/" + etag
		}

		resHeader.Set(headers.ETag, etag)

		if fresh.IsFresh(req.Header, resHeader) {
			res.WriteHeader(http.StatusNotModified)
		} else {
			writeRaw(res, hw)
		}
	})
}

// MIT License

// Copyright (c) 2016 go-http-utils

// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:

// The above copyright notice and this permission notice shall be included in all
// copies or substantial portions of the Software.

// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.
