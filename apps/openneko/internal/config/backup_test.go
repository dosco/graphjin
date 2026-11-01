package config

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

func TestCreateBackupKeyIsStableAndPrivate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "keys", "repository.key")
	first, err := CreateBackupKey(path)
	if err != nil {
		t.Fatal(err)
	}
	second, err := CreateBackupKey(path)
	if err != nil {
		t.Fatal(err)
	}
	if first != second || len(first) != 64 {
		t.Fatalf("backup key was not a stable 256-bit hex value")
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("backup key mode = %o, want 600", info.Mode().Perm())
	}
}

func TestReadBackupKeyRejectsShortExistingKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), "backup-key")
	if err := os.WriteFile(path, []byte("short\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadBackupKey(path); err == nil {
		t.Fatal("expected a short backup key to be rejected")
	}
}

func TestWriteBackupKeyNeverOverwrites(t *testing.T) {
	path := filepath.Join(t.TempDir(), "backup-key")
	first := "first-key-with-more-than-thirty-two-characters"
	second := "second-key-with-more-than-thirty-two-characters"
	if err := WriteBackupKey(path, first); err != nil {
		t.Fatal(err)
	}
	if err := WriteBackupKey(path, second); !errors.Is(err, fs.ErrExist) {
		t.Fatalf("second write error = %v, want file-exists", err)
	}
	got, err := ReadBackupKey(path)
	if err != nil {
		t.Fatal(err)
	}
	if got != first {
		t.Fatal("existing backup key was overwritten")
	}
}
