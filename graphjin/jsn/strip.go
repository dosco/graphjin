package jsn

import (
	"bytes"
)

// Strip function strips out all values from the JSON data expect for the provided path
func Strip(b []byte, path [][]byte) []byte {
	s, e, d := 0, 0, 0

	ob := b
	pi := 0
	pm := false
	state := expectKey
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

		case state == expectString && (b[i] == '"' && (slash%2 == 0)):
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
			s = i

		case state == expectNull && (b[i-1] == 'l' && b[i] == 'l'):
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

		slash = 0
	}

	return ob
}
