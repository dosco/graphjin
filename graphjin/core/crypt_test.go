package core

import (
	"crypto/rand"
	"crypto/sha256"
	"fmt"
	"io"
	"testing"

	"github.com/stretchr/testify/assert"
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
	if err != nil {
		panic(err)
	}

	nonce := sha256.Sum256(js)

	out1, err := encryptValues(
		js, []byte(encPrefix), []byte(decPrefix), nonce[:], key)
	if err != nil {
		t.Fatal(err)
	}

	out2, err := decryptValues(out1, []byte(decPrefix), key)
	if err != nil {
		t.Fatal(err)
	}

	assert.Equal(t, string(expjs), string(out2))
}

func TestCryptBadDecrypt(t *testing.T) {
	prefix := "__gj:enc:"

	js := []byte(
		`{ me: "null", posts_cursor: "__gj:enc:12345678901212345" }`)

	key := [32]byte{}
	_, err := io.ReadFull(rand.Reader, key[:])
	if err != nil {
		panic(err)
	}

	out, err := decryptValues(js, []byte(prefix), key)
	if err != nil {
		t.Fatal(err)
	}

	assert.Equal(t, string(js), string(out))
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
	assert.Equal(t, string(exp), string(out))

	out1 := firstCursorValue(jsb, []byte("boo"))
	assert.Empty(t, out1)
}
