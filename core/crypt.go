package core

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"encoding/base64"
)

func encryptValues(
	data, encPrefix, decPrefix, nonce []byte,
	key [32]byte) ([]byte, error) {
	var s, e int

	if e = bytes.Index(data[s:], encPrefix); e == -1 {
		return data, nil
	}

	var b bytes.Buffer
	var buf [500]byte

	block, err := aes.NewCipher(key[:])
	if err != nil {
		return nil, err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}

	b64 := base64.NewEncoder(base64.RawStdEncoding, &b)

	pl := len(encPrefix)
	nonce = nonce[:gcm.NonceSize()]

	for {
		evs := (s + e + pl)
		q := bytes.IndexByte(data[evs:], '"')
		if q == -1 {
			break
		}
		eve := evs + q
		d := data[evs:eve]
		cl := (len(d) + 64)

		var out []byte
		if cl < len(buf) {
			out = buf[:cl]
		} else {
			out = make([]byte, cl)
		}

		ev := gcm.Seal(
			out[:0],
			nonce,
			d, nil)

		if s == 0 {
			b.Grow(len(data) + (len(data) / 5))
		}
		b.Write(data[s:(s + e)])
		b.Write(decPrefix)
		if _, err := b64.Write(nonce); err != nil {
			return nil, err
		}
		if _, err := b64.Write(ev); err != nil {
			return nil, err
		}
		b64.Close()
		s = eve

		if e = bytes.Index(data[s:], encPrefix); e == -1 {
			break
		}
	}
	b.Write(data[s:])
	return b.Bytes(), nil
}

func decryptValues(data, prefix []byte, key [32]byte) ([]byte, error) {
	var s, e int
	if e = bytes.Index(data[s:], prefix); e == -1 {
		return data, nil
	}

	var b bytes.Buffer
	var buf [500]byte

	block, err := aes.NewCipher(key[:])
	if err != nil {
		return nil, err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}

	pl := len(prefix)

	for {
		var fail bool

		evs := (s + e + pl)
		q := bytes.IndexByte(data[evs:], '"')
		if q == -1 {
			break
		}
		eve := evs + q
		d := data[evs:eve]
		dl := base64.RawStdEncoding.DecodedLen(len(d))

		var out []byte
		if dl < len(buf) {
			out = buf[:dl]
		} else {
			out = make([]byte, dl)
		}

		_, err := base64.RawStdEncoding.Decode(out, d)
		fail = err != nil

		var out1 []byte
		if !fail {
			out1, err = gcm.Open(
				out[gcm.NonceSize():][:0],
				out[:gcm.NonceSize()],
				out[gcm.NonceSize():],
				nil,
			)
			fail = err != nil
		}

		if s == 0 {
			b.Grow(len(data) + (len(data) / 5))
		}
		b.Write(data[s:e])

		if !fail {
			b.Write(out1)
		} else {
			b.Write(data[(s + e):eve])
		}
		s = eve
		if e = bytes.Index(data[s:], prefix); e == -1 {
			break
		}
	}
	b.Write(data[s:])
	return b.Bytes(), nil
}

func firstCursorValue(data []byte, prefix []byte) []byte {
	var buf [100]byte
	pf := append(buf[:0], prefix...)
	pf = append(pf, []byte("0,")...)
	pl := len(pf)

	s := bytes.Index(data, pf)
	if s == -1 {
		return nil
	}
	s = (s + pl)
	e := bytes.IndexByte(data[s:], '"')
	if e == -1 {
		return nil
	}
	e = s + e
	return data[(s - 2):e]
}
