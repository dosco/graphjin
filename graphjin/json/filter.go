package json

import (
	"bytes"
	"crypto/sha1"
)

func Filter(w *bytes.Buffer, b []byte, keys []string) error {
	state := expectKey
	var err error

	kmap := make(map[[20]byte]struct{}, len(keys))

	for _, k := range keys {
		h := sha1.Sum([]byte(k))
		if _, ok := kmap[h]; !ok {
			kmap[h] = struct{}{}
		}
	}

	// is an list
	isList := false

	// list item
	item := 0

	// field in an object
	field := 0

	s, e, d := 0, 0, 0

	kf := false

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
		case state == expectKey:
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
				item++
				field = 0
			case '"':
				state = expectKeyClose
				s = i
				i++
			}
			if err != nil {
				return err
			}
		case state == expectKeyClose && b[i] == '"':
			state = expectColon
			k := b[(s + 1):i]
			_, kf = kmap[sha1.Sum(k)]

		case state == expectColon && b[i] == ':':
			state = expectValue

		case state == expectValue && b[i] == '"':
			state = expectString

		case state == expectString && b[i] == '"':
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
			i--
			e = i

		case state == expectValue &&
			(b[i] == 'f' || b[i] == 'F' || b[i] == 't' || b[i] == 'T'):
			state = expectBoolClose

		case state == expectBoolClose && (b[i] == 'e' || b[i] == 'E'):
			e = i
		}

		if e != 0 {
			state = expectKey
			cb := b[s:(e + 1)]
			e = 0

			if !kf {
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
				_, err = w.Write(cb[sk:len(cb)])
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
