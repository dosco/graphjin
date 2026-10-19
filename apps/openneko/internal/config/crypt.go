package config

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Mirrors packages/llm/src/secrets.ts exactly: AES-256-GCM, key =
// SHA-256 of the trimmed ~/.config/openneko/secret-key contents, wire
// format "enc:v1:" + base64(iv[12] || tag[16] || ciphertext). The
// cross-language fixture test pins the formats together.

const encPrefix = "enc:v1:"

const keyFilename = "secret-key"

func keyPath(overrideDir string) string {
	return filepath.Join(Dir(overrideDir), keyFilename)
}

// loadOrCreateKey reads the shared secret-key file, generating and
// persisting one (0600) when absent — same behavior as the TS side.
func loadOrCreateKey(overrideDir string) ([]byte, error) {
	path := keyPath(overrideDir)
	raw, err := os.ReadFile(path)
	if err == nil {
		secret := strings.TrimSpace(string(raw))
		if secret != "" {
			sum := sha256.Sum256([]byte(secret))
			return sum[:], nil
		}
	} else if !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	fresh := make([]byte, 32)
	if _, err := rand.Read(fresh); err != nil {
		return nil, err
	}
	secret := base64.StdEncoding.EncodeToString(fresh)
	// Exclusive create: two overlapping CLI invocations (a deploy `upgrade`
	// racing a cron `status`) must never mint different keys — every enc:v1
	// value hangs off this file.
	// The loser of O_EXCL re-reads the winner's key.
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err == nil {
		_, werr := f.WriteString(secret)
		cerr := f.Close()
		if werr != nil {
			return nil, werr
		}
		if cerr != nil {
			return nil, cerr
		}
		sum := sha256.Sum256([]byte(secret))
		return sum[:], nil
	}
	if !errors.Is(err, fs.ErrExist) {
		return nil, err
	}
	// The race winner created the file but may not have written it yet;
	// give it a moment to land before declaring the file corrupt.
	var existing string
	for attempt := 0; attempt < 20; attempt++ {
		raw, rerr := os.ReadFile(path)
		if rerr != nil {
			return nil, rerr
		}
		existing = strings.TrimSpace(string(raw))
		if existing != "" {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if existing == "" {
		return nil, fmt.Errorf("secret-key at %s exists but is empty; remove the file to let openneko generate a new key", path)
	}
	sum := sha256.Sum256([]byte(existing))
	return sum[:], nil
}

// EncryptValue encrypts a plaintext value to the enc:v1 wire format.
func EncryptValue(overrideDir, value string) (string, error) {
	key, err := loadOrCreateKey(overrideDir)
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	iv := make([]byte, 12)
	if _, err := rand.Read(iv); err != nil {
		return "", err
	}
	// Go appends the 16-byte tag after the ciphertext; the TS layout is
	// iv || tag || ciphertext, so split and reorder.
	sealed := gcm.Seal(nil, iv, []byte(value), nil)
	ct := sealed[:len(sealed)-16]
	tag := sealed[len(sealed)-16:]
	packed := make([]byte, 0, 12+16+len(ct))
	packed = append(packed, iv...)
	packed = append(packed, tag...)
	packed = append(packed, ct...)
	return encPrefix + base64.StdEncoding.EncodeToString(packed), nil
}

// MaybeDecryptValue decrypts enc:v1 values; anything else (legacy
// plaintext) passes through unchanged.
func MaybeDecryptValue(overrideDir, value string) (string, error) {
	if !strings.HasPrefix(value, encPrefix) {
		return value, nil
	}
	key, err := loadOrCreateKey(overrideDir)
	if err != nil {
		return "", err
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(value, encPrefix))
	if err != nil {
		return "", fmt.Errorf("enc:v1 value is not valid base64: %w", err)
	}
	if len(raw) < 12+16 {
		return "", errors.New("enc:v1 value too short")
	}
	iv := raw[:12]
	tag := raw[12:28]
	ct := raw[28:]
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	sealed := make([]byte, 0, len(ct)+16)
	sealed = append(sealed, ct...)
	sealed = append(sealed, tag...)
	plain, err := gcm.Open(nil, iv, sealed, nil)
	if err != nil {
		return "", fmt.Errorf("enc:v1 decrypt failed (wrong or rotated secret-key?): %w", err)
	}
	return string(plain), nil
}

// IsEncrypted reports whether a value is in the enc:v1 wire format.
func IsEncrypted(value string) bool {
	return strings.HasPrefix(value, encPrefix)
}
