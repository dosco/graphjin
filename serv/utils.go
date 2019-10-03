package serv

import (
	"bytes"
	"crypto/sha1"
	"encoding/hex"
	"io"
	"sort"
	"strings"

	"github.com/cespare/xxhash/v2"
	"github.com/dosco/super-graph/jsn"
)

func mkkey(h *xxhash.Digest, k1 string, k2 string) uint64 {
	h.WriteString(k1)
	h.WriteString(k2)
	v := h.Sum64()
	h.Reset()

	return v
}

func gqlHash(b string, vars []byte) string {
	b = strings.TrimSpace(b)
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
			io.WriteString(h, strings.ToLower(b[s:e]))
		}
		if e >= len(b) {
			break
		}
	}

	if vars == nil || len(vars) == 0 {
		return hex.EncodeToString(h.Sum(nil))
	}

	fields := jsn.Keys([]byte(vars))

	sort.Slice(fields, func(i, j int) bool {
		return bytes.Compare(fields[i], fields[j]) == -1
	})

	for i := range fields {
		h.Write(fields[i])
	}

	return hex.EncodeToString(h.Sum(nil))
}

func ws(b byte) bool {
	return b == ' ' || b == '\n' || b == '\t' || b == ','
}

func al(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || (b >= '0' && b <= '9')
}
