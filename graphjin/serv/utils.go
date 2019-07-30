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

func gqlHash(b []byte) string {
	b = bytes.TrimSpace(b)
	h := sha1.New()

	s, e := 0, 0
	space := []byte{' '}

	var b0, b1 byte

	for {
		if ws(b[e]) {
			for e < len(b) && ws(b[e]) {
				e++
			}
			if e < len(b) {
				b1 = b[e]
			}
			if al(b0) && al(b1) {
				h.Write(space)
			}
		} else {
			s = e
			for e < len(b) && ws(b[e]) == false {
				e++
			}
			if e != 0 {
				b0 = b[(e - 1)]
			}
			h.Write(bytes.ToLower(b[s:e]))
		}
		if e >= len(b) {
			break
		}
	}

	return hex.EncodeToString(h.Sum(nil))
}

func ws(b byte) bool {
	return b == ' ' || b == '\n' || b == '\t' || b == ','
}

func al(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || (b >= '0' && b <= '9')
}
