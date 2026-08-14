package jsn

import (
	"bytes"
	"hash/maphash"
)

// FilterField names one key to keep when filtering. Key is matched
// against keys in the source JSON; As, when non-empty, is written to
// the output in the key's place. The caller is responsible for As being
// safe to embed in a JSON string without escaping (GraphQL identifiers
// are).
type FilterField struct {
	Key string
	As  string
}

// Filter function filters the JSON keeping only the provided keys and removing all others
func Filter(w *bytes.Buffer, b []byte, keys []string) error {
	fields := make([]FilterField, len(keys))
	for i := range keys {
		fields[i] = FilterField{Key: keys[i]}
	}
	return FilterAliased(w, b, fields)
}

// FilterAliased filters the JSON keeping only the keys named by fields
// and writing each kept pair under its output name. The same Key may be
// requested under several output names; each occurrence in the source
// then emits one pair per requested field, in fields order.
func FilterAliased(w *bytes.Buffer, b []byte, fields []FilterField) error {
	var err error
	kmap := make(map[uint64]int, len(fields))
	var collisions map[uint64][]int
	h := maphash.Hash{}

	for i := range fields {
		addKeyHash(kmap, &collisions, hashString(&h, fields[i].Key), i)
	}

	// matched field indexes for the pair currently being emitted
	var matches []int

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

		case state == expectValue && (b[i] == '-' || (b[i] >= '0' && b[i] <= '9')):
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

			sum := hashBytes(&h, k)
			n, ok := kmap[sum]
			if !ok {
				continue
			}
			matches = gatherFilterMatches(fields, collisions, sum, n, k, matches[:0])

			for _, fi := range matches {
				if field != 0 {
					if err := w.WriteByte(','); err != nil {
						return err
					}
				}

				out := fields[fi].As
				if out != "" && out != fields[fi].Key {
					// Rename: emit the output name, then everything after
					// the source key's closing quote (colon and value).
					// Within cb the key spans cb[1 : 1+len(k)], so the
					// remainder starts at len(k)+2.
					if err := w.WriteByte('"'); err != nil {
						return err
					}
					if _, err := w.WriteString(out); err != nil {
						return err
					}
					if err := w.WriteByte('"'); err != nil {
						return err
					}
					if err := writePairCleaned(w, cb[len(k)+2:]); err != nil {
						return err
					}
				} else {
					if err := writePairCleaned(w, cb); err != nil {
						return err
					}
				}
				field++
			}
		}

		// Reset the consecutive-backslash counter so a later `"` parity
		// check only considers `\`s immediately preceding it. Without
		// this, multiple escape sequences in a string accumulate the
		// count and break the parity check on the closing quote.
		slash = 0
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

// writePairCleaned writes cb minus any raw newline/tab bytes (valid
// JSON escapes them inside strings, so these are only inter-token
// whitespace).
func writePairCleaned(w *bytes.Buffer, cb []byte) error {
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
		_, err := w.Write(cb[sk:])
		return err
	}
	if sk == 0 {
		_, err := w.Write(cb)
		return err
	}
	return nil
}

// gatherFilterMatches appends to out the index of every field whose Key
// equals key, in fields order (addCollision preserves insertion order,
// so duplicates emit in the order they were requested).
func gatherFilterMatches(fields []FilterField, collisions map[uint64][]int, sum uint64, n int, key []byte, out []int) []int {
	if n >= 0 {
		if stringEqualBytes(fields[n].Key, key) {
			out = append(out, n)
		}
		return out
	}
	for _, i := range collisions[sum] {
		if stringEqualBytes(fields[i].Key, key) {
			out = append(out, i)
		}
	}
	return out
}
