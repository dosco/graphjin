package jsn

import (
	"bytes"
	"hash/maphash"
)

func isJSONNumberStart(c byte) bool {
	return c == '-' || (c >= '0' && c <= '9')
}

func isJSONNumberPart(c byte) bool {
	return (c >= '0' && c <= '9') ||
		c == '.' || c == 'e' || c == 'E' || c == '+' || c == '-'
}

func hashBytes(h *maphash.Hash, b []byte) uint64 {
	h.Reset()
	_, _ = h.Write(b)
	return h.Sum64()
}

func hashString(h *maphash.Hash, s string) uint64 {
	h.Reset()
	_, _ = h.WriteString(s)
	return h.Sum64()
}

func addKeyHash(m map[uint64]int, collisions *map[uint64][]int, sum uint64, n int) {
	prev, ok := m[sum]
	if !ok {
		m[sum] = n
		return
	}
	addCollision(m, collisions, sum, prev, n)
}

func lookupBytesKeyHash(keys [][]byte, collisions map[uint64][]int, sum uint64, n int, key []byte) bool {
	if n >= 0 {
		return bytes.Equal(keys[n], key)
	}
	c := collisions[sum]
	for i := len(c) - 1; i >= 0; i-- {
		if bytes.Equal(keys[c[i]], key) {
			return true
		}
	}
	return false
}

func lookupStringKeyHash(keys []string, collisions map[uint64][]int, sum uint64, n int, key []byte) bool {
	if n >= 0 {
		return stringEqualBytes(keys[n], key)
	}
	c := collisions[sum]
	for i := len(c) - 1; i >= 0; i-- {
		if stringEqualBytes(keys[c[i]], key) {
			return true
		}
	}
	return false
}

func addFieldHash(m map[uint64]int, collisions *map[uint64][]int, sum uint64, n int) {
	prev, ok := m[sum]
	if !ok {
		m[sum] = n
		return
	}
	addCollision(m, collisions, sum, prev, n)
}

func addCollision(m map[uint64]int, collisions *map[uint64][]int, sum uint64, prev, n int) {
	if *collisions == nil {
		*collisions = make(map[uint64][]int)
	}
	if prev >= 0 {
		(*collisions)[sum] = []int{prev, n}
		m[sum] = -1
		return
	}
	(*collisions)[sum] = append((*collisions)[sum], n)
}

func lookupFieldHash(fields []Field, collisions map[uint64][]int, sum uint64, n int, key, value []byte) (int, bool) {
	if n >= 0 {
		if fieldMatch(fields[n], key, value) {
			return n, true
		}
		return 0, false
	}
	c := collisions[sum]
	for i := len(c) - 1; i >= 0; i-- {
		n = c[i]
		if fieldMatch(fields[n], key, value) {
			return n, true
		}
	}
	return 0, false
}

func fieldMatch(f Field, key, value []byte) bool {
	return bytes.Equal(f.Key, key) && bytes.Equal(f.Value, value)
}

func stringEqualBytes(s string, b []byte) bool {
	if len(s) != len(b) {
		return false
	}
	for i := 0; i < len(b); i++ {
		if s[i] != b[i] {
			return false
		}
	}
	return true
}
