package jsn

import (
	"bytes"
	"errors"

	"github.com/cespare/xxhash/v2"
)

func Replace(w *bytes.Buffer, b []byte, from, to []Field) error {
	if len(from) != len(to) {
		return errors.New("'from' and 'to' must be of the same length")
	}

	h := xxhash.New()
	tmap := make(map[uint64]int, len(from))

	for i, f := range from {
		h.Write(f.Key)
		h.Write(f.Value)

		tmap[h.Sum64()] = i
		h.Reset()
	}

	s, e, d := 0, 0, 0

	state := expectKey
	ws, we := -1, len(b)

	for i := 0; i < len(b); i++ {
		// skip any left padding whitespace
		if ws == -1 && (b[i] == '{' || b[i] == '[') {
			ws = i
		}

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

		case state == expectKeyClose && b[i] == '"':
			state = expectColon
			h.Write(b[(s + 1):i])
			we = s

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

		if e != 0 {
			e++

			h.Write(b[s:e])
			n, ok := tmap[h.Sum64()]
			h.Reset()

			if ok {
				if _, err := w.Write(b[ws:(we + 1)]); err != nil {
					return err
				}

				if len(to[n].Key) != 0 {
					var err error

					if _, err := w.Write(to[n].Key); err != nil {
						return err
					}
					if _, err := w.WriteString(`":`); err != nil {
						return err
					}
					if len(to[n].Value) != 0 {
						_, err = w.Write(to[n].Value)
					} else {
						_, err = w.WriteString("null")
					}
					if err != nil {
						return err
					}

					ws = e
				} else if b[e] == ',' {
					ws = e + 1
				} else {
					ws = e
				}
			}

			if !ok && (b[s] == '[' || b[s] == '{') {
				// the i++ in the for loop will add 1 so we account for that (s - 1)
				i = s - 1
			}

			state = expectKey
			we = len(b)
			e = 0
			d = 0
		}
	}

	if ws == -1 || (ws == 0 && we == len(b)) {
		w.Write(b)
	} else {
		w.Write(b[ws:we])
	}

	return nil
}
