package core

import (
	"crypto/rand"
	"crypto/sha256"
	"fmt"
	"io"
	"testing"

	"github.com/dosco/graphjin/core/v3/internal/assert"
)

func TestCryptEncryptDecrypt(t *testing.T) {
	encPrefix := "__gj:foobar:"
	decPrefix := "__gj:enc:"

	js := []byte(fmt.Sprintf(
		`{ me: "null", posts_cursor: "%s12345" }`, encPrefix))

	expjs := []byte(
		`{ me: "null", posts_cursor: "12345" }`)

	key := [32]byte{}
	_, err := io.ReadFull(rand.Reader, key[:])
	assert.NoErrorFatal(t, err)

	nonce := sha256.Sum256(js)

	out1, err := encryptValues(
		js, []byte(encPrefix), []byte(decPrefix), nonce[:], key)
	assert.NoErrorFatal(t, err)

	out2, err := decryptValues(out1, []byte(decPrefix), key)
	assert.NoErrorFatal(t, err)

	assert.Equals(t, expjs, out2)
}

func TestCryptBadDecrypt(t *testing.T) {
	prefix := "__gj:enc:"

	js := []byte(
		`{ me: "null", posts_cursor: "__gj:enc:12345678901212345" }`)

	key := [32]byte{}
	_, err := io.ReadFull(rand.Reader, key[:])
	assert.NoErrorFatal(t, err)

	out, err := decryptValues(js, []byte(prefix), key)
	assert.NoErrorFatal(t, err)

	assert.Equals(t, js, out)
}

func TestCryptFirstEncyptedValue(t *testing.T) {
	prefix := "__gc:foobar:"

	js := `{ 
		me: "null", 
		a_cursor: "%s1,ABCDEFG",
		b_cursor: "%s0,12345"
	}`

	jsb := []byte(fmt.Sprintf(js, prefix, prefix))
	exp := []byte(`0,12345`)

	out := firstCursorValue(jsb, []byte(prefix))
	assert.Equals(t, exp, out)

	out1 := firstCursorValue(jsb, []byte("boo"))
	assert.Empty(t, out1)
}
