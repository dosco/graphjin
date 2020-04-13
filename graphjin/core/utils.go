package core

import (
	"github.com/cespare/xxhash/v2"
)

// nolint: errcheck
func mkkey(h *xxhash.Digest, k1 string, k2 string) uint64 {
	h.WriteString(k1)
	h.WriteString(k2)
	v := h.Sum64()
	h.Reset()

	return v
}
