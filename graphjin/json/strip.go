package json

import (
	"bytes"
)

func Strip(b []byte, path []string) []byte {
	s := 0
	state := expectKey

	kb := make([][]byte, 0, len(path))
	ki := 0
	for _, k := range path {
		kb = append(kb, []byte(k))
	}

	for i := 0; i < len(b); i++ {
		switch {
		case state == expectKey && b[i] == '"':
			state = expectKeyClose
			s = i + 1
			continue

		case state == expectKeyClose && b[i] == '"':
			state = expectColon
		}

		if state != expectColon {
			continue
		}

		if ki >= len(kb) {
			return nil
		}

		if !bytes.Equal(b[s:i], kb[ki]) {
			state = expectKey
			continue
		}

		ki++

		e := 0
		d := 0
		s := 0
		for ; i < len(b); i++ {
			if state == expectObjClose || state == expectListClose {
				switch b[i] {
				case '{', '[':
					d++
				case '}', ']':
					d--
				}
			}

			switch {
			case state == expectColon && b[i] == ':':
				state = expectValue

			case state == expectValue && b[i] == '"':
				state = expectString
				s = i

			case state == expectString && b[i] == '"':
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
				i--
				e = i

			case state == expectValue &&
				(b[i] == 'f' || b[i] == 'F' || b[i] == 't' || b[i] == 'T'):
				state = expectBoolClose
				s = i

			case state == expectBoolClose && (b[i] == 'e' || b[i] == 'E'):
				e = i
			}

			if e != 0 && (b[s] == '[' || b[s] == '{') {
				e++
				b = b[s:e]
				i = 0

				if ki == len(kb) {
					return b
				}

				state = expectKey
				break
			}
		}
	}

	return nil
}
