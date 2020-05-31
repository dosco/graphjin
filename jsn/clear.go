package jsn

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
)

// Clear function wipes all scalar values from the json including those directly in an array
func Clear(w *bytes.Buffer, v []byte) error {
	dec := json.NewDecoder(bytes.NewReader(v))

	st := newIntStack()
	isValue := false
	inArray := false
	n := 0

	for {
		var t json.Token
		var err error

		if t, err = dec.Token(); err == io.EOF {
			break
		} else if err != nil {
			return err
		}

		switch v1 := t.(type) {
		case int:
			if isValue && !inArray {
				w.WriteByte('0')
				isValue = false
				n++
			}

		case float64:
			if isValue && !inArray {
				w.WriteString(`0.0`)
				isValue = false
				n++
			}

		case bool:
			if isValue && !inArray {
				w.WriteString(`false`)
				isValue = false
				n++
			}

		case json.Number:
			if isValue && !inArray {
				w.WriteString(`0`)
				isValue = false
				n++
			}

		case nil:
			if isValue && !inArray {
				w.WriteString(`null`)
				isValue = false
				n++
			}

		case string:
			if !isValue {
				if n != 0 {
					w.WriteByte(',')
				}

				io := int(dec.InputOffset())
				s := io - len(v1) - 2
				if io <= s || s <= 0 {
					return errors.New("invalid json")
				}

				w.Write(v[s:io])
				w.WriteString(`:`)
				isValue = true

			} else if !inArray {
				w.WriteString(`""`)
				isValue = false
				n++
			}

		case json.Delim:
			switch t.(json.Delim) {
			case '[':
				st.Push(n)
				inArray = true
				n = 0
			case ']':
				n = st.Pop()
				inArray = false
				isValue = false
				n++
			case '{':
				if n != 0 && !isValue {
					w.WriteByte(',')
				}
				st.Push(n)
				inArray = false
				isValue = false
				n = 0
			case '}':
				n = st.Pop()
				isValue = false
				n++
			}
			w.WriteByte(v[dec.InputOffset()-1])
		}

		dec.More()
	}

	return nil
}
