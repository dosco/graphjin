package serv

import (
	"bytes"
	"crypto/sha1"
	"encoding/hex"

	"github.com/cespare/xxhash/v2"
)

func mkkey(h *xxhash.Digest, k1 string, k2 string) uint64 {
	h.WriteString(k1)
	h.WriteString(k2)
	v := h.Sum64()
	h.Reset()

	return v
}

func relaxHash(b []byte) string {
	h := sha1.New()
	s, e := 0, 0

	for {
		if e == (len(b) - 1) {
			if s != 0 {
				e++
				h.Write(bytes.ToLower(b[s:e]))
			}
			break
		} else if ws(b[e]) == false && ws(b[(e+1)]) {
			e++
			h.Write(bytes.ToLower(b[s:e]))
			s = 0
		} else if ws(b[e]) && ws(b[(e+1)]) == false {
			e++
			s = e
		} else {
			e++
		}
	}

	return hex.EncodeToString(h.Sum(nil))
}

func ws(b byte) bool {
	return b == ' ' || b == '\n' || b == '\t'
}
