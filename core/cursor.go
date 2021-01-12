package core

import (
	"bytes"
	"encoding/base64"

	"github.com/dosco/graphjin/core/internal/crypto"
	"github.com/dosco/graphjin/core/internal/qcode"
	"github.com/dosco/graphjin/internal/jsn"
)

type cursors struct {
	data  []byte
	value string
}

func (gj *GraphJin) encryptCursor(qc *qcode.QCode, data []byte) (cursors, error) {
	var keys [][]byte
	cur := cursors{data: data}

	for _, sel := range qc.Selects {
		if sel.Paging.Cursor {
			keys = append(keys, []byte((sel.FieldName + "_cursor")))
		}
	}

	if len(keys) == 0 {
		return cur, nil
	}

	from := jsn.Get(data, keys)
	to := make([]jsn.Field, len(from))

	for i, f := range from {
		to[i].Key = f.Key

		if f.Value[0] != '"' || f.Value[len(f.Value)-1] != '"' {
			continue
		}

		if len(f.Value) > 2 {
			val := f.Value[1 : len(f.Value)-1]
			// save a copy of the first cursor value to use
			// with subscriptions when fetching the next set
			if cur.value == "" {
				cur.value = string(val)
			}
			v, err := crypto.Encrypt(val, &gj.encKey)
			if err != nil {
				return cur, err
			}

			var b bytes.Buffer
			b.Grow(base64.StdEncoding.EncodedLen(len(v)) + 2)
			b.WriteByte('"')
			b.WriteString(base64.StdEncoding.EncodeToString(v))
			b.WriteByte('"')
			to[i].Value = b.Bytes()
		} else {
			to[i].Value = []byte(`null`)
		}
	}

	var b bytes.Buffer
	if err := jsn.Replace(&b, data, from, to); err != nil {
		return cur, err
	}

	cur.data = b.Bytes()
	return cur, nil
}

func (gj *GraphJin) decrypt(data string) ([]byte, error) {
	v, err := base64.StdEncoding.DecodeString(data)
	if err != nil {
		return nil, err
	}
	return crypto.Decrypt(v, &gj.encKey)
}
