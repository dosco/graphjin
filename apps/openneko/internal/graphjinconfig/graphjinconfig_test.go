package graphjinconfig

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/sha256"
	"encoding/base64"
	"strings"
	"testing"
)

func encryptNodeLayout(t *testing.T, plaintext, secret string) string {
	t.Helper()
	key := sha256.Sum256([]byte(strings.TrimSpace(secret)))
	block, err := aes.NewCipher(key[:])
	if err != nil {
		t.Fatal(err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		t.Fatal(err)
	}
	nonce := []byte("0123456789ab")
	sealed := gcm.Seal(nil, nonce, []byte(plaintext), nil)
	ciphertext := sealed[:len(sealed)-gcm.Overhead()]
	tag := sealed[len(sealed)-gcm.Overhead():]
	raw := append(append(append([]byte{}, nonce...), tag...), ciphertext...)
	return encryptedPrefix + base64.StdEncoding.EncodeToString(raw)
}

func TestDecryptPassword(t *testing.T) {
	t.Run("plaintext remains unchanged", func(t *testing.T) {
		got, err := decryptPassword("plain-password", "unused")
		if err != nil || got != "plain-password" {
			t.Fatalf("decryptPassword() = %q, %v", got, err)
		}
	})

	t.Run("decrypts the existing Node ciphertext layout", func(t *testing.T) {
		value := encryptNodeLayout(t, `p@ss"word`, "  deployment-secret\n")
		got, err := decryptPassword(value, "deployment-secret")
		if err != nil {
			t.Fatal(err)
		}
		if got != `p@ss"word` {
			t.Fatalf("decryptPassword() = %q", got)
		}
	})

	t.Run("rejects truncated encrypted values", func(t *testing.T) {
		_, err := decryptPassword(encryptedPrefix+base64.StdEncoding.EncodeToString([]byte("short")), "key")
		if err == nil {
			t.Fatal("decryptPassword() unexpectedly succeeded")
		}
	})
}

func TestRenderReplacesOnlyPasswordLine(t *testing.T) {
	seed := []byte("database:\n  host: neko-db\n  password: old\n  note: password: untouched\n")
	got, err := render(seed, "quote\" and newline\n")
	if err != nil {
		t.Fatal(err)
	}
	want := "database:\n  host: neko-db\n  password: \"quote\\\" and newline\\n\"\n  note: password: untouched\n"
	if string(got) != want {
		t.Fatalf("render() = %q, want %q", got, want)
	}
}

func TestRenderRequiresPasswordField(t *testing.T) {
	if _, err := render([]byte("database:\n  host: neko-db\n"), "secret"); err == nil {
		t.Fatal("render() unexpectedly succeeded")
	}
}
