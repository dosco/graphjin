package jsn

import (
	"bytes"
	"errors"
	"hash/maphash"
)

// Replace function replaces key-value pairs provided in the `from` argument with those in the `to` argument
func Replace(w *bytes.Buffer, b []byte, from, to []Field) error {
	if len(from) != len(to) {
		return errors.New("'from' and 'to' must be of the same length")
	}

	if len(from) == 0 || len(to) == 0 {
		_, err := w.Write(b)
		return err
	}

	h := maphash.Hash{}
	tmap := make(map[uint64]int, len(from))

	for i, f := range from {
		if _, err := h.Write(f.Key); err != nil {
			return err
		}
		if _, err := h.Write(f.Value); err != nil {
			return err
		}

		tmap[h.Sum64()] = i
		h.Reset()
	}

	// dec := json.NewDecoder(bytes.NewReader(b))

	// var s, e int64
	// for {
	// 	t, err := dec.Token()
	// 	if err == io.EOF {
	// 		break
	// 	}
	// 	if err != nil {
	// 		log.Fatal(err)
	// 	}
	// 	e = dec.InputOffset()
	// 	fmt.Printf("[%d:%d] - %T: %v", s, e, t, t)
	// 	if dec.More() {
	// 		fmt.Printf(" (more)")
	// 	}
	// 	w.Write(b[s:e])
	// 	fmt.Printf("\n")
	// 	s = dec.InputOffset()
	// }

	s, e, d := 0, 0, 0

	state := expectKey
	ws, we := -1, len(b)

	instr := false
	slash := 0

	for i := 0; i < len(b); i++ {
		if instr && b[i] == '\\' {
			slash++
			continue
		}

		// skip any left padding whitespace
		if ws == -1 && (b[i] == '{' || b[i] == '[') {
			ws = i
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
			if _, err := h.Write(b[(s + 1):i]); err != nil {
				return err
			}
			we = s

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
			e++

			if e <= s {
				return errors.New("invalid json")
			}

			if _, err := h.Write(b[s:e]); err != nil {
				return err
			}

			if (we + 1) <= ws {
				return errors.New("invalid json")
			}

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

		slash = 0
	}

	if ws == -1 || (ws == 0 && we == len(b)) {
		w.Write(b)
	} else if ws < we {
		w.Write(b[ws:we])
	}

	return nil
}
