package jsn

import (
	"bytes"
	"hash/maphash"
)

// Filter function filters the JSON keeping only the provided keys and removing all others
func Filter(w *bytes.Buffer, b []byte, keys []string) error {
	var err error
	kmap := make(map[uint64]struct{}, len(keys))
	h := maphash.Hash{}

	for i := range keys {
		_, _ = h.WriteString(keys[i])
		kmap[h.Sum64()] = struct{}{}
		h.Reset()
	}

	// is an list
	isList := false

	// list item
	item := 0

	// field in an object
	field := 0

	s, e, d := 0, 0, 0

	var k []byte
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

		if state == expectKey {
			switch b[i] {
			case '[':
				if !isList {
					err = w.WriteByte('[')
				}
				isList = true
			case '{':
				if item == 0 {
					err = w.WriteByte('{')
				} else {
					_, err = w.Write([]byte("},{"))
				}
				field = 0
				item++
			}
			if err != nil {
				return err
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

		case state == expectString && (b[i] == '"' && (slash%2 == 0)):
			e = i

		case state == expectValue && b[i] == '[':
			state = expectListClose
			d++

		case state == expectListClose && d == 0 && b[i] == ']':
			e = i

		case state == expectValue && b[i] == '{':
			state = expectObjClose
			d++

		case state == expectObjClose && d == 0 && b[i] == '}':
			e = i

		case state == expectValue && (b[i] >= '0' && b[i] <= '9'):
			state = expectNumClose

		case state == expectNumClose &&
			((b[i] < '0' || b[i] > '9') &&
				(b[i] != '.' && b[i] != 'e' && b[i] != 'E' && b[i] != '+' && b[i] != '-')):
			e = i - 1

		case state == expectValue &&
			(b[i] == 'f' || b[i] == 'F' || b[i] == 't' || b[i] == 'T'):
			state = expectBoolClose

		case state == expectBoolClose && (b[i] == 'e' || b[i] == 'E'):
			e = i

		case state == expectValue && b[i] == 'n':
			state = expectNull

		case state == expectNull && (b[i-1] == 'l' && b[i] == 'l'):
			e = i
		}

		if e != 0 {
			state = expectKey
			cb := b[s:(e + 1)]
			e = 0

			_, _ = h.Write(k)
			_, ok := kmap[h.Sum64()]
			h.Reset()

			if !ok {
				continue
			}

			if field != 0 {
				if err := w.WriteByte(','); err != nil {
					return err
				}
			}

			sk := 0
			for i := 0; i < len(cb); i++ {
				if cb[i] == '\n' || cb[i] == '\t' {
					if _, err := w.Write(cb[sk:i]); err != nil {
						return err
					}
					sk = i + 1
				}
			}

			if sk > 0 && sk < len(cb) {
				_, err = w.Write(cb[sk:])
			} else {
				_, err = w.Write(cb)
			}

			if err != nil {
				return err
			}
			field++
		}
	}

	if item != 0 {
		if err := w.WriteByte('}'); err != nil {
			return err
		}
	}

	if isList {
		if err := w.WriteByte(']'); err != nil {
			return err
		}
	}

	return nil
}
