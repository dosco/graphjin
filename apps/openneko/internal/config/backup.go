package config

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// ReadBackupKey reads a key without ever printing it and tightens its mode
// when the caller owns the file. Key files are deliberately plain files so
// the same recovery flow works on headless Linux and macOS hosts.
func ReadBackupKey(path string) (string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	value := strings.TrimSpace(string(raw))
	if len(value) < 32 {
		return "", fmt.Errorf("backup key %s is empty or too short", path)
	}
	if strings.ContainsAny(value, "\r\n") {
		return "", fmt.Errorf("backup key %s contains a newline", path)
	}
	if err := os.Chmod(path, 0o600); err != nil && !errors.Is(err, fs.ErrPermission) {
		return "", err
	}
	return value, nil
}

// CreateBackupKey creates a random key atomically. Existing files are never
// overwritten, which keeps concurrent starts and reinstalls fail-safe.
func CreateBackupKey(path string) (string, error) {
	if value, err := ReadBackupKey(path); err == nil {
		return value, nil
	} else if !errors.Is(err, fs.ErrNotExist) {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return "", err
	}
	fresh := make([]byte, 32)
	if _, err := rand.Read(fresh); err != nil {
		return "", err
	}
	value := hex.EncodeToString(fresh)
	if err := WriteBackupKey(path, value); errors.Is(err, fs.ErrExist) {
		return CreateBackupKey(path)
	} else if err != nil {
		return "", err
	}
	return value, nil
}

// WriteBackupKey copies a validated key into a new durable location. It never
// replaces an existing key.
func WriteBackupKey(path, value string) error {
	value = strings.TrimSpace(value)
	if len(value) < 32 || strings.ContainsAny(value, "\r\n") {
		return fmt.Errorf("refusing to write an invalid backup key")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	if _, err := file.WriteString(value + "\n"); err != nil {
		_ = file.Close()
		_ = os.Remove(path)
		return err
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		_ = os.Remove(path)
		return err
	}
	if err := file.Close(); err != nil {
		_ = os.Remove(path)
		return err
	}
	return nil
}
