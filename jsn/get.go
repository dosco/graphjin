package jsn

import (
	"github.com/cespare/xxhash/v2"
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

type Field struct {
	Key   []byte
	Value []byte
}

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

func Get(b []byte, keys [][]byte) []Field {
	kmap := make(map[uint64]struct{}, len(keys))

	for i := range keys {
		kmap[xxhash.Sum64(keys[i])] = struct{}{}
	}

	res := make([]Field, 0, 20)

	s, e, d := 0, 0, 0

	var k []byte
	state := expectKey

	n := 0
	for i := 0; i < len(b); i++ {
		if state == expectObjClose || state == expectListClose {
			switch b[i] {
			case '{', '[':
				d++
			case '}', ']':
				d--
			}
		}

		switch {
		case state == expectKey && b[i] == '"':
			state = expectKeyClose
			s = i

		case state == expectKeyClose && (b[i-1] != '\\' && b[i] == '"'):
			state = expectColon
			k = b[(s + 1):i]

		case state == expectColon && b[i] == ':':
			state = expectValue

		case state == expectValue && b[i] == '"':
			state = expectString
			s = i

		case state == expectString && (b[i-1] != '\\' && b[i] == '"'):
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

		case state == expectNull && b[i] == 'l':
			e = i
		}

		if e != 0 {
			_, ok := kmap[xxhash.Sum64(k)]

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
	}

	return res[:n]
}
