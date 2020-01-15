package jsn

import (
	"bytes"
)

func Strip(b []byte, path [][]byte) []byte {
	s, e, d := 0, 0, 0

	ob := b
	pi := 0
	pm := false
	state := expectKey

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
			if pi == len(path) {
				pi = 0
			}
			pm = bytes.Equal(b[(s+1):i], path[pi])
			if pm {
				pi++
			}

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

		case state == expectValue && b[i] == '{':
			state = expectObjClose
			s = i
			d++

		case state == expectObjClose && d == 0 && b[i] == '}':
			e = i

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
			if pm && (b[s] == '[' || b[s] == '{') {
				b = b[s:(e + 1)]
				i = 0

				if pi == len(path) {
					return b
				}
			}

			state = expectKey
			e = 0
		}
	}

	return ob
}
