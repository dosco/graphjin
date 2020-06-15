package core

import "hash/maphash"

// nolint: errcheck
func mkkey(h *maphash.Hash, k1, k2 string) uint64 {
	h.WriteString(k1)
	h.WriteString(k2)
	v := h.Sum64()
	h.Reset()

	return v
}
