package serv

import "github.com/cespare/xxhash/v2"

func mkkey(h *xxhash.Digest, k1 string, k2 string) uint64 {
	h.WriteString(k1)
	h.WriteString(k2)
	v := h.Sum64()
	h.Reset()

	return v
}
