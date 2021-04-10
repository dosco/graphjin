//nolint:errcheck
package etags

import (
	"bytes"
	"crypto/sha1"
	"encoding/hex"
	"hash"
	"net/http"

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
func Handler(h http.Handler, weak bool) http.Handler {
	return http.HandlerFunc(func(res http.ResponseWriter, req *http.Request) {
		hw := hashWriter{rw: res, hash: sha1.New(), buf: bytes.NewBuffer(nil)}
		h.ServeHTTP(&hw, req)

		resHeader := res.Header()

		if hw.hash == nil ||
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

		if IsFresh(req.Header, resHeader) {
			res.WriteHeader(http.StatusNotModified)
		} else {
			writeRaw(res, hw)
		}
	})
}
