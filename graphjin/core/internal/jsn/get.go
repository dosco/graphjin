package jsn

import (
	"hash/maphash"
)

const (
	expectKey int = iota
	expectKeyClose
	expectColon
	expectValue
	expectString
	expectNull
	expectListClose
	expectObjClose
	expectBoolClose
	expectNumClose
)

// Field struct holds a JSON key and value
type Field struct {
	Key   []byte
	Value []byte
}

// Value function is a utility function to sanitize returned values
func Value(b []byte) []byte {
	e := (len(b) - 1)
	switch {
	case b[0] == '"' && b[e] == '"':
		return b[1:(len(b) - 1)]
	case b[0] == '[' && b[e] == ']':
		return nil
	case b[0] == '{' && b[e] == '}':
		return nil
	default:
		return b
	}
}

// Keys function fetches values for the provided keys
func Get(b []byte, keys [][]byte) []Field {
	kmap := make(map[uint64]struct{}, len(keys))
	h := maphash.Hash{}

	for i := range keys {
		_, _ = h.Write(keys[i])
		kmap[h.Sum64()] = struct{}{}
		h.Reset()
	}

	res := make([]Field, 0, 20)

	s, e, d := 0, 0, 0

	var k []byte
	state := expectKey

	n := 0
	instr := false
	slash := 0

	for i := 0; i < len(b); i++ {
		if instr && b[i] == '\\' {
			slash++
			continue
		}

		if b[i] == '"' && (slash%2 == 0) {
			instr = !instr
		}

		if state == expectObjClose || state == expectListClose {
			if !instr {
				switch b[i] {
				case '{', '[':
					d++
				case '}', ']':
					d--
				}
			}
		}

		switch {
		case state == expectKey && b[i] == '"':
			state = expectKeyClose
			s = i

		case state == expectKeyClose && (b[i] == '"' && (slash%2 == 0)):
			state = expectColon
			k = b[(s + 1):i]

		case state == expectColon && b[i] == ':':
			state = expectValue

		case state == expectValue && b[i] == '"':
			state = expectString
			s = i

		case state == expectString && (b[i] == '"' && (slash%2 == 0)):
			e = i

		case state == expectValue && b[i] == '[':
			state = expectListClose
			s = i
			d++

		case state == expectListClose && d == 0 && b[i] == ']':
			e = i
			i = s

		case state == expectValue && b[i] == '{':
			state = expectObjClose
			s = i
			d++

		case state == expectObjClose && d == 0 && b[i] == '}':
			e = i
			i = s

		case state == expectValue && (b[i] >= '0' && b[i] <= '9'):
			state = expectNumClose
			s = i

		case state == expectNumClose &&
			((b[i] < '0' || b[i] > '9') &&
				(b[i] != '.' && b[i] != 'e' && b[i] != 'E' && b[i] != '+' && b[i] != '-')):
			e = i - 1

		case state == expectValue &&
			(b[i] == 'f' || b[i] == 'F' || b[i] == 't' || b[i] == 'T'):
			state = expectBoolClose
			s = i

		case state == expectBoolClose && (b[i] == 'e' || b[i] == 'E'):
			e = i

		case state == expectValue && b[i] == 'n':
			state = expectNull
			s = i

		case state == expectNull && (b[i-1] == 'l' && b[i] == 'l'):
			e = i
		}

		if e != 0 {
			_, _ = h.Write(k)
			_, ok := kmap[h.Sum64()]
			h.Reset()

			if ok {
				res = append(res, Field{k, b[s:(e + 1)]})
				n++
			}

			if state == expectListClose {
			loop:
				for j := i + 1; j < len(b); j++ {
					switch b[j] {
					case ' ', '\t', '\n':
						continue
					case '{':
						break loop
					}
					i = e
					break loop
				}
			}

			state = expectKey
			e = 0
		}

		slash = 0
	}

	return res[:n]
}
