package core

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"encoding/base64"
)

// encryptValues encrypts the values in the data using the given key
// data: the data to encrypt
// encPrefix: the prefix to search for the values to encrypt
// decPrefix: the prefix to replace the values with
// nonce: the nonce to use for encryption
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
		b64.Close() //nolint:errcheck
		s = eve

		if e = bytes.Index(data[s:], encPrefix); e == -1 {
			break
		}
	}
	b.Write(data[s:])
	return b.Bytes(), nil
}

// decryptValues decrypts the values in the data using the given key
// data: the data to decrypt
// prefix: the prefix to search for the values to decrypt
// key: the key to use for decryption
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

		evs := e + pl
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
			b.Write(data[e:eve])
		}
		s = eve
		if e = bytes.Index(data[s:], prefix); e == -1 {
			break
		}
		e += s // Convert relative offset to absolute position
	}
	b.Write(data[s:])
	return b.Bytes(), nil
}

// firstCursorValue returns the first cursor value in the data
// Cursor formats differ by database:
// - Postgres: prefix + decimal(sel.ID) + "," + values (comma-separated)
// - MariaDB/MSSQL: prefix + hex(sel.ID) + ":" + index + ":" + values (colon-separated)
// Examples: "gj-12345,1" (Postgres), "gj-6954668a:0:1" (MariaDB/MSSQL)
// When there are no results, cursor may be just prefix + sel.ID with no values.
// We only return cursors that have actual values (separator after sel.ID).
// Cursors without values would cause the query to restart from the beginning.
func firstCursorValue(data []byte, prefix []byte) []byte {
	s := bytes.Index(data, prefix)
	if s == -1 {
		return nil
	}
	// skip prefix
	i := s + len(prefix)

	// skip alphanumeric digits (sel.ID) - can contain 0-9 and a-f (hex for MariaDB, decimal for others)
	for i < len(data) && ((data[i] >= '0' && data[i] <= '9') || (data[i] >= 'a' && data[i] <= 'f')) {
		i++
	}

	// Find the end quote
	e := bytes.IndexByte(data[i:], '"')
	if e == -1 {
		return nil
	}
	e = i + e

	// Only return cursor if it has actual values (separator + values after sel.ID).
	// Cursors that end with just the quote (no values) would cause queries to
	// restart from the beginning, so we treat them as empty/no cursor.
	// Check: separator exists AND there's content between separator and end quote
	if i < len(data) && (data[i] == ',' || data[i] == ':') && (i+1 < e) {
		return data[s:e]
	}
	return nil
}
